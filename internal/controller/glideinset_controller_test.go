package controller

import (
	"context"
	"os"
	"path"

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
		const glideinManagerUrl = "localhost:10010"

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
				Name:      RNBase.nameFor(&resource),
				Namespace: "default",
			}

			collectorTokensNamespacedName := types.NamespacedName{
				Name:      RNCollectorTokens.nameFor(&resource),
				Namespace: "default",
			}

			dataNamespacedName := types.NamespacedName{
				Name:      RNData.nameFor(&resource),
				Namespace: "default",
			}

			accessTokenNamespacedName := types.NamespacedName{
				Name:      RNTokens.nameFor(&resource),
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
			Expect(myPoller.hasUpdateHandlerForResource(namespacedNameFor(&resource))).Should(BeTrue())
		})

		It("Should register a collector client", func() {
			resource := gmosv1alpha1.GlideinSet{}
			err := k8sClient.Get(ctx, typeNamespacedName, &resource)
			Expect(err).NotTo(HaveOccurred())

			namespacedName := namespacedNameFor(&resource)
			Expect(collectorClients).Should(HaveKey(namespacedName))
		})

		It("Should remove clients when deleted", func() {
			resource := gmosv1alpha1.GlideinSet{}
			err := k8sClient.Get(ctx, typeNamespacedName, &resource)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Delete(ctx, &resource)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				namespacedName := namespacedNameFor(&resource)
				Expect(collectorClients).ShouldNot(HaveKey(namespacedName))
				Expect(activeGlideinManagerPollers).ShouldNot(HaveKey(glideinManagerUrl))
			})
		})
	})
})

var _ = Describe("GlideinManagerPilotSet Controller", func() {
	Context("When reconciling a GlideinSet with upstream config", func() {
		const resourceName = "test-configured-glideinset"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		glideinset := &gmosv1alpha1.GlideinSet{}
		const glideinManagerUrl = "localhost:8080"
		upstreamConfig := &gmosv1alpha1.PilotSetNamespaceConfig{
			Namespace: "default",
			Image:     "ubuntu:latest",
			Command:   []string{"ls", "-lh"},
			Env: []gmosv1alpha1.PilotSetEnv{
				{
					Name:  "TEST_VAR",
					Value: "TEST_VALUE",
				},
				{
					Name:  "TEST_VAR_2",
					Value: "TEST_VALUE_2",
				},
			},
			Volume: gmosv1alpha1.PilotSetVolumeMount{
				Src: "test-dir",
				Dst: "/mnt/data/test-dir",
			},
			SecretSource: gmosv1alpha1.PilotSetSecretSource{
				SecretName: "test-secret",
				Dst:        "/mnt/secrets/test.secret",
			},
			Security: gmosv1alpha1.PilotSetSecurity{
				User:  1234,
				Group: 1234,
			},
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind GlideinSet with a RemoteManifest")
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
					RemoteManifest: upstreamConfig,
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		BeforeEach(func() {
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

		It("Should set top-level deployment fields when an upstream config is added", func() {
			Eventually(func(g Gomega) {
				resource := gmosv1alpha1.GlideinSet{}
				err := k8sClient.Get(ctx, typeNamespacedName, &resource)
				Expect(err).NotTo(HaveOccurred())

				deploymentNamespacedName := types.NamespacedName{
					Name:      RNBase.nameFor(&resource),
					Namespace: "default",
				}

				deployment := &appsv1.Deployment{}
				err = k8sClient.Get(ctx, deploymentNamespacedName, deployment)
				Expect(err).NotTo(HaveOccurred())

				containerSpec := deployment.Spec.Template.Spec.Containers[0]
				Expect(containerSpec.Command).To(Equal(upstreamConfig.Command))
				Expect(containerSpec.Image).To(Equal(upstreamConfig.Image))

				Expect(containerSpec.SecurityContext.RunAsNonRoot).To(BeTrue())
				Expect(containerSpec.SecurityContext.RunAsUser).To(Equal(upstreamConfig.Security.User))
				Expect(containerSpec.SecurityContext.RunAsGroup).To(Equal(upstreamConfig.Security.Group))

				By("Check that the first variables in the env come from the spec")
				// Need to iterate one by one since two different types with the same fields are used
				for idx, envVar := range upstreamConfig.Env {
					Expect(containerSpec.Env[idx].Name).To(Equal(envVar.Name))
					Expect(containerSpec.Env[idx].Value).To(Equal(envVar.Value))
				}
			})
		})

		It("Should set volume-mount paths when an upstream config is added", func() {
			Eventually(func(g Gomega) {
				resource := gmosv1alpha1.GlideinSet{}
				err := k8sClient.Get(ctx, typeNamespacedName, &resource)
				Expect(err).NotTo(HaveOccurred())

				deploymentNamespacedName := types.NamespacedName{
					Name:      RNBase.nameFor(&resource),
					Namespace: "default",
				}

				deployment := &appsv1.Deployment{}
				err = k8sClient.Get(ctx, deploymentNamespacedName, deployment)
				Expect(err).NotTo(HaveOccurred())

				containerSpec := deployment.Spec.Template.Spec.Containers[0]
				Expect(containerSpec.VolumeMounts[0].MountPath).To(Equal(upstreamConfig.Volume.Dst))
				// TODO cannot figure out how to
				tokenDir, _ := path.Split(upstreamConfig.SecretSource.Dst)
				Expect(containerSpec.VolumeMounts[1].MountPath).To(Equal(tokenDir))

			})

		})
	})
})
