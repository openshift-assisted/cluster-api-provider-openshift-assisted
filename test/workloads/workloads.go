/*
Copyright 2024.

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

package workloads

import (
	"fmt"
	"os/exec"

	"github.com/openshift-assisted/cluster-api-agent/test/utils"
)

const (
	prometheusOperatorVersion = "v0.68.0"
	prometheusOperatorURL     = "https://github.com/prometheus-operator/prometheus-operator/" +
		"releases/download/%s/bundle.yaml"

	certmanagerVersion = "v1.5.3"
	certmanagerURLTmpl = "https://github.com/jetstack/cert-manager/releases/download/%s/cert-manager.yaml"
)

func CAPM3(kubeconfig, verb string) error {
	url := "https://github.com/metal3-io/cluster-api-provider-metal3/config/default?ref=main"
	command := fmt.Sprintf("kubectl kustomize \"%s\" | kubectl --kubeconfig %s %s -f -", url, kubeconfig, verb)
	cmd := exec.Command("bash", "-c", command)
	if _, err := utils.Run(cmd); err != nil {
		return err
	}
	return nil
}

func CAPI(kubeconfig, verb string) error {
	url := "https://github.com/kubernetes-sigs/cluster-api-operator/releases/latest/download/operator-components.yaml"
	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig, verb, "-f", url)
	if _, err := utils.Run(cmd); err != nil {
		return err
	}
	return nil
}

func BMOIronic(kubeconfig, verb string) error {
	url := "https://github.com/jianzzha/sylva-poc/manifests/bmo?ref=main"
	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig, verb, "-k", url)
	if _, err := utils.Run(cmd); err != nil {
		return err
	}
	url = "https://github.com/jianzzha/sylva-poc/manifests/ironic?ref=main"
	cmd = exec.Command("kubectl", "--kubeconfig", kubeconfig, verb, "-k", url)
	if _, err := utils.Run(cmd); err != nil {
		return err
	}
	return nil
}
func UninstallBMOIronic(kubeconfig string) error {
	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig, "delete", "ns", "baremetal-operator-system")
	if _, err := utils.Run(cmd); err != nil {
		return err
	}
	return nil
}

// InstallPrometheusOperator installs the prometheus Operator to be used to export the enabled metrics.
func InstallPrometheusOperator() error {
	url := fmt.Sprintf(prometheusOperatorURL, prometheusOperatorVersion)
	cmd := exec.Command("kubectl", "create", "-f", url)
	_, err := utils.Run(cmd)
	return err
}

// UninstallPrometheusOperator uninstalls the prometheus
func UninstallPrometheusOperator() {
	url := fmt.Sprintf(prometheusOperatorURL, prometheusOperatorVersion)
	cmd := exec.Command("kubectl", "delete", "-f", url)
	if _, err := utils.Run(cmd); err != nil {
		utils.WarnError(err)
	}
}

// UninstallCertManager uninstalls the cert manager
func UninstallCertManager(kubeconfig string) error {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig, "delete", "-f", url, "--force", "--wait=false")
	if _, err := utils.Run(cmd); err != nil {
		return err
	}
	return nil
}
func AssistedServiceCRDs(kubeconfig, verb string) error {
	baseURL := "https://raw.githubusercontent.com/openshift/assisted-service/master/hack/crds/"
	CRDs := []string{
		"hive.openshift.io_clusterdeployments.yaml",
		"hive.openshift.io_clusterimagesets.yaml",
		"metal3.io_baremetalhosts.yaml",
		"metal3.io_preprovisioningimages.yaml",
	}
	for _, crd := range CRDs {
		crdFile := fmt.Sprintf("%s%s", baseURL, crd)
		cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig, verb, "-f", crdFile)
		if _, err := utils.Run(cmd); err != nil {
			return err
		}
	}
	return nil
}

func AssistedService(kubeconfig, verb string) error {
	url := "https://github.com/openshift/assisted-service/config/default?ref=master"
	command := fmt.Sprintf("kubectl %s \"%s\" | kubectl --kubeconfig %s apply -f -", verb, url, kubeconfig)
	cmd := exec.Command("bash", "-c", command)
	if _, err := utils.Run(cmd); err != nil {
		return err
	}
	return nil
}

func UninstallAssistedService(kubeconfig string) error {
	url := "https://github.com/openshift/assisted-service/config/default?ref=master"
	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig, "delete", "-k", url)
	if _, err := utils.Run(cmd); err != nil {
		return err
	}
	return nil
}

func AgentServiceConfig(kubeconfig, verb string) error {
	file := "../../hack/deploy/artifacts/agent-service-config.yaml"
	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig, verb, "-f", file)
	if _, err := utils.Run(cmd); err != nil {
		return err
	}
	return nil
}

func NginxIngress(kubeconfig, verb string) error {
	url := "https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml"
	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig, verb, "-f", url)
	if _, err := utils.Run(cmd); err != nil {
		return err
	}
	return nil
}

func BootstrapProvider(kubeconfig, verb string) error {
	url := "https://raw.githubusercontent.com/openshift-assisted/cluster-api-agent/master/bootstrap-components.yaml"
	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig, verb, "-f", url)
	if _, err := utils.Run(cmd); err != nil {
		return err
	}
	return nil
}

func ControlPlaneProvider(kubeconfig, verb string) error {
	url := "https://raw.githubusercontent.com/openshift-assisted/cluster-api-agent/master/controlplane-components.yaml"
	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig, verb, "-f", url)
	if _, err := utils.Run(cmd); err != nil {
		return err
	}
	return nil
}

// InstallCertManager installs the cert manager bundle.
func InstallCertManager(kubeconfig string) error {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.Command("kubectl", "--kubeconfig", kubeconfig, "apply", "-f", url)
	if _, err := utils.Run(cmd); err != nil {
		return err
	}
	// Wait for cert-manager-webhook to be ready, which can take time if cert-manager
	// was re-installed after uninstalling on a cluster.
	cmd = exec.Command("kubectl", "wait", "deployment.apps/cert-manager-webhook",
		"--for", "condition=Available",
		"--namespace", "cert-manager",
		"--timeout", "5m",
	)

	_, err := utils.Run(cmd)
	return err
}
