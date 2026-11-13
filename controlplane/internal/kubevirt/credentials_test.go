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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	controlplanev1alpha3 "github.com/openshift-assisted/cluster-api-provider-openshift-assisted/controlplane/api/v1alpha3"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func mustMarshalInfraCredentialsRecord(record *infraCredentialsRoleBindingRecord) string {
	value, err := record.marshal()
	ExpectWithOffset(1, err).To(Succeed())
	return value
}

var _ = Describe("EnsureInfraCredentialsManifests", func() {
	const sourceToken = "eyJhbGciOiJub25lIn0.eyJzdWIiOiJzeXN0ZW06c2VydmljZWFjY291bnQ6dGVzdC1uczppbmZyYS11c2VyIn0.signature"

	sourceKubeconfigForServer := func(token, server, certificateAuthorityData string) string {
		cluster := "    server: " + server + "\n"
		if certificateAuthorityData != "" {
			cluster += "    certificate-authority-data: " + certificateAuthorityData + "\n"
		}

		return `apiVersion: v1
kind: Config
clusters:
- cluster:
` + cluster + `  name: infra
  name: infra
contexts:
- context:
    cluster: infra
    user: infra
  name: infra
current-context: infra
users:
- name: infra
  user:
    token: ` + token + "\n"
	}

	newIdentityServer := func(token, username string) (*httptest.Server, string) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+token {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			review := &authenticationv1.SelfSubjectReview{}
			review.Status.UserInfo.Username = username
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(review); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		}))
		caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
		kubeconfig := sourceKubeconfigForServer(
			token,
			server.URL,
			base64.StdEncoding.EncodeToString(caPEM),
		)
		return server, kubeconfig
	}

	newCertificateIdentityServer := func(impersonatedUser string) (*httptest.Server, string) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			username := r.Header.Get("Impersonate-User")
			if username == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			review := &authenticationv1.SelfSubjectReview{}
			review.Status.UserInfo.Username = username
			w.Header().Set("Content-Type", "application/json")
			Expect(json.NewEncoder(w).Encode(review)).To(Succeed())
		}))
		caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})

		privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		Expect(err).NotTo(HaveOccurred())
		template := &x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject:      pkix.Name{CommonName: "certificate-common-name"},
			DNSNames:     []string{"client.example.com"},
		}
		certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
		Expect(err).NotTo(HaveOccurred())
		clientCertificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
		privateKeyDER, err := x509.MarshalECPrivateKey(privateKey)
		Expect(err).NotTo(HaveOccurred())
		clientKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})

		kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: %s
    certificate-authority-data: %s
  name: infra
contexts:
- context:
    cluster: infra
    user: infra
  name: infra
current-context: infra
users:
- name: infra
  user:
    as: %s
    client-certificate-data: %s
    client-key-data: %s
