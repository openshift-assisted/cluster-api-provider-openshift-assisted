package util

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift-assisted/cluster-api-agent/pkg/containers"
	imageapi "github.com/openshift/api/image/v1"
	yaml "sigs.k8s.io/yaml/goyaml.v2"

	controlplanev1alpha1 "github.com/openshift-assisted/cluster-api-agent/controlplane/api/v1alpha2"
	logutil "github.com/openshift-assisted/cluster-api-agent/util/log"

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

func GetK8sVersionFromImageRef(imageRef, pullsecret string) (string, error) {
	const filepath = "/release-manifests/image-references"
	auth, err := containers.PullSecretKeyChainFromString(pullsecret)
	if err != nil {
		return "", fmt.Errorf("unable to load auth from pull-secret: %v", err)
	}
	extractor, err := containers.NewImageExtractor(imageRef, auth)
	if err != nil {
		return "", fmt.Errorf("unable to create extractor: %v", err)
	}
	fileContent, err := extractor.ExtractFileFromImage(filepath)
	if err != nil {
		return "", fmt.Errorf("unable to extract file %s from imageRef %s : %v", filepath, imageRef, err)
	}

	is := &imageapi.ImageStream{}
	if err := yaml.Unmarshal(fileContent, &is); err != nil {
		return "", fmt.Errorf("unable to load release image-references: %v", err)
	}
	k8sVersion, err := getK8sVersionFromImageStream(*is)
	if err != nil {
		return "", fmt.Errorf("unable to extract k8s from release image-references: %v", err)
	}
	return k8sVersion, nil
}

func getK8sVersionFromImageStream(is imageapi.ImageStream) (string, error) {
	const annotationBuildVersions = "io.openshift.build.versions"

	for _, tag := range is.Spec.Tags {
		versions, ok := tag.Annotations[annotationBuildVersions]
		if !ok {
			continue
		}
		parts := strings.Split(versions, "=")
		if len(parts) != 2 {
			continue
		}
		if parts[0] == "kubernetes" {
			return parts[1], nil
		}
	}
	return "", fmt.Errorf("unable to find kubernetes version")
}
