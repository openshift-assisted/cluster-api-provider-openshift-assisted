package util

import (
	"context"
	"errors"
	"fmt"

	controlplanev1alpha1 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha2"
	logutil "github.com/openshift-assisted/cluster-api-agent/util/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/labels/format"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetTypedOwner(ctx context.Context, k8sClient client.Client, obj client.Object, owner client.Object) error {
	log := ctrl.LoggerFrom(ctx)

	// TODO: can we guess Kind and APIVersion before retrieving it?
	for _, ownerRef := range obj.GetOwnerReferences() {
		err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      ownerRef.Name,
		}, owner)
		if err != nil {
			log.V(logutil.TraceLevel).
				Info(fmt.Sprintf("could not find %T", owner), "name", ownerRef.Name, "namespace", obj.GetNamespace())
			continue
		}
		gvk := owner.GetObjectKind().GroupVersionKind()
		if ownerRef.APIVersion == gvk.GroupVersion().String() && ownerRef.Kind == gvk.Kind {
			return nil
		}
	}
	return fmt.Errorf("couldn't find %T owner for %T", owner, obj)
}

// ControlPlaneMachineLabelsForCluster returns a set of labels to add
// to a control plane machine for this specific cluster.
func ControlPlaneMachineLabelsForCluster(
	acp *controlplanev1alpha1.OpenshiftAssistedControlPlane,
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
