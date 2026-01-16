package util

import (
	"context"
	"errors"
	"fmt"
	"strings"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	logutil "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/util/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/labels/format"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetTypedOwner(ctx context.Context, k8sClient client.Client, obj client.Object, owner client.Object) error {
	log := ctrl.LoggerFrom(ctx)

	gvks, _, err := k8sClient.Scheme().ObjectKinds(owner)
	if err != nil || len(gvks) == 0 {
		return fmt.Errorf("no GVK registered for %T", owner)
	}
	gvk := gvks[0]

	for _, ownerRef := range obj.GetOwnerReferences() {
		ownerGV, _ := schema.ParseGroupVersion(ownerRef.APIVersion)
		if ownerGV.Group != gvk.Group || ownerRef.Kind != gvk.Kind {
			continue
		}
		err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      ownerRef.Name,
		}, owner)
		if err != nil {
			log.V(logutil.TraceLevel).
				Info(fmt.Sprintf("could not find %T", owner), "name", ownerRef.Name, "namespace", obj.GetNamespace())
			continue
		}
		return nil
	}
	return fmt.Errorf("couldn't find %T owner for %T", owner, obj)
}

// ownerRefMigration defines the expected group prefix and current API version for a Kind
type ownerRefMigration struct {
	groupPrefix       string
	currentAPIVersion string
}

// ownerRefMigrations maps Kind to its migration info.
// Only Kinds we set owner references to are included.
var ownerRefMigrations = map[string]ownerRefMigration{
	"Machine":                         {groupPrefix: "cluster.x-k8s.io/", currentAPIVersion: clusterv1.GroupVersion.String()},
	"Cluster":                         {groupPrefix: "cluster.x-k8s.io/", currentAPIVersion: clusterv1.GroupVersion.String()},
	"OpenshiftAssistedConfig":         {groupPrefix: "bootstrap.cluster.x-k8s.io/", currentAPIVersion: "bootstrap.cluster.x-k8s.io/v1alpha2"},
	"OpenshiftAssistedConfigTemplate": {groupPrefix: "bootstrap.cluster.x-k8s.io/", currentAPIVersion: "bootstrap.cluster.x-k8s.io/v1alpha2"},
	"OpenshiftAssistedControlPlane":   {groupPrefix: "controlplane.cluster.x-k8s.io/", currentAPIVersion: "controlplane.cluster.x-k8s.io/v1alpha3"},
}

// MigrateOwnerReferences updates owner references on an object to use current API versions.
// Returns true if any references were updated.
func MigrateOwnerReferences(ctx context.Context, k8sClient client.Client, obj client.Object) (bool, error) {
	log := ctrl.LoggerFrom(ctx)
	ownerRefs := obj.GetOwnerReferences()
	updated := false

	for i, ref := range ownerRefs {
		migration, ok := ownerRefMigrations[ref.Kind]
		if !ok {
			// Kind not in our migration map, skip
			continue
		}

		// Verify it belongs to the expected CAPI group
		if !strings.HasPrefix(ref.APIVersion, migration.groupPrefix) {
			continue
		}

		// Check if migration is needed
		if ref.APIVersion != migration.currentAPIVersion {
			log.V(logutil.DebugLevel).Info("Migrating owner reference API version",
				"object", obj.GetName(),
				"ownerKind", ref.Kind,
				"ownerName", ref.Name,
				"oldAPIVersion", ref.APIVersion,
				"newAPIVersion", migration.currentAPIVersion)

			ownerRefs[i].APIVersion = migration.currentAPIVersion
			updated = true
		}
	}

	if updated {
		obj.SetOwnerReferences(ownerRefs)
		if err := k8sClient.Update(ctx, obj); err != nil {
			return false, fmt.Errorf("failed to update owner references on %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
		log.V(logutil.DebugLevel).Info("Updated owner references", "object", obj.GetName())
	}

	return updated, nil
}

// ControlPlaneMachineLabelsForCluster returns a set of labels to add
// to a control plane machine for this specific cluster.
func ControlPlaneMachineLabelsForCluster(
	acp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	clusterName string,
) map[string]string {
	labels := map[string]string{}

	// Add the labels from the MachineTemplate.
	// Note: we intentionally don't use the map directly to ensure we don't modify the map in KCP.
	for k, v := range acp.Spec.MachineTemplate.ObjectMeta.Labels {
		labels[k] = v
	}

	// Always force these labels over the ones coming from the spec.
	labels[clusterv1.ClusterNameLabel] = clusterName
	labels[clusterv1.MachineControlPlaneLabel] = ""
	// Note: MustFormatValue is used here as the label value can be a hash if the control plane name is
	// longer than 63 characters.
	labels[clusterv1.MachineControlPlaneNameLabel] = format.MustFormatValue(acp.Name)
	return labels
}

func GetClusterKubeconfigSecret(
	ctx context.Context,
	client client.Client,
	clusterName, namespace string,
) (*corev1.Secret, error) {
	secretName := fmt.Sprintf("%s-kubeconfig", clusterName)
	kubeconfigSecret := &corev1.Secret{}
	if err := client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, kubeconfigSecret); err != nil {
		return nil, err
	}
	return kubeconfigSecret, nil
}

// ExtractKubeconfigFromSecret takes a kubernetes secret and returns the kubeconfig
func ExtractKubeconfigFromSecret(kubeconfigSecret *corev1.Secret, dataKey string) ([]byte, error) {
	kubeconfig, ok := kubeconfigSecret.Data[dataKey]
	if !ok {
		return nil, errors.New("kubeconfig not found in secret")
	}
	return kubeconfig, nil
}

// FindStatusCondition takes a set of conditions and a condition to find and returns it if it exists
func FindStatusCondition(conditions clusterv1.Conditions,
	conditionToFind clusterv1.ConditionType) *clusterv1.Condition {
	for _, condition := range conditions {
		if condition.Type == conditionToFind {
			return &condition
		}
	}
	return nil
}

func GetWorkloadKubeconfig(
	ctx context.Context,
	client client.Client,
	clusterName string,
	clusterNamespace string,
) ([]byte, error) {
	kubeconfigSecret, err := GetClusterKubeconfigSecret(ctx, client, clusterName, clusterNamespace)
	if err != nil {
		return nil, errors.Join(err, fmt.Errorf("failed to get cluster kubeconfig secret"))
	}

	if kubeconfigSecret == nil {
		return nil, fmt.Errorf("kubeconfig secret was not found")
	}

	kubeconfig, err := ExtractKubeconfigFromSecret(kubeconfigSecret, "value")
	if err != nil {
		err = errors.Join(err, fmt.Errorf("failed to extract kubeconfig from secret %s", kubeconfigSecret.Name))
		return nil, err
	}
	return kubeconfig, nil
}
