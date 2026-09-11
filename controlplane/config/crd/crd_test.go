package crd_test

import (
	"bytes"
	_ "embed"
	"io"
	"os"
	"path/filepath"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const (
	machineNetworkMaxCIDRLength = int64(43)
)

//go:embed bases/controlplane.cluster.x-k8s.io_openshiftassistedcontrolplanes.yaml
var controlPlaneCRD []byte

func TestGeneratedControlPlaneCRDSchema(t *testing.T) {
	crd := decodeCRD(t)

	for _, version := range crd.Spec.Versions {
		t.Run("has bounded machine network for "+version.Name, func(t *testing.T) {
			if version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
				t.Fatal("CRD version schema is not set")
			}

			machineNetwork := crdSchemaProperty(t, version.Schema.OpenAPIV3Schema, "spec", "config", "machineNetwork")
			if machineNetwork.MaxItems == nil {
				t.Fatal("machineNetwork.maxItems is not set")
			}
			if *machineNetwork.MaxItems <= 0 {
				t.Fatalf("machineNetwork.maxItems = %d, want a positive bound", *machineNetwork.MaxItems)
			}
			if machineNetwork.Items == nil || machineNetwork.Items.Schema == nil {
				t.Fatal("machineNetwork.items schema is not set")
			}

			cidr := crdSchemaProperty(t, machineNetwork.Items.Schema, "cidr")
			if cidr.MaxLength == nil {
				t.Fatal("machineNetwork.items.properties.cidr.maxLength is not set")
			}
			if *cidr.MaxLength != machineNetworkMaxCIDRLength {
				t.Fatalf("cidr.maxLength = %d, want %d", *cidr.MaxLength, machineNetworkMaxCIDRLength)
			}
		})
	}
}

func TestGeneratedCRDsCanBeInstalled(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS is not set")
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			"bases",
			filepath.Join("..", "..", "..", "bootstrap", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}
	_, err := testEnv.Start()
	if err != nil {
		_ = testEnv.Stop()
		t.Fatalf("install generated CRD: %v", err)
	}
	t.Cleanup(func() {
		if err := testEnv.Stop(); err != nil {
			t.Errorf("stop envtest: %v", err)
		}
	})
}

func decodeCRD(t *testing.T) *apiextensionsv1.CustomResourceDefinition {
	t.Helper()

	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(controlPlaneCRD), 4096)
	var crd apiextensionsv1.CustomResourceDefinition
	if err := decoder.Decode(&crd); err != nil {
		t.Fatalf("decode CRD: %v", err)
	}

	var extra apiextensionsv1.CustomResourceDefinition
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("expected one CRD document, got additional document or decode error: %v", err)
	}

	return &crd
}

func crdSchemaProperty(t *testing.T, schema *apiextensionsv1.JSONSchemaProps, path ...string) *apiextensionsv1.JSONSchemaProps {
	t.Helper()
	if schema == nil {
		t.Fatalf("CRD schema is nil before property %q", path[0])
	}

	for _, property := range path {
		next, ok := schema.Properties[property]
		if !ok {
			t.Fatalf("CRD schema property %q not found", property)
		}
		schema = &next
	}

	return schema
}
