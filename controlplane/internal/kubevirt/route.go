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

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	routeGVK             = schema.GroupVersionKind{Group: "route.openshift.io", Version: "v1", Kind: "Route"}
	ingressControllerGVK = schema.GroupVersionKind{Group: "operator.openshift.io", Version: "v1", Kind: "IngressController"}
)

// EnsureExternalRoutes creates passthrough OpenShift Routes on the infra cluster
// for the tenant cluster's API and ingress traffic.
//
// The API Route forwards SNI-matched traffic on port 443 to the API service
// (targetPort 6443). The wildcard ingress Route forwards
// *.apps.<cluster>.<baseDomain> to the ingress service.
//
// This is used in pod-networking mode where baseDomain is a subdomain of the
// infra cluster's apps domain.
func EnsureExternalRoutes(
	ctx context.Context,
	c client.Client,
	oacp *controlplanev1alpha3.OpenshiftAssistedControlPlane,
	clusterName string,
	namespace string,
) error {
	log := ctrl.LoggerFrom(ctx)

	baseDomain := oacp.Spec.Config.BaseDomain
	apiHost := fmt.Sprintf("api.%s.%s", clusterName, baseDomain)
	appsHost := fmt.Sprintf("apps.%s.%s", clusterName, baseDomain)
	apiServiceName := clusterName + APIServiceNameSuffix
	ingressServiceName := clusterName + IngressServiceNameSuffix

	apiRoute, err := buildPassthroughRoute(
		clusterName+"-api-route",
		namespace,
		apiHost,
		apiServiceName,
		6443,
		"",
	)
	if err != nil {
		return fmt.Errorf("failed to build API route: %w", err)
	}
	if err := ensureRoute(ctx, c, apiRoute); err != nil {
		return fmt.Errorf("failed to ensure API route: %w", err)
	}
	log.Info("ensured API passthrough route", "host", apiHost)

	ingressRoute, err := buildPassthroughRoute(
		clusterName+"-ingress-route",
		namespace,
		"wildcard."+appsHost,
		ingressServiceName,
		443,
		"Subdomain",
	)
	if err != nil {
		return fmt.Errorf("failed to build ingress route: %w", err)
	}
	if err := ensureRoute(ctx, c, ingressRoute); err != nil {
		return fmt.Errorf("failed to ensure ingress wildcard route: %w", err)
	}
	log.Info("ensured ingress wildcard passthrough route", "host", "*."+appsHost)

	return nil
}

// EnsureIngressControllerWildcardPolicy patches the default IngressController to allow
// wildcard Routes (required for the *.apps subdomain passthrough).
func EnsureIngressControllerWildcardPolicy(ctx context.Context, c client.Client) error {
	log := ctrl.LoggerFrom(ctx)

	ic := &unstructured.Unstructured{}
	ic.SetGroupVersionKind(ingressControllerGVK)

	err := c.Get(ctx, types.NamespacedName{Name: "default", Namespace: "openshift-ingress-operator"}, ic)
	if err != nil {
		return fmt.Errorf("failed to get default IngressController: %w", err)
	}

	routeAdmission, _, _ := unstructured.NestedMap(ic.Object, "spec", "routeAdmission")
	if routeAdmission != nil {
		if policy, ok := routeAdmission["wildcardPolicy"]; ok && policy == "WildcardsAllowed" {
			log.V(1).Info("IngressController already allows wildcard routes")
			return nil
		}
	}

	patch := []byte(`{"spec":{"routeAdmission":{"wildcardPolicy":"WildcardsAllowed"}}}`)
	patchObj := &unstructured.Unstructured{}
	patchObj.SetGroupVersionKind(ingressControllerGVK)
	patchObj.SetName("default")
	patchObj.SetNamespace("openshift-ingress-operator")

	if err := c.Patch(ctx, patchObj, client.RawPatch(types.MergePatchType, patch)); err != nil {
		return fmt.Errorf("failed to patch IngressController wildcard policy: %w", err)
	}

	log.Info("patched IngressController to allow wildcard routes")
	return nil
}

func buildPassthroughRoute(name, namespace, host, serviceName string, targetPort int, wildcardPolicy string) (*unstructured.Unstructured, error) {
	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(routeGVK)
	route.SetName(name)
	route.SetNamespace(namespace)

	spec := map[string]interface{}{
		"host": host,
		"to": map[string]interface{}{
			"kind": "Service",
			"name": serviceName,
		},
		"port": map[string]interface{}{
			"targetPort": int64(targetPort),
		},
		"tls": map[string]interface{}{
			"termination": "passthrough",
		},
	}

	if wildcardPolicy != "" {
		spec["wildcardPolicy"] = wildcardPolicy
	}

	if err := unstructured.SetNestedMap(route.Object, spec, "spec"); err != nil {
		return nil, fmt.Errorf("failed to build route spec for %s: %w", name, err)
	}

	return route, nil
}

func ensureRoute(ctx context.Context, c client.Client, desired *unstructured.Unstructured) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(routeGVK)

	err := c.Get(ctx, types.NamespacedName{
		Name:      desired.GetName(),
		Namespace: desired.GetNamespace(),
	}, existing)

	if errors.IsNotFound(err) {
		return c.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	desiredSpec, _, _ := unstructured.NestedMap(desired.Object, "spec")
	for _, field := range []string{"host", "to", "port", "tls", "wildcardPolicy"} {
		if val, ok := desiredSpec[field]; ok {
			_ = unstructured.SetNestedField(existing.Object, val, "spec", field)
		}
	}
	return c.Update(ctx, existing)
}
