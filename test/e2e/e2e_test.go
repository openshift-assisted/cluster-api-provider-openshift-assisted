package e2e

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-assisted/cluster-api-agent/test/utils"
	"github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aimodels "github.com/openshift/assisted-service/models"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"libvirt.org/go/libvirt"
	clusterctlclient "sigs.k8s.io/cluster-api/cmd/clusterctl/client"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/tree"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	namespace = "test-capi"

	libvirtnetNameserver = "192.168.222.1"
	defaultNameserver    = "8.8.8.8"
	clusterName          = "test-multinode"
)

var _ = Describe("Libvirt Test", func() {
	var (
		ctx              = context.Background()
		username         = os.Getenv("REMOTE_USERNAME")
		hostname         = os.Getenv("REMOTE_HOSTNAME")
		networkInterface = os.Getenv("REMOTE_HOST_NETWORK_INTERFACE")

		conn                   *libvirt.Connect
		network                *libvirt.Network
		k8sClient              client.Client
		installClusterManifest string = "./examples/multi-node-example.yaml"
		multinodeHostNumber    int    = 5
		vars                   map[string]string
	)
	BeforeEach(func() {
		vars = map[string]string{
			"<SSH_AUTHORIZED_KEY>": os.Getenv("SSH_AUTHORIZED_KEY"),
			"<PULLSECRET>":         os.Getenv("PULLSECRET"),
		}
		var err error
		uri := fmt.Sprintf("qemu+ssh://%s@%s/system", username, hostname)
		conn, err = libvirt.NewConnect(uri)
		if err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] ERROR setting up libvirt connection\n")
		}
		Expect(err).ToNot(HaveOccurred())
		scheme := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())

		Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())

		Expect(v1beta1.AddToScheme(scheme)).To(Succeed())

		cfg, err := config.GetConfig()
		if err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] Error getting k8s config: %s", err.Error())
		}
		k8sClient, err = client.New(cfg, client.Options{
			Scheme: scheme,
		})
		if err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] ERROR setting up client\n")
		}
		Expect(err).ToNot(HaveOccurred())

		// cleanup
		_ = CleanupSushyTools(username, hostname)
		_ = CleanNetworks(conn)
		_ = CleanupDomains(conn)

		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] Setting up DNS server")
		Expect(SetDNS(username, hostname, networkInterface, defaultNameserver)).ToNot(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "OK\n")
		// setup infra workloads
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] Installing cert-manager...")
		Expect(utils.InstallCertManager()).To(Succeed())
		_, _ = fmt.Fprintf(GinkgoWriter, "OK\n")

		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] Installing ironic...")
		Expect(InstallIronic()).To(Succeed())
		_, _ = fmt.Fprintf(GinkgoWriter, "OK\n")
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] Installing BMO...")
		Expect(InstallBMO()).To(Succeed())
		_, _ = fmt.Fprintf(GinkgoWriter, "OK\n")
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] Installing MetalLB...")
		Expect(InstallMetalLB()).To(Succeed())
		_, _ = fmt.Fprintf(GinkgoWriter, "OK\n")

		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] Installing Ingress-NGINX...")
		ingressIP, err := InstallIngressNginx(ctx, k8sClient)
		Expect(err).NotTo(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "OK\n")

		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] Installing infrastructure-operator...")
		Expect(InstallInfrastructureOperator(ctx, k8sClient)).To(Succeed())
		_, _ = fmt.Fprintf(GinkgoWriter, "OK\n")

		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] Installing CORE-CAPI")
		Expect(InstallCoreCAPI()).To(Succeed())
		_, _ = fmt.Fprintf(GinkgoWriter, "OK\n")

		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] Installing CAPI providers")
		Expect(InstallCAPIProviders()).To(Succeed())
		_, _ = fmt.Fprintf(GinkgoWriter, "OK\n")

		// infra workloads ready

		// Setup Network
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] Setting up Network")
		network, err = SetupLibvirtNetwork(conn, ingressIP, defaultNameserver, multinodeHostNumber)
		Expect(err).NotTo(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "OK\n")
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] Setting up DNS server")
		Expect(SetDNS(username, hostname, networkInterface, libvirtnetNameserver)).ToNot(HaveOccurred())
		_, _ = fmt.Fprintf(GinkgoWriter, "OK\n")
		// spin up sushy tools
		_, _ = fmt.Fprintf(GinkgoWriter, "[DEBUG] Setting up SushyTools")
		Expect(SetupSushyTools(username, hostname)).To(Succeed())
		_, _ = fmt.Fprintf(GinkgoWriter, "OK\n")
	})
	AfterEach(func() {
		_, _ = fmt.Fprintf(GinkgoWriter, "network %v", network)

		ns := v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		}
		Expect(k8sClient.Delete(ctx, &ns)).To(Succeed())

		ns = v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "nginx-ingress"},
		}
		Expect(k8sClient.Delete(ctx, &ns)).To(Succeed())

		_ = CleanupSushyTools(username, hostname)
		_ = CleanNetworks(conn)
		_ = CleanupDomains(conn)
	})
	It("libvirt go", func() {
		//kind create cluster --config kind.yaml --name openshift-capi
		// TODO: if KUBECONFIG not provided, setup kind into remote host with default config

		ns := v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		// we don't really care if the namespace is already existing
		_ = k8sClient.Create(ctx, &ns)

		Expect(CreateVMsAndBMHs(ctx, k8sClient, username, hostname, namespace, multinodeHostNumber)).To(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "Checking all BMHs are in available provisioning state...")
		Expect(
			waitForAvailableBMHs(ctx, k8sClient, namespace),
		).To(Succeed())
		_, _ = fmt.Fprintf(GinkgoWriter, "[OK]\n")

		Expect(InstallExampleCluster(installClusterManifest, namespace, vars)).To(Succeed())

		_, _ = fmt.Fprintf(GinkgoWriter, "Checking ACI is in installing state...")
		Expect(
			waitForAgentClusterInstall(
				ctx,
				k8sClient,
				clusterName,
				namespace,
				aimodels.ClusterStatusInstalling,
				time.Second*600,
			),
		).To(Succeed())
		_, _ = fmt.Fprintf(GinkgoWriter, "[OK]\n")

		_, _ = fmt.Fprintf(GinkgoWriter, "Checking ACI is in installed state...")
		Expect(
			waitForAgentClusterInstall(
				ctx,
				k8sClient,
				clusterName,
				namespace,
				aimodels.ClusterStatusAddingHosts,
				time.Second*3600,
			),
		).To(Succeed())
		_, _ = fmt.Fprintf(GinkgoWriter, "[OK]\n")

		Expect(isClusterInstalled(ctx, clusterName, namespace)).To(Succeed())
	})
})

