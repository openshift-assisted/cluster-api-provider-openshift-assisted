package util

import (
	"context"

	"fmt"
	logutil "github.com/openshift-assisted/cluster-api-agent/util/log"
	"k8s.io/apimachinery/pkg/types"
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
			log.V(logutil.TraceLevel).Info(fmt.Sprintf("could not find %T", owner), "name", ownerRef.Name, "namespace", obj.GetNamespace())
			continue
		}
		gvk := owner.GetObjectKind().GroupVersionKind()
		if ownerRef.APIVersion == gvk.GroupVersion().String() && ownerRef.Kind == gvk.Kind {
			return nil
		}
	}
	return fmt.Errorf("couldn't find %T owner for %T", owner, obj)
}
