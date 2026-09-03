/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kubevirt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetJobImage returns the image to use for RHCOS provisioning Jobs.
// Priority: resolved CLI image from release payload > RELATED_IMAGE_CLI env var > default.
func GetJobImage(resolvedCliImage string) string {
	if resolvedCliImage != "" {
		return resolvedCliImage
	}
	if img := os.Getenv(RelatedImageCLIEnvVar); img != "" {
		return img
	}
	return DefaultJobImage
}

const (
	RHCOSGoldenPVCReadyAnnotation = "capoa.openshift.io/rhcos-golden-pvc"
	RHCOSGoldenPVCNamePrefix      = "rhcos-golden-"
	RHCOSGoldenPVCDefaultSize     = "30Gi"
	RHCOSURLConfigMapName         = "capoa-rhcos-url"
	RHCOSURLJobNamePrefix         = "rhcos-url-resolve-"
	RHCOSURLConfigMapKey          = "qcow2-url"

	RHCOSImportJobNamePrefix = "rhcos-import-"

	jobFailedCooldownAnnotation = "capoa.openshift.io/last-job-failure"
	jobFailedCooldown           = 5 * time.Minute
	jobTTLSeconds               = int32(3600)

	// DefaultJobImage is used only when no release-payload-resolved image is
	// available (e.g., during the initial CLI image resolution bootstrap).
	// In disconnected environments, the operator deployment must set the
	// RELATED_IMAGE_CLI env var via the CSV to an accessible mirror.
	DefaultJobImage = "registry.redhat.io/openshift4/ose-tools-rhel9:latest"

	// RelatedImageCLIEnvVar is the env var injected by OLM (via CSV RELATED_IMAGES)
	// to provide a mirrored CLI/tools image for disconnected environments.
	RelatedImageCLIEnvVar = "RELATED_IMAGE_CLI"
)

// GoldenPVCName returns the name of the golden PVC for a given OCP version.
func GoldenPVCName(version string) string {
	majorMinor := extractMajorMinor(version)
	if majorMinor == "" {
		return RHCOSGoldenPVCNamePrefix + version
	}
	return RHCOSGoldenPVCNamePrefix + majorMinor
}

