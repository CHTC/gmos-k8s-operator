package controller

import (
	"context"
	"os"

	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("GlideinManagerPilotSet Controller", func() {
	Context("When reconciling a GlideinSet with no upstream config", func() {
		const resourceName = "test-unconfigured-glideinset"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		glideinset := &gmosv1alpha1.GlideinSet{}
		const glideinManagerUrl = "localhost:8080"

		BeforeEach(func() {
			By("creating the custom resource for the Kind GlideinManagerPilotSet with no GlideinSets")
			os.Setenv("CLIENT_NAME", "localhost:8080")
			err := k8sClient.Get(ctx, typeNamespacedName, glideinset)
			if err != nil && errors.IsNotFound(err) {
				resource := &gmosv1alpha1.GlideinSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
						Labels:    labelsForPilotSet(resourceName),
					},
					Spec: gmosv1alpha1.GlideinSetSpec{
						Name:              resourceName,
						Size:              5,
						GlideinManagerUrl: glideinManagerUrl,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// Cleanup logic after each test, like removing the resource instance.
			os.Unsetenv("CLIENT_NAME")
			resource := &gmosv1alpha1.GlideinSet{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance GlideinManagerPilotSet")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GlideinSetReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should create a set of resources for the glidein", func() {
			resource := gmosv1alpha1.GlideinSet{}
			err := k8sClient.Get(ctx, typeNamespacedName, &resource)
			Expect(err).NotTo(HaveOccurred())

			deploymentNamespacedName := types.NamespacedName{
				Name:      RNBase.NameFor(&resource),
				Namespace: "default",
			}

			collectorTokensNamespacedName := types.NamespacedName{
				Name:      RNCollectorTokens.NameFor(&resource),
				Namespace: "default",
			}

			dataNamespacedName := types.NamespacedName{
				Name:      RNData.NameFor(&resource),
				Namespace: "default",
			}

			accessTokenNamespacedName := types.NamespacedName{
				Name:      RNTokens.NameFor(&resource),
				Namespace: "default",
			}

			Eventually(func(g Gomega) {
				deployment := &appsv1.Deployment{}
				err = k8sClient.Get(ctx, deploymentNamespacedName, deployment)
				Expect(err).NotTo(HaveOccurred())

				collectorTokens := &corev1.Secret{}
				err = k8sClient.Get(ctx, collectorTokensNamespacedName, collectorTokens)
				Expect(err).NotTo(HaveOccurred())

				dataSecret := &corev1.Secret{}
				err = k8sClient.Get(ctx, dataNamespacedName, dataSecret)
				Expect(err).NotTo(HaveOccurred())

				accessTokens := &corev1.Secret{}
				err = k8sClient.Get(ctx, accessTokenNamespacedName, accessTokens)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		It("Should register a glidein manager client", func() {
			// TODO: Scaffolding a functioning glidein manager in the test framework is quite
			// difficult. For starters, just check that a client is registered.
			resource := gmosv1alpha1.GlideinSet{}
			err := k8sClient.Get(ctx, typeNamespacedName, &resource)
			Expect(err).NotTo(HaveOccurred())

			Expect(activeGlideinManagerPollers).Should(HaveKey(glideinManagerUrl))
			myPoller := activeGlideinManagerPollers[glideinManagerUrl]
			Expect(myPoller.HasUpdateHandlerForResource(NamespacedNameFor(&resource))).Should(BeTrue())
		})
	})
})
