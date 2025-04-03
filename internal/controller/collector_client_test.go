package controller

import (
	"context"
	"os"

	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("Glidein Manager Watcher Test", Ordered, func() {
	Context("When registering a GlideinSet as a Collector Watcher", func() {
		const resourceName = "test-collector-glideinset"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		glideinset := &gmosv1alpha1.GlideinSet{}
		const glideinManagerUrl = "localhost:10010"
		var tempDir string

		BeforeAll(func() {
			By("creating the custom resource for the Kind GlideinSetCollection with no GlideinSets")
			os.Setenv("CLIENT_NAME", "localhost:8080")
			tempDir = GinkgoT().TempDir()
			makeTestSourceData(tempDir)

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

		AfterAll(func() {
			// Cleanup logic after each test, like removing the resource instance.
			os.Unsetenv("CLIENT_NAME")
			resource := &gmosv1alpha1.GlideinSet{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance GlideinSetCollection")
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

		It("Should update secret values based on collector token values", func() {
			resource := gmosv1alpha1.GlideinSet{}
			err := k8sClient.Get(ctx, typeNamespacedName, &resource)
			Expect(err).NotTo(HaveOccurred())

			namespacedName := namespacedNameFor(&resource)
			Expect(collectorClients).Should(HaveKey(namespacedName))

			By("Requesting an update when no token values are present")
			client := collectorClients[namespacedName]
			shouldUpdate, err := client.updateHandler.shouldUpdateTokens()
			Expect(err).NotTo(HaveOccurred())
			Expect(shouldUpdate).To(BeTrue())

			By("Supplying new token values to the update handler")
			glideinTkn := "sample-glidein-tkn"
			epTkn := "sample-ep-tkn"
			err = client.updateHandler.applyTokensUpdate(glideinTkn, epTkn)
			Expect(err).NotTo(HaveOccurred())

			By("Checking that the glidein token set secret was updated")
			secName := types.NamespacedName{
				Name:      resourceName + string(RNCollectorTokens),
				Namespace: "default",
			}
			secret := corev1.Secret{}
			err = k8sClient.Get(ctx, secName, &secret)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.Data).Should(HaveKey("collector.tkn"))
			Expect(secret.Data["collector.tkn"]).To(Equal([]byte(glideinTkn)))

			By("Checking that the EP token set secret was updated")
			secName = types.NamespacedName{
				Name:      resourceName + string(RNTokens),
				Namespace: "default",
			}
			secret = corev1.Secret{}
			err = k8sClient.Get(ctx, secName, &secret)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.Data).Should(HaveKey("collector.tkn"))
			Expect(secret.Data["collector.tkn"]).To(Equal([]byte(epTkn)))
		})
	})
})
