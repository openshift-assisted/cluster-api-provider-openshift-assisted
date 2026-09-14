package kubevirt

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	// VMIStaleThreshold is how long a VMI can run without an agent registering
	// before we consider it failed to process its Ignition config.
	// Must account for: VM boot (~30s) + container image pull (~60s) + agent startup (~60s).
	// Set generously to avoid destroying VMs that successfully booted but need time to register.
	VMIStaleThreshold = 7 * time.Minute

	// MaxRecoveryAttempts limits how many times we'll retry before giving up.
	// Each attempt reimports the disk (~60-90s) plus waits for boot + agent (~120s).
	MaxRecoveryAttempts = 5

	recoveryCountAnnotation = "capoa.openshift.io/ignition-recovery-count"
)

// RecoverStaleVM handles the case where a KubeVirt VM booted but Ignition
// failed to detect the config drive (a known race condition in initramfs).
// Since Ignition only runs on first boot, simply restarting the VMI is
// insufficient - the root disk must also be deleted to force a fresh import
// and restore the firstboot marker.
//
// Recovery steps:
//  1. Delete the VMI (stops the VM)
//  2. Delete the root disk PVC (removes consumed firstboot state)
//  3. KubeVirt's DataVolumeTemplate recreates the DV, CDI reimports the image
//  4. runStrategy:Always restarts the VM with a fresh disk
//
// Returns true if recovery was triggered.
func RecoverStaleVM(ctx context.Context, k8sClient client.Client, vmiName, vmiNamespace string) (bool, error) {
	log := ctrl.LoggerFrom(ctx)

	vmi := &unstructured.Unstructured{}
	vmi.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "kubevirt.io",
		Version: "v1",
		Kind:    "VirtualMachineInstance",
	})

	if err := k8sClient.Get(ctx, client.ObjectKey{Name: vmiName, Namespace: vmiNamespace}, vmi); err != nil {
		return false, client.IgnoreNotFound(err)
	}

	creationTime := vmi.GetCreationTimestamp()
	if creationTime.IsZero() {
		return false, nil
	}

	age := time.Since(creationTime.Time)
	if age < VMIStaleThreshold {
		return false, nil
	}

	phase, _, _ := unstructured.NestedString(vmi.Object, "status", "phase")
	if phase != "Running" {
		return false, nil
	}

	// Check recovery attempt count (prevent infinite loops)
	vm := &unstructured.Unstructured{}
	vm.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "kubevirt.io",
		Version: "v1",
		Kind:    "VirtualMachine",
	})
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: vmiName, Namespace: vmiNamespace}, vm); err != nil {
		return false, client.IgnoreNotFound(err)
	}
	vmAnnotations := vm.GetAnnotations()
	attemptCount := 0
	if vmAnnotations != nil {
		if countStr, ok := vmAnnotations[recoveryCountAnnotation]; ok {
			if _, err := fmt.Sscanf(countStr, "%d", &attemptCount); err != nil {
				log.V(1).Info("ignoring malformed recovery count annotation", "value", countStr, "error", err)
			}
			if attemptCount >= MaxRecoveryAttempts {
				return false, nil
			}
		}
	}

	attemptCount++
	log.Info("recovering stale VM: Ignition likely missed config drive due to initramfs race",
		"vm", vmiName, "namespace", vmiNamespace, "age", age.String(),
		"attempt", attemptCount, "maxAttempts", MaxRecoveryAttempts)

	// Update the attempt count
	if vmAnnotations == nil {
		vmAnnotations = make(map[string]string)
	}
	vmAnnotations[recoveryCountAnnotation] = fmt.Sprintf("%d", attemptCount)
	vm.SetAnnotations(vmAnnotations)
	if err := k8sClient.Update(ctx, vm); err != nil {
		log.Error(err, "failed to update VM recovery count", "vm", vmiName)
	}

	// Step 1: Delete the VMI to stop the VM
	if err := k8sClient.Delete(ctx, vmi); err != nil && !apierrors.IsNotFound(err) {
		return false, fmt.Errorf("failed to delete stale VMI %s/%s: %w", vmiNamespace, vmiName, err)
	}
	log.Info("deleted VMI", "vmi", vmiName)

	// Step 2: Delete the root disk PVC to force fresh import (restores firstboot marker).
	// The DataVolume name follows CAPK's convention: <vm-name>-rootdisk
	rootDiskPVCName := vmiName + "-rootdisk"
	pvc := &corev1.PersistentVolumeClaim{}
	pvcKey := client.ObjectKey{Name: rootDiskPVCName, Namespace: vmiNamespace}
	if err := k8sClient.Get(ctx, pvcKey, pvc); err == nil {
		if err := k8sClient.Delete(ctx, pvc); err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "failed to delete root disk PVC", "pvc", rootDiskPVCName)
		} else {
			log.Info("deleted root disk PVC for fresh reimport", "pvc", rootDiskPVCName)
		}
	}

	// Also delete the DataVolume if it exists (CDI will recreate from VM's DataVolumeTemplate)
	dv := &unstructured.Unstructured{}
	dv.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cdi.kubevirt.io",
		Version: "v1beta1",
		Kind:    "DataVolume",
	})
	dvKey := client.ObjectKey{Name: rootDiskPVCName, Namespace: vmiNamespace}
	if err := k8sClient.Get(ctx, dvKey, dv); err == nil {
		if err := k8sClient.Delete(ctx, dv); err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "failed to delete root disk DataVolume", "dv", rootDiskPVCName)
		} else {
			log.Info("deleted root disk DataVolume for fresh reimport", "dv", rootDiskPVCName)
		}
	}

	return true, nil
}
