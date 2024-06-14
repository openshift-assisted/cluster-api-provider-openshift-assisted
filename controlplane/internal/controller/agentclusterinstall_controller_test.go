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

package controller

import (
	"context"
	bootstrapv1alpha1 "github.com/openshift-assisted/cluster-api-agent/bootstrap/api/v1alpha1"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	agentControlPlaneName = "test-resource"
	clusterName           = "test-cluster"
	namespace             = "test"
)

var _ = Describe("AgentClusterInstall Controller", func() {
	ctx := context.Background()
	//var controllerReconciler *AgentClusterInstallReconciler
	var k8sClient client.Client

	BeforeEach(func() {
		k8sClient = fakeclient.NewClientBuilder().WithScheme(testScheme).
			WithStatusSubresource(&bootstrapv1alpha1.AgentBootstrapConfig{}).
			Build()
		Expect(k8sClient).NotTo(BeNil())
		/*
			controllerReconciler = &AgentClusterInstallReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

		*/
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		By("creating the test namespace")
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})

	AfterEach(func() {
		k8sClient = nil
		//controllerReconciler = nil
	})
})
