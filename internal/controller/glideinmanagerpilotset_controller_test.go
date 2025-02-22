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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
)

var _ = Describe("GlideinManagerPilotSet Controller", func() {
	Context("When reconciling a resource with no GlideinSets", func() {
		const resourceName = "test-empty-pilotset"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		glideinmanagerpilotset := &gmosv1alpha1.GlideinManagerPilotSet{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind GlideinManagerPilotSet with no GlideinSets")
			err := k8sClient.Get(ctx, typeNamespacedName, glideinmanagerpilotset)
			if err != nil && errors.IsNotFound(err) {
				resource := &gmosv1alpha1.GlideinManagerPilotSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: gmosv1alpha1.GlideinManagerPilotSetSpec{
						GlideinManagerUrl: "localhost:8080",
						GlideinSets:       []gmosv1alpha1.GlideinSetSpec{},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &gmosv1alpha1.GlideinManagerPilotSet{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance GlideinManagerPilotSet")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GlideinManagerPilotSetReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create a Collector", func() {
			By("Checking that a Collector deployment and associated resources were created")
			resource := &gmosv1alpha1.GlideinManagerPilotSet{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			collectorNamespacedName := types.NamespacedName{
				Name:      RNCollector.nameFor(resource),
				Namespace: resource.GetNamespace(),
			}
			collectorConfigNamespacedName := types.NamespacedName{
				Name:      RNCollectorConfig.nameFor(resource),
				Namespace: resource.GetNamespace(),
			}
			collectorSigKeyNamespacedName := types.NamespacedName{
				Name:      RNCollectorSigkey.nameFor(resource),
				Namespace: resource.GetNamespace(),
			}

			Eventually(func(g Gomega) {
				configMap := &corev1.ConfigMap{}
				err = k8sClient.Get(ctx, collectorConfigNamespacedName, configMap)
				Expect(err).NotTo(HaveOccurred())

				sigKey := &corev1.Secret{}
				err = k8sClient.Get(ctx, collectorSigKeyNamespacedName, sigKey)
				Expect(err).NotTo(HaveOccurred())

				collector := &appsv1.Deployment{}
				err = k8sClient.Get(ctx, collectorNamespacedName, collector)
				Expect(err).NotTo(HaveOccurred())

				service := &corev1.Service{}
				err = k8sClient.Get(ctx, collectorNamespacedName, service)
				Expect(err).NotTo(HaveOccurred())

			}, 30*time.Second, 1*time.Second).Should(Succeed())
		})

		It("should not create a GlideinSet", func() {
			Eventually(func(g Gomega) {
				glideinSets := gmosv1alpha1.GlideinSetList{}
				matchingLabels := client.MatchingLabels(map[string]string{"app.kubernetes.io/instance": resourceName})
				err := k8sClient.List(ctx, &glideinSets, client.InNamespace("default"), matchingLabels)

				Expect(err).NotTo(HaveOccurred())
				Expect(len(glideinSets.Items)).To(Equal(0))
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		})
	})

	Context("When reconciling a resource with one GlideinSet", Ordered, func() {
		const resourceName = "test-single-pilotset"

		const glideinSetName = "sample"
		const glideinSetSize = 1

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		glideinSetNamespacedName := types.NamespacedName{
			Name:      resourceName + "-" + glideinSetName,
			Namespace: "default",
		}
		glideinmanagerpilotset := &gmosv1alpha1.GlideinManagerPilotSet{}
		glideinManagerResource := &gmosv1alpha1.GlideinManagerPilotSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
			},
			Spec: gmosv1alpha1.GlideinManagerPilotSetSpec{
				GlideinManagerUrl: "localhost:8081",
				GlideinSets: []gmosv1alpha1.GlideinSetSpec{{
					Name:              glideinSetName,
					Size:              glideinSetSize,
					PriorityClassName: "sample-class",
					NodeSelector: map[string]string{
						"key": "value",
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"cpu":    *resource.NewMilliQuantity(100, resource.DecimalSI),
							"memory": *resource.NewMilliQuantity(100, resource.DecimalSI),
						},
					},
				}},
			},
		}

		BeforeAll(func() {
			By("creating the custom resource for the Kind GlideinManagerPilotSet with one GlideinSets")
			err := k8sClient.Get(ctx, typeNamespacedName, glideinmanagerpilotset)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, glideinManagerResource)).To(Succeed())
			}
		})

		AfterAll(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &gmosv1alpha1.GlideinManagerPilotSet{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance GlideinManagerPilotSet")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GlideinManagerPilotSetReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create a GlideinSet", func() {
			Eventually(func(g Gomega) {
				glideinSet := &gmosv1alpha1.GlideinSet{}
				err := k8sClient.Get(ctx, glideinSetNamespacedName, glideinSet)
				Expect(err).NotTo(HaveOccurred())
				// Expect that the proper fields were copied from the GlideinManagerPilotSet into the GlideinSet
				By("Using the schema from the parent GlideinManagerPilotSet")
				spec := glideinSet.Spec
				baseSpec := glideinManagerResource.Spec

				Expect(spec.GlideinManagerUrl).Should(Equal(baseSpec.GlideinManagerUrl))
				Expect(spec.Size).Should(Equal(baseSpec.GlideinSets[0].Size))
				Expect(spec.PriorityClassName).Should(Equal(baseSpec.GlideinSets[0].PriorityClassName))
				Expect(spec.NodeSelector).Should(Equal(baseSpec.GlideinSets[0].NodeSelector))

				// Default equality doesn't appear to work here, need to use the custom resource.Quantity equals
				Expect(spec.Resources.Requests.Cpu().Equal(*baseSpec.GlideinSets[0].Resources.Requests.Cpu())).Should(BeTrue())
				Expect(spec.Resources.Requests.Memory().Equal(*baseSpec.GlideinSets[0].Resources.Requests.Memory())).Should(BeTrue())
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		})
		It("should modify GlideinSets when the base spec is changed", func() {
			pilotSet := gmosv1alpha1.GlideinManagerPilotSet{}
			err := k8sClient.Get(ctx, typeNamespacedName, &pilotSet)
			Expect(err).NotTo(HaveOccurred())

			const newManagerUrl = "localhost:9090"
			var newSize int32 = 5
			newResources := corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"cpu":    *resource.NewMilliQuantity(400, resource.DecimalSI),
					"memory": *resource.NewMilliQuantity(400, resource.DecimalSI),
				},
			}

			pilotSet.Spec.GlideinManagerUrl = newManagerUrl
			pilotSet.Spec.GlideinSets[0].Size = newSize
			pilotSet.Spec.GlideinSets[0].Resources = newResources

			err = k8sClient.Update(ctx, &pilotSet)
			Expect(err).NotTo(HaveOccurred())
			By("Re-reconciling the new spec for the resource")
			controllerReconciler := &GlideinManagerPilotSetReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				glideinSet := &gmosv1alpha1.GlideinSet{}
				err := k8sClient.Get(ctx, glideinSetNamespacedName, glideinSet)
				Expect(err).NotTo(HaveOccurred())
				// Expect that the proper fields were copied from the GlideinManagerPilotSet into the GlideinSet
				By("Propegating the updates to the schema from the parent GlideinManagerPilotSet")
				spec := glideinSet.Spec

				Expect(spec.GlideinManagerUrl).Should(Equal(newManagerUrl))
				Expect(spec.Size).Should(Equal(newSize))
				// Default equality doesn't appear to work here, need to use the custom resource.Quantity equals
				Expect(spec.Resources.Requests.Cpu().Equal(*newResources.Requests.Cpu())).Should(BeTrue())
				Expect(spec.Resources.Requests.Memory().Equal(*newResources.Requests.Memory())).Should(BeTrue())
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		})

		It("should delete GlideinSets when removed from the base spec", func() {
			pilotSet := gmosv1alpha1.GlideinManagerPilotSet{}
			err := k8sClient.Get(ctx, typeNamespacedName, &pilotSet)
			Expect(err).NotTo(HaveOccurred())
			pilotSet.Spec.GlideinSets = []gmosv1alpha1.GlideinSetSpec{}

			err = k8sClient.Update(ctx, &pilotSet)
			Expect(err).NotTo(HaveOccurred())
			By("Re-reconciling the new spec for the resource")
			controllerReconciler := &GlideinManagerPilotSetReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func(g Gomega) {
				glideinSets := gmosv1alpha1.GlideinSetList{}
				matchingLabels := client.MatchingLabels(map[string]string{"app.kubernetes.io/instance": resourceName})
				err := k8sClient.List(ctx, &glideinSets, client.InNamespace("default"), matchingLabels)

				Expect(err).NotTo(HaveOccurred())
				Expect(len(glideinSets.Items)).To(Equal(0))
			}, 30*time.Second, 1*time.Second).Should(Succeed())
		})
	})
})