// EnsureRHCOSGoldenPVC provisions a golden PVC containing the RHCOS disk image.
//
// The approach uses two Jobs:
//  1. A URL-resolution Job extracts the RHCOS ociarchive download URL from the release
//     payload's coreos-stream.json and writes it to /dev/termination-log.
//  2. An import Job downloads the ociarchive, parses the OCI layout to find the disk
//     layer, and streams the qcow2 directly to a block-mode PVC.
//
// No CDI, no container registry push, no qemu-img — only standard POSIX tools plus jq.
// KubeVirt auto-detects qcow2 format on block PVCs.
//
// Returns true when the golden PVC is ready (import Job succeeded).
func EnsureRHCOSGoldenPVC(
	ctx context.Context,
	c client.Client,
	infraClient client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	releaseImage string,
	pullSecretName string,
	infraNamespace string,
	resolvedCliImage string,
) (bool, error) {
	log := ctrl.LoggerFrom(ctx)

	if oacp.Annotations != nil && oacp.Annotations[RHCOSGoldenPVCReadyAnnotation] != "" {
		return true, nil
	}

	majorMinor := extractMajorMinor(oacp.Spec.DistributionVersion)
	if majorMinor == "" {
		return false, fmt.Errorf("cannot extract major.minor from version %q", oacp.Spec.DistributionVersion)
	}

	pvcName := GoldenPVCName(oacp.Spec.DistributionVersion)
	namespace := infraNamespace

	// Step 1: Resolve the RHCOS ociarchive download URL from the release payload
	rhcosURL, err := ensureRHCOSURLResolved(ctx, infraClient, namespace, majorMinor, releaseImage, pullSecretName, resolvedCliImage)
	if err != nil {
		return false, err
	}
	if rhcosURL == "" {
		return false, nil
	}

	// Step 2: Create the golden PVC
	pvc := &corev1.PersistentVolumeClaim{}
	err = infraClient.Get(ctx, client.ObjectKey{Name: pvcName, Namespace: namespace}, pvc)
	if errors.IsNotFound(err) {
		pvc = buildGoldenPVC(pvcName, namespace, oacp)
		if createErr := infraClient.Create(ctx, pvc); createErr != nil {
			if errors.IsAlreadyExists(createErr) {
				return false, nil
			}
			return false, fmt.Errorf("failed to create golden PVC: %w", createErr)
		}
		log.Info("created RHCOS golden PVC", "name", pvcName)
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check golden PVC: %w", err)
	}

	// Step 3: Create/check the import Job
	jobName := RHCOSImportJobNamePrefix + majorMinor
	existingJob := &batchv1.Job{}
	err = infraClient.Get(ctx, client.ObjectKey{Name: jobName, Namespace: namespace}, existingJob)
	if err == nil {
		if existingJob.Status.Succeeded > 0 {
			log.Info("RHCOS golden PVC ready", "name", pvcName)
			if oacp.Annotations == nil {
				oacp.Annotations = make(map[string]string)
			}
			oacp.Annotations[RHCOSGoldenPVCReadyAnnotation] = pvcName
			if updateErr := c.Update(ctx, oacp); updateErr != nil {
				return false, fmt.Errorf("failed to persist golden PVC annotation: %w", updateErr)
			}
			return true, nil
		}
		if existingJob.Status.Failed > 0 {
			if !isJobFailureCooldownElapsed(oacp) {
				log.V(1).Info("RHCOS import Job failed recently, waiting for cooldown", "job", jobName)
				return false, nil
			}
			log.Info("RHCOS import Job failed, deleting for retry", "job", jobName)
			_ = infraClient.Delete(ctx, existingJob, client.PropagationPolicy(metav1.DeletePropagationBackground))
			setJobFailureCooldown(oacp)
			_ = c.Update(ctx, oacp)
			return false, nil
		}
		log.V(1).Info("RHCOS import Job still running", "job", jobName)
		return false, nil
	}
	if !errors.IsNotFound(err) {
		return false, fmt.Errorf("failed to check RHCOS import Job: %w", err)
	}

	job := buildRHCOSImportJob(jobName, namespace, pvcName, rhcosURL, GetJobImage(resolvedCliImage))
	if err := infraClient.Create(ctx, job); err != nil {
		if errors.IsAlreadyExists(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to create RHCOS import Job: %w", err)
	}
	log.Info("created RHCOS import Job", "job", jobName, "url", rhcosURL)
	return false, nil
}

// isJobFailureCooldownElapsed checks whether enough time has passed since
// the last recorded Job failure to allow a retry.
func isJobFailureCooldownElapsed(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane) bool {
	if oacp.Annotations == nil {
		return true
	}
	lastFailure := oacp.Annotations[jobFailedCooldownAnnotation]
	if lastFailure == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, lastFailure)
	if err != nil {
		return true
	}
	return time.Since(t) > jobFailedCooldown
}

func setJobFailureCooldown(oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane) {
	if oacp.Annotations == nil {
		oacp.Annotations = make(map[string]string)
	}
	oacp.Annotations[jobFailedCooldownAnnotation] = time.Now().UTC().Format(time.RFC3339)
}

func buildGoldenPVC(name, namespace string, oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane) *corev1.PersistentVolumeClaim {
	volumeMode := corev1.PersistentVolumeBlock
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"capoa.openshift.io/cluster-name": oacp.Name,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			VolumeMode:  &volumeMode,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(RHCOSGoldenPVCDefaultSize),
				},
			},
		},
	}

	if oacp.Spec.Config.CSIDriver != nil && oacp.Spec.Config.CSIDriver.InfraStorageClass != "" {
		pvc.Spec.StorageClassName = ptr.To(oacp.Spec.Config.CSIDriver.InfraStorageClass)
	}

	return pvc
}

