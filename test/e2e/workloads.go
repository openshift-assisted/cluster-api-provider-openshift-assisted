package e2e

import (
	"context"
	"fmt"
	"time"

	"github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func InstallMetalLB() error {
	cmd := exec.Command(
		"kubectl", "apply",
		"-f", "https://raw.githubusercontent.com/metallb/metallb/v0.13.7/config/manifests/metallb-native.yaml",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] error applying metallb: %s", out)
		return err
	}

	cmd = exec.Command(
		"kubectl", "wait",
		"--for=condition=available", "deployment/controller",
		"--timeout=300s",
		"-n", "metallb-system",
	)
	out, err = cmd.CombinedOutput()
	if err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] error applying metallb: %s", out)
		return err
	}

	metallbIpPoolPath := "./test/e2e/manifests/metallb"
	cmd = exec.Command("kubectl", "apply", "-f", metallbIpPoolPath)
	out, err = cmd.CombinedOutput()
	if err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] error applying metallb: %s", out)
		return err
	}

	return nil
}

func InstallInfrastructureOperator(ctx context.Context, k8sClient client.Client) error {

	infrastructureOperatorPath := "./test/e2e/manifests/infrastructure-operator"
	cmd := exec.Command("kubectl", "apply", "-k", infrastructureOperatorPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] error applying infrastructure operator manifest: %s", string(out))
		return err
	}
	_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] waiting for assisted-service deployment")
	err = waitForDeployment(ctx, k8sClient, "assisted-installer", "assisted-service")
	if err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] error waiting for assisted: %s", string(out))
		return err
	}
	_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] waiting for assisted-image-service statefulset")
	cmd = exec.Command(
		"kubectl", "wait",
		"--for=condition=ready", "pod/assisted-image-service-0",
		"--timeout=600s",
		"-n", "assisted-installer",
	)
	out, err = cmd.CombinedOutput()
	if err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] error waiting for assisted: %s", string(out))
		return err
	}
	return nil
}

func InstallIngressNginx(ctx context.Context, k8sClient client.Client) (string, error) {

	ingressManifestPath := "./test/e2e/manifests/ingress-nginx"
	cmd := exec.Command("kubectl", "create", "ns", "nginx-ingress")
	out, err := cmd.CombinedOutput()
	if err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] failed to create namespace nginx-ingress: %s", string(out))
	}

	cmd = exec.Command("kubectl", "-n", "nginx-ingress", "apply", "-f", ingressManifestPath)
	out, err = cmd.CombinedOutput()
	if err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] failed applying ingress nginx: %s", string(out))
		return "", err
	}

	err = waitForLoadBalancerIP(ctx, k8sClient, "nginx-ingress", "ingress-nginx-controller")
	if err != nil {
		return "", err
	}
	svc := corev1.Service{}
	err = k8sClient.Get(ctx, types.NamespacedName{
		Namespace: "nginx-ingress",
		Name:      "ingress-nginx-controller",
	}, &svc)
	if err != nil {
		return "", err
	}
	ip := svc.Status.LoadBalancer.Ingress[0].IP
	return ip, nil
}

func waitForLoadBalancerIP(ctx context.Context, k8sClient client.Client, name string, namespace string) error {
	timeout := time.Second * 300
	interval := time.Second * 10
	condition := func(ctx context.Context) (bool, error) {
		svc := corev1.Service{}
		if err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: name,
			Name:      namespace,
		}, &svc); err != nil {
			return false, nil
		}
		if len(svc.Status.LoadBalancer.Ingress) > 0 {
			if svc.Status.LoadBalancer.Ingress[0].IP != "" {
				return true, nil
			}
		}

		return false, nil
	}
	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, condition)
}

func waitForDeployment(ctx context.Context, k8sClient client.Client, name string, namespace string) error {
	timeout := time.Second * 300
	interval := time.Second * 10
	condition := func(ctx context.Context) (bool, error) {
		deploy := appsv1.Deployment{}
		if err := k8sClient.Get(ctx, types.NamespacedName{
			Namespace: name,
			Name:      namespace,
		}, &deploy); err != nil {
			return false, nil
		}
		if deploy.Spec.Replicas != nil && *deploy.Spec.Replicas == deploy.Status.ReadyReplicas {
			return true, nil
		}
		return false, nil
	}
	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, condition)
}

func InstallIronic() error {

	ironicManifestPath := "./test/e2e/manifests/ironic"
	cmd := exec.Command("kubectl", "apply", "-k", ironicManifestPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] failed applying ironic: %s", string(out))
		return err
	}

	cmd = exec.Command(
		"kubectl", "wait",
		"--for=condition=available", "deployment/ironic",
		"--timeout=600s",
		"-n", "baremetal-operator-system",
	)
	out, err = cmd.CombinedOutput()
	if err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] error waiting for ironic deployment: %s", out)
	}

	return err
}

func InstallBMO() error {
	bmoManifestPath := "./test/e2e/manifests/bmo"
	cmd := exec.Command("kubectl", "apply", "-k", bmoManifestPath)
	_, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	cmd = exec.Command(
		"kubectl", "wait",
		"--for=condition=available", "deployment/baremetal-operator-controller-manager",
		"--timeout=600s",
		"-n", "baremetal-operator-system",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] error waiting for bmo deployment: %s", out)
		}
	}
	return nil
}

func waitForAgentClusterInstall(
	ctx context.Context,
	k8sClient client.Client,
	name, namespace, status string,
	timeout time.Duration,
) error {
	aci := v1beta1.AgentClusterInstall{}
	condition := func(ctx context.Context) (bool, error) {
		// ACI will be created by controlplane manager, won't be available right away
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &aci); err != nil {
			return false, nil
		}
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] ACI state: %s\n", aci.Status.DebugInfo.State)
		if aci.Status.DebugInfo.State == status {
			return true, nil
		}
		return false, nil
	}
	interval := time.Second * 10
	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, condition)
}

func waitForAvailableBMHs(ctx context.Context, k8sClient client.Client, namespace string) error {
	bmhs := v1alpha1.BareMetalHostList{}
	condition := func(ctx context.Context) (bool, error) {
		if err := k8sClient.List(ctx, &bmhs, client.InNamespace(namespace)); err != nil {
			return false, nil
		}
		for _, bmh := range bmhs.Items {
			if bmh.Status.Provisioning.State != v1alpha1.StateAvailable {
				return false, nil
			}
		}
		return true, nil
	}
	interval := time.Second * 2
	timeout := time.Second * 60
	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, condition)
}

func InstallCAPIBootstrapProvider() error {
	cmd := exec.Command("kubectl", "apply", "-f", "./dist/bootstrap_install.yaml")
	_, err := cmd.CombinedOutput()
	return err
}

func InstallCAPIControlPlaneProvider() error {
	cmd := exec.Command("kubectl", "apply", "-f", "./dist/controlplane_install.yaml")
	_, err := cmd.CombinedOutput()
	return err
}

func InstallCAPIProviders() error {
	if err := InstallCAPIBootstrapProvider(); err != nil {
		return err
	}
	if err := InstallCAPIControlPlaneProvider(); err != nil {
		return err
	}
	return nil
}

func InstallCoreCAPI() error {
	cmd := exec.Command(
		"clusterctl", "init",
		"--core", "cluster-api:v1.7.1",
		"--bootstrap", "-",
		"--control-plane", "-",
		"--infrastructure", "metal3:v1.6.0",
	)
	_, err := cmd.CombinedOutput()
	return err
}