func isClusterInstalled(ctx context.Context, clusterName, namespace string) error {
	condition := func(ctx context.Context) (bool, error) {
		c, err := clusterctlclient.New(ctx, "")
		if err != nil {
			return true, err
		}
		dc := clusterctlclient.DescribeClusterOptions{
			Namespace:           namespace,
			ClusterName:         clusterName,
			ShowOtherConditions: "all",
			Grouping:            false,
		}
		objTree, err := c.DescribeCluster(ctx, dc)
		if err != nil {
			return false, err
		}
		return checkTree(objTree, objTree.GetRoot()), nil
	}
	interval := time.Second * 2
	timeout := time.Second * 60
	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, condition)
}

func checkTree(objectTree *tree.ObjectTree, obj client.Object) bool {
	cond := tree.GetReadyCondition(obj)
	if cond != nil && cond.Status != "True" {
		return false
	}
	otherConditions := tree.GetOtherConditions(obj)
	for _, cond := range otherConditions {
		if cond != nil && cond.Status != "True" {
			return false
		}
	}
	childrenObj := objectTree.GetObjectsByParent(obj.GetUID())
	for _, childObj := range childrenObj {
		if !checkTree(objectTree, childObj) {
			return false
		}
	}
	return true
}

func InstallExampleCluster(manifest, namespace string, vars map[string]string) error {
	outFile, err := os.CreateTemp("/tmp", "cluster-manifest")
	if err != nil {
		log.Fatal(err)
	}
	//nolint:all
	defer os.Remove(outFile.Name())
	input, err := os.ReadFile(manifest)
	if err != nil {
		log.Fatalln(err)
	}

	lines := strings.Split(string(input), "\n")

	for i, line := range lines {
		for key, value := range vars {
			if strings.Contains(line, key) {
				lines[i] = strings.Replace(line, key, value, -1)
			}
		}
	}
	output := strings.Join(lines, "\n")
	err = os.WriteFile(outFile.Name(), []byte(output), 0644)
	if err != nil {
		log.Fatalln(err)
	}

	cmd := exec.Command("kubectl", "-n", namespace, "apply", "-f", outFile.Name())
	_, err = cmd.CombinedOutput()
	Expect(err).NotTo(HaveOccurred())

	if err != nil {
		return err
	}
	Expect(err).NotTo(HaveOccurred())
	return nil
}