func buildRHCOSImportJob(name, namespace, pvcName, rhcosURL, jobImage string) *batchv1.Job {
	// The RHCOS kubevirt ociarchive is ~1GB compressed. The script extracts
	// the disk filename from the OCI layer's tar listing, then streams the
	// disk directly to the block device in a single decompression pass.
	script := `#!/bin/bash
set -euo pipefail

WORKDIR=$(mktemp -d)
cleanup() { rm -rf "$WORKDIR"; }
trap cleanup EXIT

cd "$WORKDIR"

echo "Downloading ociarchive from $RHCOS_URL..."
curl -fsSL --retry 3 --retry-delay 5 "$RHCOS_URL" -o rhcos.ociarchive
echo "Download complete ($(du -h rhcos.ociarchive | cut -f1))"

echo "Extracting ociarchive..."
tar xf rhcos.ociarchive
rm -f rhcos.ociarchive

if [ ! -f index.json ]; then
  echo "ERROR: Not a valid OCI archive — index.json missing" >&2
  ls -la >&2
  exit 1
fi

echo "Parsing OCI layout..."
MANIFEST_DIGEST=$(jq -r '.manifests[0].digest' index.json | sed 's/sha256://')
if [ -z "$MANIFEST_DIGEST" ] || [ "$MANIFEST_DIGEST" = "null" ]; then
  echo "ERROR: Could not parse manifest digest from index.json" >&2
  cat index.json >&2
  exit 1
fi
MANIFEST_PATH="blobs/sha256/$MANIFEST_DIGEST"

LAYER_DIGEST=$(jq -r '.layers[-1].digest' "$MANIFEST_PATH" | sed 's/sha256://')
if [ -z "$LAYER_DIGEST" ] || [ "$LAYER_DIGEST" = "null" ]; then
  echo "ERROR: Could not parse layer digest from manifest" >&2
  jq . "$MANIFEST_PATH" >&2
  exit 1
fi
LAYER_PATH="blobs/sha256/$LAYER_DIGEST"
echo "  manifest=$MANIFEST_DIGEST"
echo "  disk_layer=$LAYER_DIGEST ($(du -h "$LAYER_PATH" | cut -f1))"

echo "Identifying disk file in layer..."
# Extract filename from OCI manifest annotations if available, fall back to tar listing
DISK_NAME=$(jq -r '.layers[-1].annotations["org.opencontainers.image.title"] // empty' "$MANIFEST_PATH" 2>/dev/null)
if [ -z "$DISK_NAME" ] || ! echo "$DISK_NAME" | grep -qE '\.(qcow2|raw|img)$'; then
  DISK_NAME=$(gunzip -c "$LAYER_PATH" | tar -tf - | grep -E '\.(qcow2|raw|img)$' | head -1)
fi
if [ -z "$DISK_NAME" ]; then
  echo "ERROR: No disk file found in layer" >&2
  gunzip -c "$LAYER_PATH" | tar -tf - >&2
  exit 1
fi
echo "  disk_file=$DISK_NAME"

echo "Streaming disk to /dev/disk..."
gunzip -c "$LAYER_PATH" | tar -xf - --to-stdout "$DISK_NAME" | dd of=/dev/disk bs=4M status=progress

echo "Disk write complete."
`

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To[int32](2),
			TTLSecondsAfterFinished: ptr.To(jobTTLSeconds),
			ActiveDeadlineSeconds:   ptr.To[int64](3600),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:    "import-rhcos",
							Image:   jobImage,
							Command: []string{"/bin/bash", "-c", script},
							Env: []corev1.EnvVar{
								{Name: "RHCOS_URL", Value: rhcosURL},
							},
							VolumeDevices: []corev1.VolumeDevice{
								{Name: "disk", DevicePath: "/dev/disk"},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:              resource.MustParse("500m"),
									corev1.ResourceMemory:           resource.MustParse("512Mi"),
									corev1.ResourceEphemeralStorage: resource.MustParse("4Gi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "disk",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcName,
								},
							},
						},
					},
				},
			},
		},
	}
}

// ensureRHCOSURLResolved ensures the RHCOS ociarchive download URL has been extracted
// from the release payload. Uses a lightweight Job that runs `oc adm release info`
// to read coreos-stream.json and extracts the kubevirt ociarchive URL via jq.
// The URL is written to /dev/termination-log and read by the controller from the
// pod's termination message (no ConfigMap RBAC needed for the Job's ServiceAccount).
// Returns the URL if resolved, or "" if the Job is still running.
func ensureRHCOSURLResolved(
	ctx context.Context,
	c client.Client,
	namespace string,
	majorMinor string,
	releaseImage string,
	pullSecretName string,
	resolvedCliImage string,
) (string, error) {
	log := ctrl.LoggerFrom(ctx)

	// Check if URL is already resolved in ConfigMap
	cmName := RHCOSURLConfigMapName + "-" + majorMinor
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, client.ObjectKey{Name: cmName, Namespace: namespace}, cm); err == nil {
		if url := cm.Data[RHCOSURLConfigMapKey]; url != "" {
			return url, nil
		}
	}

	// Check/create the URL resolution Job
	jobName := RHCOSURLJobNamePrefix + majorMinor
	existingJob := &batchv1.Job{}
	err := c.Get(ctx, client.ObjectKey{Name: jobName, Namespace: namespace}, existingJob)
	if err == nil {
		if existingJob.Status.Succeeded > 0 {
			url, logErr := ReadURLFromJobPod(ctx, c, jobName, namespace)
			if logErr != nil || url == "" {
				log.Info("RHCOS URL Job completed but could not read URL, deleting for retry", "error", logErr)
				_ = c.Delete(ctx, existingJob, client.PropagationPolicy(metav1.DeletePropagationBackground))
				return "", nil
			}
			newCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: namespace},
				Data:       map[string]string{RHCOSURLConfigMapKey: url},
			}
			if createErr := c.Create(ctx, newCM); createErr != nil && !errors.IsAlreadyExists(createErr) {
				return "", fmt.Errorf("failed to create RHCOS URL ConfigMap: %w", createErr)
			}
			log.Info("resolved RHCOS ociarchive URL from release payload", "url", url)
			return url, nil
		}
		if existingJob.Status.Failed > 0 {
			log.Info("RHCOS URL resolution Job failed, deleting for retry", "job", jobName)
			_ = c.Delete(ctx, existingJob, client.PropagationPolicy(metav1.DeletePropagationBackground))
			return "", nil
		}
		log.V(1).Info("RHCOS URL resolution Job still running", "job", jobName)
		return "", nil
	}
	if !errors.IsNotFound(err) {
		return "", fmt.Errorf("failed to check RHCOS URL Job: %w", err)
	}

	job := buildRHCOSURLResolveJob(jobName, namespace, releaseImage, pullSecretName, GetJobImage(resolvedCliImage))
	if err := c.Create(ctx, job); err != nil {
		if errors.IsAlreadyExists(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to create RHCOS URL resolve Job: %w", err)
	}
	log.Info("created RHCOS URL resolution Job", "job", jobName)
	return "", nil
}