`, server.URL, base64.StdEncoding.EncodeToString(caPEM), impersonatedUser, base64.StdEncoding.EncodeToString(clientCertificate), base64.StdEncoding.EncodeToString(clientKey))
		return server, kubeconfig
	}

	var (
		ctx                context.Context
		scheme             *runtime.Scheme
		oacp               *controlplanev1alpha3.OpenshiftAssistedControlPlane
		source             *corev1.Secret
		kubernetes         *corev1.Service
		identityServer     *httptest.Server
		identityKubeconfig string
		sourceKubeconfig   func(token string) string
		newInfraClient     func(objects ...client.Object) client.Client
		newInfraResult     func(client.Client, string, ...string) *InfraClientResult
	)

	sourceKubeconfigWithTLSName := func(token, tlsServerName string) string {
		return strings.Replace(
			sourceKubeconfig(token),
			"  name: infra\n",
			"    tls-server-name: "+tlsServerName+"\n  name: infra\n",
			1,
		)
	}

	withInfraKubeconfigNamespace := func(kubeconfigData string) string {
		return strings.Replace(
			kubeconfigData,
			"    cluster: infra\n    user: infra\n",
			"    cluster: infra\n    user: infra\n    namespace: infra-ns\n",
			1,
		)
	}

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		utilruntime.Must(corev1.AddToScheme(scheme))
		utilruntime.Must(rbacv1.AddToScheme(scheme))
		utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))

		identityServer, identityKubeconfig = newIdentityServer(sourceToken, "system:serviceaccount:test-ns:infra-user")
		DeferCleanup(identityServer.Close)
		sourceKubeconfig = func(token string) string {
			Expect(token).To(Equal(sourceToken))
			return identityKubeconfig
		}

		oacp = &controlplanev1alpha3.OpenshiftAssistedControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-ns",
				UID:       "test-uid",
			},
			Spec: controlplanev1alpha3.OpenshiftAssistedControlPlaneSpec{
				Config: controlplanev1alpha3.OpenshiftAssistedControlPlaneConfigSpec{
					CloudControllerManager: &controlplanev1alpha3.CCMSpec{Enabled: true},
				},
			},
		}
		source = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "infra-cluster-credentials", Namespace: "test-ns"},
			Data:       map[string][]byte{"kubeconfig": []byte(sourceKubeconfig(sourceToken))},
		}
		kubernetes = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "kubernetes", Namespace: "default"},
			Spec: corev1.ServiceSpec{
				ClusterIP: "172.30.0.1",
				Ports:     []corev1.ServicePort{{Port: 443}},
			},
		}

		newInfraClient = func(objects ...client.Object) client.Client {
			return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
		}
		newInfraResult = func(infraClient client.Client, namespace string, kubeconfigData ...string) *InfraClientResult {
			if len(kubeconfigData) > 0 {
				target, err := kubeconfigClusterTarget(kubeconfigData[0])
				Expect(err).NotTo(HaveOccurred())
				return &InfraClientResult{
					Client:          infraClient,
					InfraClusterRef: "secret/infra-cluster-credentials",
					Namespace:       namespace,
					Target:          target,
					IsRemote:        true,
				}
			}
			return &InfraClientResult{
				Client:          infraClient,
				InfraClusterRef: localInfraClusterTarget,
				Namespace:       namespace,
				Target:          localInfraClusterTarget,
				IsRemote:        false,
			}
		}
	})

	It("rewrites pod-networking credentials to the infra Kubernetes service", func() {
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, source).Build()
		infraClient := newInfraClient(kubernetes)

		err := EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, oacp.Namespace), oacp)
		Expect(err).NotTo(HaveOccurred())

		credentialsConfigMap := &corev1.ConfigMap{}
		err = sourceClient.Get(ctx, client.ObjectKey{Name: InfraCredentialsCMName, Namespace: "test-ns"}, credentialsConfigMap)
		Expect(err).NotTo(HaveOccurred())
		Expect(credentialsConfigMap.Data["01-ccm-credentials-secret.yaml"]).To(ContainSubstring("server: https://172.30.0.1:443"))
		Expect(credentialsConfigMap.Data["01-ccm-credentials-secret.yaml"]).To(ContainSubstring("tls-server-name: 127.0.0.1"))

		role := &rbacv1.Role{}
		err = infraClient.Get(ctx, client.ObjectKey{Name: "kubevirt-cloud-controller-manager", Namespace: "test-ns"}, role)
		Expect(err).NotTo(HaveOccurred())
		Expect(role.Rules).To(ContainElement(rbacv1.PolicyRule{
			APIGroups: []string{"discovery.k8s.io"},
			Resources: []string{"endpointslices"},
			Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
		}))
		Expect(role.Rules).To(ContainElement(rbacv1.PolicyRule{
			APIGroups: []string{"kubevirt.io"},
			Resources: []string{"virtualmachineinstances"},
			Verbs:     []string{"get", "list", "watch"},
		}))
		roleBinding := &rbacv1.RoleBinding{}
		err = infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(oacp), Namespace: "test-ns"}, roleBinding)
		Expect(err).NotTo(HaveOccurred())
		currentOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: oacp.Name, Namespace: oacp.Namespace}, currentOACP)).To(Succeed())
		Expect(currentOACP.Annotations).To(HaveKey(infraCredentialsRoleBindingAnnotation))
		record, err := parseInfraCredentialsRoleBindingRecord(currentOACP.Annotations[infraCredentialsRoleBindingAnnotation])
		Expect(err).NotTo(HaveOccurred())
		Expect(record.InfraClusterRef).To(Equal(localInfraClusterTarget))
		Expect(record.InfraNamespace).To(Equal("test-ns"))
		Expect(record.RoleBindingName).To(Equal(infraRoleBindingName(oacp)))
		Expect(record.InfraClusterTarget).NotTo(BeEmpty())
		Expect(roleBinding.Subjects).To(Equal([]rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      "infra-user",
			Namespace: "test-ns",
		}}))
	})

	It("uses the infra API identity for opaque bearer tokens", func() {
		opaqueIdentityServer, opaqueKubeconfig := newIdentityServer("opaque-token", "system:serviceaccount:other-ns:other-user")
		DeferCleanup(opaqueIdentityServer.Close)
		source.Data["kubeconfig"] = []byte(opaqueKubeconfig)
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, source).Build()
		infraClient := newInfraClient(kubernetes)

		err := EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns"), oacp)
		Expect(err).NotTo(HaveOccurred())

		roleBinding := &rbacv1.RoleBinding{}
		err = infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(oacp), Namespace: "infra-ns"}, roleBinding)
		Expect(err).NotTo(HaveOccurred())
		Expect(roleBinding.Subjects).To(Equal([]rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      "other-user",
			Namespace: "other-ns",
		}}))
	})

	It("keeps a separate RoleBinding for each workload cluster", func() {
		secondOACP := oacp.DeepCopy()
		secondOACP.Name = "second-cluster"
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, secondOACP, source).Build()
		infraClient := newInfraClient(kubernetes)

		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns"), oacp)).To(Succeed())
		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns"), secondOACP)).To(Succeed())

		firstBinding := &rbacv1.RoleBinding{}
		Expect(infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(oacp), Namespace: "infra-ns"}, firstBinding)).To(Succeed())
		secondBinding := &rbacv1.RoleBinding{}
		Expect(infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(secondOACP), Namespace: "infra-ns"}, secondBinding)).To(Succeed())
		Expect(firstBinding.Name).NotTo(Equal(secondBinding.Name))
		Expect(firstBinding.Subjects).To(Equal(secondBinding.Subjects))
	})

	It("removes the previous RoleBinding when the infra namespace changes", func() {
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, source).Build()
		infraClient := newInfraClient(kubernetes)
		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns"), oacp)).To(Succeed())

		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "new-infra-ns"), oacp)).To(Succeed())

		oldRoleBinding := &rbacv1.RoleBinding{}
		err := infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(oacp), Namespace: "infra-ns"}, oldRoleBinding)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
		newRoleBinding := &rbacv1.RoleBinding{}
		Expect(infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(oacp), Namespace: "new-infra-ns"}, newRoleBinding)).To(Succeed())

		currentOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: oacp.Name, Namespace: oacp.Namespace}, currentOACP)).To(Succeed())
		record, err := parseInfraCredentialsRoleBindingRecord(currentOACP.Annotations[infraCredentialsRoleBindingAnnotation])
		Expect(err).NotTo(HaveOccurred())
		Expect(record.InfraNamespace).To(Equal("new-infra-ns"))
	})

	It("builds the recorded remote infra client from its source namespace", func() {
		remoteSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "recorded-infra", Namespace: "test-ns"},
			Data:       map[string][]byte{"kubeconfig": []byte(identityKubeconfig)},
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(remoteSecret).Build()
		target, err := kubeconfigClusterTarget(identityKubeconfig)
		Expect(err).NotTo(HaveOccurred())
		record := &infraCredentialsRoleBindingRecord{
			InfraClusterRef:    "secret/recorded-infra",
			InfraClusterTarget: target,
			InfraNamespace:     "infra-ns",
			RoleBindingName:    infraRoleBindingName(oacp),
		}

		currentOACP := oacp.DeepCopy()
		currentOACP.Spec.Config.InfraClusterRef = &corev1.LocalObjectReference{Name: "new-infra"}
		recordedClient, err := infraClientForRecord(ctx, sourceClient, currentOACP, record, nil, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(recordedClient).NotTo(BeNil())
	})

	It("refuses role binding cleanup after the recorded secret points to another cluster", func() {
		remoteSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "recorded-infra", Namespace: "test-ns"},
			Data:       map[string][]byte{"kubeconfig": []byte(identityKubeconfig)},
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(remoteSecret).Build()
		target, err := kubeconfigClusterTarget(identityKubeconfig)
		Expect(err).NotTo(HaveOccurred())
		record := &infraCredentialsRoleBindingRecord{
			InfraClusterRef:    "secret/recorded-infra",
			InfraClusterTarget: target,
			InfraNamespace:     "infra-ns",
			RoleBindingName:    infraRoleBindingName(oacp),
		}

		replacementServer, replacementKubeconfig := newIdentityServer("replacement-token", "system:serviceaccount:test-ns:replacement")
		DeferCleanup(replacementServer.Close)
		remoteSecret.Data["kubeconfig"] = []byte(replacementKubeconfig)
		Expect(sourceClient.Update(ctx, remoteSecret)).To(Succeed())

		currentOACP := oacp.DeepCopy()
		currentOACP.Spec.Config.InfraClusterRef = &corev1.LocalObjectReference{Name: "recorded-infra"}
		_, err = infraClientForRecord(ctx, sourceClient, currentOACP, record, nil, false)
		Expect(err).To(MatchError(ContainSubstring("now identifies target")))
	})

	It("rotates a RoleBinding when the same infra Secret changes target", func() {
		remoteOACP := oacp.DeepCopy()
		remoteOACP.Spec.Config.InfraClusterRef = &corev1.LocalObjectReference{Name: source.Name}
		remoteKubeconfig := withInfraKubeconfigNamespace(identityKubeconfig)
		source.Data["kubeconfig"] = []byte(remoteKubeconfig)
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(remoteOACP, source).Build()
		infraClient := newInfraClient(kubernetes)
		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns", remoteKubeconfig), remoteOACP)).To(Succeed())

		replacementServer, replacementKubeconfig := newIdentityServer("replacement-token", "system:serviceaccount:test-ns:replacement")
		DeferCleanup(replacementServer.Close)
		replacementKubeconfig = withInfraKubeconfigNamespace(replacementKubeconfig)
		source.Data["kubeconfig"] = []byte(replacementKubeconfig)
		Expect(sourceClient.Update(ctx, source)).To(Succeed())

		currentOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: remoteOACP.Name, Namespace: remoteOACP.Namespace}, currentOACP)).To(Succeed())
		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns", replacementKubeconfig), currentOACP)).To(Succeed())

		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: remoteOACP.Name, Namespace: remoteOACP.Namespace}, currentOACP)).To(Succeed())
		record, err := parseInfraCredentialsRoleBindingRecord(currentOACP.Annotations[infraCredentialsRoleBindingAnnotation])
		Expect(err).NotTo(HaveOccurred())
		replacementTarget, err := kubeconfigClusterTarget(replacementKubeconfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(record.InfraClusterTarget).To(Equal(replacementTarget))

		roleBinding := &rbacv1.RoleBinding{}
		Expect(infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(remoteOACP), Namespace: "infra-ns"}, roleBinding)).To(Succeed())
		Expect(roleBinding.Subjects).To(Equal([]rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      "replacement",
			Namespace: "test-ns",
		}}))
	})

	It("rotates a RoleBinding when the infra reference changes from local to remote", func() {
		existing := infraCredentialsRoleBindingRecord{
			InfraClusterRef:    localInfraClusterTarget,
			InfraClusterTarget: localInfraClusterTarget,
			InfraNamespace:     "test-ns",
			RoleBindingName:    infraRoleBindingName(oacp),
		}
		desired := existing
		desired.InfraClusterRef = "secret/new-infra"
		desired.InfraClusterTarget = "new-target"
		desired.InfraNamespace = "new-infra-ns"
		rotatingOACP := oacp.DeepCopy()
		rotatingOACP.Spec.Config.InfraClusterRef = &corev1.LocalObjectReference{Name: "new-infra"}
		rotatingOACP.Annotations = map[string]string{
			infraCredentialsRoleBindingAnnotation: mustMarshalInfraCredentialsRecord(&existing),
		}
		oldRoleBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: infraRoleBindingName(oacp), Namespace: "test-ns"},
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rotatingOACP, oldRoleBinding).Build()
		infraClient := newInfraClient(kubernetes)

		Expect(ensureInfraCredentialsRoleBindingRecord(ctx, sourceClient, infraClient, rotatingOACP, &desired)).To(Succeed())

		oldErr := sourceClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(oacp), Namespace: "test-ns"}, &rbacv1.RoleBinding{})
		Expect(apierrors.IsNotFound(oldErr)).To(BeTrue())
		currentRecord, err := parseInfraCredentialsRoleBindingRecord(rotatingOACP.Annotations[infraCredentialsRoleBindingAnnotation])
		Expect(err).NotTo(HaveOccurred())
		Expect(*currentRecord).To(Equal(desired))
		Expect(rotatingOACP.Annotations).NotTo(HaveKey(infraCredentialsPendingRoleBindingsAnnotation))
	})

	It("retains a previous remote record when rotating to local infra", func() {
		existing := infraCredentialsRoleBindingRecord{
			InfraClusterRef:    "secret/old-infra",
			InfraClusterTarget: "old-target",
			InfraNamespace:     "old-infra-ns",
			RoleBindingName:    infraRoleBindingName(oacp),
		}
		desired := existing
		desired.InfraClusterRef = localInfraClusterTarget
		desired.InfraClusterTarget = localInfraClusterTarget
		desired.InfraNamespace = "test-ns"
		rotatingOACP := oacp.DeepCopy()
		rotatingOACP.Annotations = map[string]string{
			infraCredentialsRoleBindingAnnotation: mustMarshalInfraCredentialsRecord(&existing),
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rotatingOACP).Build()
		infraClient := newInfraClient(kubernetes)

		Expect(ensureInfraCredentialsRoleBindingRecord(ctx, sourceClient, infraClient, rotatingOACP, &desired)).To(Succeed())

		currentRecord, err := parseInfraCredentialsRoleBindingRecord(rotatingOACP.Annotations[infraCredentialsRoleBindingAnnotation])
		Expect(err).NotTo(HaveOccurred())
		Expect(*currentRecord).To(Equal(desired))
		pendingRecords, err := parseInfraCredentialsPendingRecords(rotatingOACP.Annotations[infraCredentialsPendingRoleBindingsAnnotation])
		Expect(err).NotTo(HaveOccurred())
		Expect(pendingRecords).To(ConsistOf(existing))
	})

	It("does not rewrite credentials when reading the infra Service is forbidden", func() {
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, source).Build()
		infraClient := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, clientWithWatch client.WithWatch, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
				if key == (types.NamespacedName{Name: "kubernetes", Namespace: "default"}) {
					return apierrors.NewForbidden(rbacv1.Resource("services"), "kubernetes", errors.New("denied"))
				}
				return clientWithWatch.Get(ctx, key, object, options...)
			},
		}).Build()

		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, oacp.Namespace), oacp)).To(Succeed())

		credentialsConfigMap := &corev1.ConfigMap{}
		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: InfraCredentialsCMName, Namespace: "test-ns"}, credentialsConfigMap)).To(Succeed())
		Expect(credentialsConfigMap.Data["01-ccm-credentials-secret.yaml"]).To(ContainSubstring(identityServer.URL))
		Expect(credentialsConfigMap.Data["01-ccm-credentials-secret.yaml"]).NotTo(ContainSubstring("server: https://172.30.0.1:443"))
	})

	It("retains the previous identity when credentials rotate on the same target", func() {
		rotationServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			username := "system:serviceaccount:test-ns:infra-user"
			if request.Header.Get("Authorization") == "Bearer replacement-token" {
				username = "system:serviceaccount:test-ns:replacement"
			}
			review := &authenticationv1.SelfSubjectReview{}
			review.Status.UserInfo.Username = username
			w.Header().Set("Content-Type", "application/json")
			Expect(json.NewEncoder(w).Encode(review)).To(Succeed())
		}))
		DeferCleanup(rotationServer.Close)
		caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rotationServer.Certificate().Raw})
		oldKubeconfig := withInfraKubeconfigNamespace(sourceKubeconfigForServer(
			"old-token",
			rotationServer.URL,
			base64.StdEncoding.EncodeToString(caPEM),
		))
		newKubeconfig := withInfraKubeconfigNamespace(sourceKubeconfigForServer(
			"replacement-token",
			rotationServer.URL,
			base64.StdEncoding.EncodeToString(caPEM),
		))

		remoteOACP := oacp.DeepCopy()
		remoteOACP.Spec.Config.InfraClusterRef = &corev1.LocalObjectReference{Name: source.Name}
		source.Data["kubeconfig"] = []byte(oldKubeconfig)
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(remoteOACP, source).Build()
		infraClient := newInfraClient(kubernetes)
		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns", oldKubeconfig), remoteOACP)).To(Succeed())

		source.Data["kubeconfig"] = []byte(newKubeconfig)
		Expect(sourceClient.Update(ctx, source)).To(Succeed())
		currentOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: remoteOACP.Name, Namespace: remoteOACP.Namespace}, currentOACP)).To(Succeed())
		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns", newKubeconfig), currentOACP)).To(Succeed())
		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns", newKubeconfig), currentOACP)).To(Succeed())

		roleBinding := &rbacv1.RoleBinding{}
		Expect(infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(remoteOACP), Namespace: "infra-ns"}, roleBinding)).To(Succeed())
		Expect(roleBinding.Subjects).To(ConsistOf(
			rbacv1.Subject{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      "infra-user",
				Namespace: "test-ns",
			},
			rbacv1.Subject{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      "replacement",
				Namespace: "test-ns",
			},
		))
	})

	It("does not duplicate a human User subject across reconciles", func() {
		humanServer, humanKubeconfig := newIdentityServer(sourceToken, "human-user")
		DeferCleanup(humanServer.Close)
		humanSource := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: source.Name, Namespace: source.Namespace},
			Data:       map[string][]byte{"kubeconfig": []byte(humanKubeconfig)},
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, humanSource).Build()
		infraClient := newInfraClient(kubernetes)

		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns"), oacp)).To(Succeed())
		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns"), oacp)).To(Succeed())

		roleBinding := &rbacv1.RoleBinding{}
		Expect(infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(oacp), Namespace: "infra-ns"}, roleBinding)).To(Succeed())
		Expect(roleBinding.Subjects).To(Equal([]rbacv1.Subject{{
			Kind:     rbacv1.UserKind,
			APIGroup: rbacv1.GroupName,
			Name:     "human-user",
		}}))
	})

	It("rejects a forged RoleBinding name", func() {
		forgedOACP := oacp.DeepCopy()
		forgedOACP.Spec.Config.InfraClusterRef = &corev1.LocalObjectReference{Name: "new-infra"}
		record := &infraCredentialsRoleBindingRecord{
			InfraClusterRef: localInfraClusterTarget,
			InfraNamespace:  "test-ns",
			RoleBindingName: "unrelated-role-binding",
		}

		_, err := infraClientForRecord(ctx, nil, forgedOACP, record, nil, false)
		Expect(err).To(MatchError(ContainSubstring("the expected RoleBinding for test-ns/test-cluster is")))
	})

	It("rejects a forged pending RoleBinding name", func() {
		pendingValue, err := marshalInfraCredentialsPendingRecords([]infraCredentialsRoleBindingRecord{{
			InfraClusterRef: localInfraClusterTarget,
			InfraNamespace:  "test-ns",
			RoleBindingName: "unrelated-role-binding",
		}})
		Expect(err).NotTo(HaveOccurred())
		forgedOACP := oacp.DeepCopy()
		forgedOACP.Annotations = map[string]string{
			infraCredentialsPendingRoleBindingsAnnotation: pendingValue,
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(forgedOACP).Build()

		err = reconcilePendingInfraCredentialsRoleBindings(ctx, sourceClient, forgedOACP)
		Expect(err).To(MatchError(ContainSubstring("the expected RoleBinding for test-ns/test-cluster is")))
	})

	It("allows a legacy local record with the expected RoleBinding name", func() {
		legacyOACP := oacp.DeepCopy()
		record := &infraCredentialsRoleBindingRecord{
			InfraNamespace:  "test-ns",
			RoleBindingName: infraRoleBindingName(legacyOACP),
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(legacyOACP).Build()

		recordClient, err := infraClientForRecord(ctx, sourceClient, legacyOACP, record, sourceClient, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(recordClient).To(Equal(sourceClient))
	})

	It("rejects a local record with a non-local recorded target", func() {
		legacyOACP := oacp.DeepCopy()
		record := &infraCredentialsRoleBindingRecord{
			InfraClusterRef:    localInfraClusterTarget,
			InfraClusterTarget: "recorded-target",
			InfraNamespace:     "test-ns",
			RoleBindingName:    infraRoleBindingName(legacyOACP),
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(legacyOACP).Build()

		_, err := infraClientForRecord(ctx, sourceClient, legacyOACP, record, sourceClient, false)
		Expect(err).To(MatchError(ContainSubstring("does not match the current local infra cluster")))
	})

	It("rejects a credential target that changed after the infra client was selected", func() {
		changedOACP := oacp.DeepCopy()
		changedOACP.Spec.Config.InfraClusterRef = &corev1.LocalObjectReference{Name: "new-infra"}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(changedOACP, source).Build()
		infraClient := newInfraClient(kubernetes)

		err := EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, oacp.Namespace), changedOACP)
		Expect(err).To(MatchError(ContainSubstring("infra credentials target changed")))

		credentialsConfigMap := &corev1.ConfigMap{}
		err = sourceClient.Get(ctx, client.ObjectKey{Name: InfraCredentialsCMName, Namespace: "test-ns"}, credentialsConfigMap)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("continues with a new target when the previous Secret is missing", func() {
		newKubeconfig := withInfraKubeconfigNamespace(identityKubeconfig)
		newSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "new-infra", Namespace: "test-ns"},
			Data:       map[string][]byte{"kubeconfig": []byte(newKubeconfig)},
		}
		remoteOACP := oacp.DeepCopy()
		remoteOACP.Spec.Config.InfraClusterRef = &corev1.LocalObjectReference{Name: "new-infra"}
		remoteOACP.Annotations = map[string]string{
			infraCredentialsRoleBindingAnnotation: mustMarshalInfraCredentialsRecord(&infraCredentialsRoleBindingRecord{
				InfraClusterRef:    "secret/old-infra",
				InfraClusterTarget: "old-target",
				InfraNamespace:     "old-infra-ns",
				RoleBindingName:    infraRoleBindingName(oacp),
			}),
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(remoteOACP, newSecret).Build()
		infraClient := newInfraClient(kubernetes)
		newTarget, err := kubeconfigClusterTarget(newKubeconfig)
		Expect(err).NotTo(HaveOccurred())
		infraResult := &InfraClientResult{
			Client:          infraClient,
			InfraClusterRef: "secret/new-infra",
			Namespace:       "infra-ns",
			Target:          newTarget,
			IsRemote:        true,
		}

		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, infraResult, remoteOACP)).To(Succeed())

		currentOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: remoteOACP.Name, Namespace: remoteOACP.Namespace}, currentOACP)).To(Succeed())
		currentRecord, err := parseInfraCredentialsRoleBindingRecord(currentOACP.Annotations[infraCredentialsRoleBindingAnnotation])
		Expect(err).NotTo(HaveOccurred())
		Expect(currentRecord.InfraClusterRef).To(Equal("secret/new-infra"))
		pendingRecords, err := parseInfraCredentialsPendingRecords(currentOACP.Annotations[infraCredentialsPendingRoleBindingsAnnotation])
		Expect(err).NotTo(HaveOccurred())
		Expect(pendingRecords).To(HaveLen(1))
		Expect(pendingRecords[0].InfraClusterRef).To(Equal("secret/old-infra"))

		roleBinding := &rbacv1.RoleBinding{}
		Expect(infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(remoteOACP), Namespace: "infra-ns"}, roleBinding)).To(Succeed())
	})

	It("retains a pending RoleBinding and continues when its delete is unauthorized", func() {
		unauthorizedServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodDelete {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
		}))
		DeferCleanup(unauthorizedServer.Close)
		caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: unauthorizedServer.Certificate().Raw})
		oldKubeconfig := sourceKubeconfigForServer(
			"old-token",
			unauthorizedServer.URL,
			base64.StdEncoding.EncodeToString(caPEM),
		)
		oldTarget, err := kubeconfigClusterTarget(oldKubeconfig)
		Expect(err).NotTo(HaveOccurred())
		oldSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "old-infra", Namespace: "test-ns"},
			Data:       map[string][]byte{"kubeconfig": []byte(oldKubeconfig)},
		}

		newKubeconfig := withInfraKubeconfigNamespace(identityKubeconfig)
		newSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "new-infra", Namespace: "test-ns"},
			Data:       map[string][]byte{"kubeconfig": []byte(newKubeconfig)},
		}
		pendingValue, err := marshalInfraCredentialsPendingRecords([]infraCredentialsRoleBindingRecord{{
			InfraClusterRef:    "secret/old-infra",
			InfraClusterTarget: oldTarget,
			InfraNamespace:     "old-infra-ns",
			RoleBindingName:    infraRoleBindingName(oacp),
		}})
		Expect(err).NotTo(HaveOccurred())
		remoteOACP := oacp.DeepCopy()
		remoteOACP.Spec.Config.InfraClusterRef = &corev1.LocalObjectReference{Name: "new-infra"}
		remoteOACP.Annotations = map[string]string{
			infraCredentialsPendingRoleBindingsAnnotation: pendingValue,
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(remoteOACP, oldSecret, newSecret).Build()
		infraClient := newInfraClient(kubernetes)
		newTarget, err := kubeconfigClusterTarget(newKubeconfig)
		Expect(err).NotTo(HaveOccurred())
		infraResult := &InfraClientResult{
			Client:          infraClient,
			InfraClusterRef: "secret/new-infra",
			Namespace:       "infra-ns",
			Target:          newTarget,
			IsRemote:        true,
		}

		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, infraResult, remoteOACP)).To(Succeed())

		currentOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: remoteOACP.Name, Namespace: remoteOACP.Namespace}, currentOACP)).To(Succeed())
		pendingRecords, err := parseInfraCredentialsPendingRecords(currentOACP.Annotations[infraCredentialsPendingRoleBindingsAnnotation])
		Expect(err).NotTo(HaveOccurred())
		Expect(pendingRecords).To(HaveLen(1))
		Expect(pendingRecords[0].InfraClusterRef).To(Equal("secret/old-infra"))

		roleBinding := &rbacv1.RoleBinding{}
		Expect(infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(remoteOACP), Namespace: "infra-ns"}, roleBinding)).To(Succeed())
		credentialsConfigMap := &corev1.ConfigMap{}
		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: InfraCredentialsCMName, Namespace: "test-ns"}, credentialsConfigMap)).To(Succeed())
		Expect(credentialsConfigMap.Data).To(HaveKey("01-ccm-credentials-secret.yaml"))
	})

	It("queues the previous RoleBinding when rotation delete is unauthorized", func() {
		existing := infraCredentialsRoleBindingRecord{
			InfraClusterRef:    localInfraClusterTarget,
			InfraClusterTarget: localInfraClusterTarget,
			InfraNamespace:     "old-infra-ns",
			RoleBindingName:    infraRoleBindingName(oacp),
		}
		desired := existing
		desired.InfraNamespace = "new-infra-ns"
		rotatingOACP := oacp.DeepCopy()
		rotatingOACP.Annotations = map[string]string{
			infraCredentialsRoleBindingAnnotation: mustMarshalInfraCredentialsRecord(&existing),
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rotatingOACP).Build()
		infraClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(
					ctx context.Context,
					clientWithWatch client.WithWatch,
					object client.Object,
					options ...client.DeleteOption,
				) error {
					return apierrors.NewForbidden(
						schema.GroupResource{Group: rbacv1.GroupName, Resource: "rolebindings"},
						object.GetName(),
						errors.New("delete denied"),
					)
				},
			}).
			Build()

		Expect(ensureInfraCredentialsRoleBindingRecord(ctx, sourceClient, infraClient, rotatingOACP, &desired)).To(Succeed())

		currentRecord, err := parseInfraCredentialsRoleBindingRecord(rotatingOACP.Annotations[infraCredentialsRoleBindingAnnotation])
		Expect(err).NotTo(HaveOccurred())
		Expect(currentRecord.InfraNamespace).To(Equal("new-infra-ns"))
		pendingRecords, err := parseInfraCredentialsPendingRecords(rotatingOACP.Annotations[infraCredentialsPendingRoleBindingsAnnotation])
		Expect(err).NotTo(HaveOccurred())
		Expect(pendingRecords).To(ConsistOf(existing))
	})

	It("retains a pending record when its Secret is missing", func() {
		deletingOACP := oacp.DeepCopy()
		deletingOACP.Spec.Config.InfraClusterRef = &corev1.LocalObjectReference{Name: "new-infra"}
		pendingValue, err := marshalInfraCredentialsPendingRecords([]infraCredentialsRoleBindingRecord{{
			InfraClusterRef:    "secret/old-infra",
			InfraClusterTarget: "old-target",
			InfraNamespace:     "old-infra-ns",
			RoleBindingName:    infraRoleBindingName(oacp),
		}})
		Expect(err).NotTo(HaveOccurred())
		deletingOACP.Annotations = map[string]string{
			infraCredentialsPendingRoleBindingsAnnotation: pendingValue,
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(deletingOACP).Build()

		err = CleanupInfraCredentialsRBAC(ctx, sourceClient, deletingOACP, nil)
		Expect(err).To(MatchError(ContainSubstring("infra CCM role binding target is unreachable; retaining cleanup record")))

		currentOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: deletingOACP.Name, Namespace: deletingOACP.Namespace}, currentOACP)).To(Succeed())
		Expect(currentOACP.Annotations).To(HaveKey(infraCredentialsPendingRoleBindingsAnnotation))
	})

	It("resolves the effective identity for certificate credentials with impersonation", func() {
		certificateServer, certificateKubeconfig := newCertificateIdentityServer("system:serviceaccount:test-ns:certificate-impersonated")
		DeferCleanup(certificateServer.Close)

		subject, err := kubeconfigSubject(ctx, certificateKubeconfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(subject).To(Equal(rbacv1.Subject{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      "certificate-impersonated",
			Namespace: "test-ns",
		}))
	})

	It("retains a record when its Secret is missing", func() {
		deletingOACP := oacp.DeepCopy()
		deletingOACP.Spec.Config.InfraClusterRef = &corev1.LocalObjectReference{Name: "recorded-infra"}
		deletingOACP.Annotations = map[string]string{
			infraCredentialsRoleBindingAnnotation: mustMarshalInfraCredentialsRecord(&infraCredentialsRoleBindingRecord{
				InfraClusterRef:    "secret/recorded-infra",
				InfraClusterTarget: "recorded-target",
				InfraNamespace:     "infra-ns",
				RoleBindingName:    infraRoleBindingName(oacp),
			}),
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(deletingOACP).Build()

		err := CleanupInfraCredentialsRBAC(ctx, sourceClient, deletingOACP, func(*controlplanev1alpha3.OpenshiftAssistedControlPlane) (client.Client, error) {
			return sourceClient, nil
		})
		Expect(err).To(MatchError(ContainSubstring("infra CCM role binding target is unreachable; retaining cleanup record")))

		currentOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: deletingOACP.Name, Namespace: deletingOACP.Namespace}, currentOACP)).To(Succeed())
		Expect(currentOACP.Annotations).To(HaveKey(infraCredentialsRoleBindingAnnotation))
	})

	It("retains a record when the recorded Secret points to another target", func() {
		remoteSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "recorded-infra", Namespace: "test-ns"},
			Data:       map[string][]byte{"kubeconfig": []byte(identityKubeconfig)},
		}
		deletingOACP := oacp.DeepCopy()
		deletingOACP.Spec.Config.InfraClusterRef = &corev1.LocalObjectReference{Name: "recorded-infra"}
		deletingOACP.Annotations = map[string]string{
			infraCredentialsRoleBindingAnnotation: mustMarshalInfraCredentialsRecord(&infraCredentialsRoleBindingRecord{
				InfraClusterRef:    "secret/recorded-infra",
				InfraClusterTarget: "different-target",
				InfraNamespace:     "infra-ns",
				RoleBindingName:    infraRoleBindingName(oacp),
			}),
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(deletingOACP, remoteSecret).Build()

		err := CleanupInfraCredentialsRBAC(ctx, sourceClient, deletingOACP, func(*controlplanev1alpha3.OpenshiftAssistedControlPlane) (client.Client, error) {
			return sourceClient, nil
		})
		Expect(err).To(MatchError(ContainSubstring("infra CCM role binding target is unreachable; retaining cleanup record")))

		currentOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: deletingOACP.Name, Namespace: deletingOACP.Namespace}, currentOACP)).To(Succeed())
		Expect(currentOACP.Annotations).To(HaveKey(infraCredentialsRoleBindingAnnotation))
	})

	It("rejects a local record when the current infra cluster is remote", func() {
		deletingOACP := oacp.DeepCopy()
		deletingOACP.Spec.Config.InfraClusterRef = &corev1.LocalObjectReference{Name: "new-infra"}
		deletingOACP.Annotations = map[string]string{
			infraCredentialsRoleBindingAnnotation: mustMarshalInfraCredentialsRecord(&infraCredentialsRoleBindingRecord{
				InfraClusterRef: localInfraClusterTarget,
				InfraNamespace:  "test-ns",
				RoleBindingName: infraRoleBindingName(oacp),
			}),
		}
		roleBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: infraRoleBindingName(oacp), Namespace: "test-ns"},
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(deletingOACP, roleBinding).Build()

		err := CleanupInfraCredentialsRBAC(ctx, sourceClient, deletingOACP, func(*controlplanev1alpha3.OpenshiftAssistedControlPlane) (client.Client, error) {
			return newInfraClient(), nil
		})
		Expect(err).To(MatchError(ContainSubstring("does not match the current infra cluster")))

		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(oacp), Namespace: "test-ns"}, &rbacv1.RoleBinding{})).To(Succeed())
	})

	It("retains a record when deleting the infra RoleBinding is forbidden", func() {
		deletingOACP := oacp.DeepCopy()
		deletingOACP.Annotations = map[string]string{
			infraCredentialsRoleBindingAnnotation: mustMarshalInfraCredentialsRecord(&infraCredentialsRoleBindingRecord{
				InfraClusterRef: localInfraClusterTarget,
				InfraNamespace:  "test-ns",
				RoleBindingName: infraRoleBindingName(oacp),
			}),
		}
		roleBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: infraRoleBindingName(oacp), Namespace: "test-ns"},
		}
		sourceClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(deletingOACP, roleBinding).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(
					ctx context.Context,
					clientWithWatch client.WithWatch,
					object client.Object,
					options ...client.DeleteOption,
				) error {
					return apierrors.NewForbidden(
						schema.GroupResource{Group: rbacv1.GroupName, Resource: "rolebindings"},
						object.GetName(),
						errors.New("delete denied"),
					)
				},
			}).
			Build()

		err := CleanupInfraCredentialsRBAC(ctx, sourceClient, deletingOACP, func(*controlplanev1alpha3.OpenshiftAssistedControlPlane) (client.Client, error) {
			return sourceClient, nil
		})
		Expect(err).To(MatchError(ContainSubstring("infra CCM role binding cannot be deleted; retaining cleanup record")))

		currentOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: deletingOACP.Name, Namespace: deletingOACP.Namespace}, currentOACP)).To(Succeed())
		Expect(currentOACP.Annotations).To(HaveKey(infraCredentialsRoleBindingAnnotation))
	})

	It("cleans up only the workload cluster RoleBinding", func() {
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, source).Build()
		infraClient := newInfraClient(kubernetes)
		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns"), oacp)).To(Succeed())

		Expect(CleanupInfraCredentialsRBAC(ctx, sourceClient, oacp, func(*controlplanev1alpha3.OpenshiftAssistedControlPlane) (client.Client, error) {
			return infraClient, nil
		})).To(Succeed())

		roleBinding := &rbacv1.RoleBinding{}
		err := infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(oacp), Namespace: "infra-ns"}, roleBinding)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("does not resolve infra credentials when no RoleBinding was recorded", func() {
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, source).Build()

		Expect(CleanupInfraCredentialsRBAC(ctx, sourceClient, oacp, nil)).To(Succeed())
	})

	It("cleans up a recorded RoleBinding after CCM is disabled", func() {
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, source).Build()
		infraClient := newInfraClient(kubernetes)
		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns"), oacp)).To(Succeed())

		currentOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: oacp.Name, Namespace: oacp.Namespace}, currentOACP)).To(Succeed())
		currentOACP.Spec.Config.CloudControllerManager.Enabled = false
		Expect(sourceClient.Update(ctx, currentOACP)).To(Succeed())

		Expect(CleanupInfraCredentialsRBAC(ctx, sourceClient, oacp, func(*controlplanev1alpha3.OpenshiftAssistedControlPlane) (client.Client, error) {
			return infraClient, nil
		})).To(Succeed())

		roleBinding := &rbacv1.RoleBinding{}
		err := infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(oacp), Namespace: "infra-ns"}, roleBinding)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("removes the RoleBinding and its record when CCM is disabled", func() {
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, source).Build()
		infraClient := newInfraClient(kubernetes)
		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns"), oacp)).To(Succeed())

		currentOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: oacp.Name, Namespace: oacp.Namespace}, currentOACP)).To(Succeed())
		currentOACP.Spec.Config.CloudControllerManager.Enabled = false
		Expect(sourceClient.Update(ctx, currentOACP)).To(Succeed())

		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns"), oacp)).To(Succeed())

		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: oacp.Name, Namespace: oacp.Namespace}, currentOACP)).To(Succeed())
		Expect(currentOACP.Annotations).NotTo(HaveKey(infraCredentialsRoleBindingAnnotation))
		roleBinding := &rbacv1.RoleBinding{}
		err := infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(oacp), Namespace: "infra-ns"}, roleBinding)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("does not create the RoleBinding while the OACP is deleting", func() {
		deletingOACP := oacp.DeepCopy()
		deletionTimestamp := metav1.Now()
		deletingOACP.DeletionTimestamp = &deletionTimestamp
		deletingOACP.Finalizers = []string{"test-finalizer"}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(deletingOACP, source).Build()
		infraClient := newInfraClient(kubernetes)

		Expect(EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns"), deletingOACP)).To(Succeed())

		roleBinding := &rbacv1.RoleBinding{}
		err := infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(oacp), Namespace: "infra-ns"}, roleBinding)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("creates remote credentials in the infra namespace", func() {
		oacp.Spec.Config.CSIDriver = &controlplanev1alpha3.CSIDriverSpec{
			Enabled:           true,
			InfraStorageClass: "infra-storage",
		}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, source).Build()
		infraClient := newInfraClient(kubernetes)

		err := EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, "infra-ns"), oacp)
		Expect(err).NotTo(HaveOccurred())

		credentialsConfigMap := &corev1.ConfigMap{}
		err = sourceClient.Get(ctx, client.ObjectKey{Name: InfraCredentialsCMName, Namespace: "test-ns"}, credentialsConfigMap)
		Expect(err).NotTo(HaveOccurred())
		Expect(credentialsConfigMap.Data["02-csi-credentials-secret.yaml"]).To(ContainSubstring("infraClusterNamespace: infra-ns"))

		role := &rbacv1.Role{}
		err = infraClient.Get(ctx, client.ObjectKey{Name: "kubevirt-cloud-controller-manager", Namespace: "infra-ns"}, role)
		Expect(err).NotTo(HaveOccurred())
		roleBinding := &rbacv1.RoleBinding{}
		err = infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(oacp), Namespace: "infra-ns"}, roleBinding)
		Expect(err).NotTo(HaveOccurred())
	})

	It("preserves an explicit TLS server name", func() {
		source.Data["kubeconfig"] = []byte(sourceKubeconfigWithTLSName(sourceToken, "api-cert.example.com"))
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, source).Build()
		infraClient := newInfraClient(kubernetes)

		err := EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, oacp.Namespace), oacp)
		Expect(err).NotTo(HaveOccurred())

		credentialsConfigMap := &corev1.ConfigMap{}
		err = sourceClient.Get(ctx, client.ObjectKey{Name: InfraCredentialsCMName, Namespace: "test-ns"}, credentialsConfigMap)
		Expect(err).NotTo(HaveOccurred())
		Expect(credentialsConfigMap.Data["01-ccm-credentials-secret.yaml"]).To(ContainSubstring("tls-server-name: api-cert.example.com"))
	})

	It("does not create CCM credentials or RBAC for bridge networking", func() {
		oacp.Spec.Config.APIVIPs = []string{"192.0.2.10"}
		oacp.Spec.Config.IngressVIPs = []string{"192.0.2.11"}
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, source).Build()
		infraClient := newInfraClient()

		err := EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, oacp.Namespace), oacp)
		Expect(err).NotTo(HaveOccurred())

		credentialsConfigMap := &corev1.ConfigMap{}
		err = sourceClient.Get(ctx, client.ObjectKey{Name: InfraCredentialsCMName, Namespace: "test-ns"}, credentialsConfigMap)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())

		roleBinding := &rbacv1.RoleBinding{}
		err = infraClient.Get(ctx, client.ObjectKey{Name: infraRoleBindingName(oacp), Namespace: "test-ns"}, roleBinding)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())

		currentOACP := &controlplanev1alpha3.OpenshiftAssistedControlPlane{}
		Expect(sourceClient.Get(ctx, client.ObjectKey{Name: oacp.Name, Namespace: oacp.Namespace}, currentOACP)).To(Succeed())
		Expect(currentOACP.Annotations).NotTo(HaveKey(infraCredentialsRoleBindingAnnotation))
	})

	It("fails when pod-networking credentials cannot reach the infra service", func() {
		sourceClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(oacp, source).Build()
		infraClient := newInfraClient()

		err := EnsureInfraCredentialsManifests(ctx, sourceClient, newInfraResult(infraClient, oacp.Namespace), oacp)
		Expect(err).To(MatchError(ContainSubstring("failed to get infra cluster Kubernetes service")))
	})
})

var _ = Describe("ensureManifestsConfigMap", func() {
	It("removes stale manifest keys when the generated filenames change", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		utilruntime.Must(corev1.AddToScheme(scheme))
		utilruntime.Must(controlplanev1alpha3.AddToScheme(scheme))

		owner := &controlplanev1alpha3.OpenshiftAssistedControlPlane{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-ns",
				UID:       "test-uid",
			},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner).Build()

		err := ensureManifestsConfigMap(ctx, fakeClient, owner, "test-manifests", []ManifestEntry{
			{Filename: "old.yaml", Content: "old"},
		})
		Expect(err).NotTo(HaveOccurred())

		err = ensureManifestsConfigMap(ctx, fakeClient, owner, "test-manifests", []ManifestEntry{
			{Filename: "new.yaml", Content: "new"},
		})
		Expect(err).NotTo(HaveOccurred())

		configMap := &corev1.ConfigMap{}
		err = fakeClient.Get(ctx, client.ObjectKey{Name: "test-manifests", Namespace: "test-ns"}, configMap)
		Expect(err).NotTo(HaveOccurred())
		Expect(configMap.Data).To(HaveLen(1))
		Expect(configMap.Data).To(HaveKeyWithValue("new.yaml", "new"))
		Expect(configMap.Data).NotTo(HaveKey("old.yaml"))
	})
})
