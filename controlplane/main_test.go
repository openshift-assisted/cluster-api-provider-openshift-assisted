package main

import (
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

func TestSchemeIncludesOpenShiftRoute(t *testing.T) {
	gvk, err := apiutil.GVKForObject(&routev1.Route{}, scheme)
	if err != nil {
		t.Fatalf("route type is not registered in the manager scheme: %v", err)
	}
	expected := schema.GroupVersionKind{Group: "route.openshift.io", Version: "v1", Kind: "Route"}
	if gvk != expected {
		t.Fatalf("unexpected route GVK: got %s, want %s", gvk, expected)
	}
}