// ReadURLFromJobPod reads the termination message from the completed Job's pod.
func ReadURLFromJobPod(ctx context.Context, c client.Client, jobName, namespace string) (string, error) {
	pods := &corev1.PodList{}
	if err := c.List(ctx, pods, client.InNamespace(namespace), client.MatchingLabels{"job-name": jobName}); err != nil {
		return "", err
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded {
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Terminated != nil && cs.State.Terminated.Message != "" {
					result := strings.TrimSpace(cs.State.Terminated.Message)
					if result != "" {
						return result, nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("no completed pod found for job %s", jobName)
}

func buildRHCOSURLResolveJob(name, namespace, releaseImage, pullSecretName, jobImage string) *batchv1.Job {
	script := `#!/bin/bash
set -euo pipefail

ERRTMP=$(mktemp)
MOS_IMAGE=$(oc adm release info "$RELEASE_IMAGE" --image-for=machine-os-images --registry-config=/pull-secret/.dockerconfigjson 2>"$ERRTMP" || true)
if [ -z "$MOS_IMAGE" ]; then
  echo "ERROR: Could not find machine-os-images in release payload" >&2
  cat "$ERRTMP" >&2
  rm -f "$ERRTMP"
  exit 1
fi
rm -f "$ERRTMP"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT
oc image extract "$MOS_IMAGE" --path /coreos/coreos-stream.json:"$TMPDIR" --registry-config=/pull-secret/.dockerconfigjson

URL=$(jq -r '.architectures.x86_64.artifacts.kubevirt.formats.ociarchive.disk.location' "$TMPDIR/coreos-stream.json")
if [ -z "$URL" ] || [ "$URL" = "null" ]; then
  echo "ERROR: RHCOS kubevirt ociarchive URL not found in coreos-stream.json" >&2
  jq '.architectures.x86_64.artifacts | keys' "$TMPDIR/coreos-stream.json" >&2
  exit 1
fi

echo "$URL" > /dev/termination-log
echo "Resolved: $URL"
`
	return buildPullSecretJob(name, namespace, releaseImage, pullSecretName, jobImage, script, "resolve-url",
		corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
		corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")},
	)
}

// buildPullSecretJob creates a Job that runs a script with access to the release
// image env var and a mounted pull secret. Used by both URL resolution and CLI
// image resolution Jobs.
func buildPullSecretJob(name, namespace, releaseImage, pullSecretName, jobImage, script, containerName string, requests, limits corev1.ResourceList) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To[int32](3),
			TTLSecondsAfterFinished: ptr.To(jobTTLSeconds),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:    containerName,
							Image:   jobImage,
							Command: []string{"/bin/bash", "-c", script},
							Env: []corev1.EnvVar{
								{Name: "RELEASE_IMAGE", Value: releaseImage},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "pull-secret", MountPath: "/pull-secret", ReadOnly: true},
							},
							Resources: corev1.ResourceRequirements{
								Requests: requests,
								Limits:   limits,
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "pull-secret",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: pullSecretName,
								},
							},
						},
					},
				},
			},
		},
	}
}

// extractMajorMinor returns "X.Y" from a version string like "4.20.24" or "4.20".
func extractMajorMinor(version string) string {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "." + parts[1]
}
