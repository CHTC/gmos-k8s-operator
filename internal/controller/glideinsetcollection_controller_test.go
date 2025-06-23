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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
)

var _ = Describe("GlideinSetCollection Controller", func() {
	Context("When reconciling a resource with no GlideinSets", func() {
		const resourceName = "test-empty-pilotset"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		glideinsetcollection := &gmosv1alpha1.GlideinSetCollection{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind GlideinSetCollection with no GlideinSets")
			err := k8sClient.Get(ctx, typeNamespacedName, glideinsetcollection)
			if err != nil && errors.IsNotFound(err) {
				resource := &gmosv1alpha1.GlideinSetCollection{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: gmosv1alpha1.GlideinSetCollectionSpec{
						GlideinManagerUrl: "localhost:8080",
						GlideinSets:       []gmosv1alpha1.GlideinSetSpec{},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &gmosv1alpha1.GlideinSetCollection{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance GlideinSetCollection")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GlideinSetCollectionReconciler{
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
			resource := &gmosv1alpha1.GlideinSetCollection{}
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
		})

		It("should not create a GlideinSet", func() {
			glideinSets := gmosv1alpha1.GlideinSetList{}
			matchingLabels := client.MatchingLabels(map[string]string{"app.kubernetes.io/instance": resourceName})
			err := k8sClient.List(ctx, &glideinSets, client.InNamespace("default"), matchingLabels)

			Expect(err).NotTo(HaveOccurred())
			Expect(len(glideinSets.Items)).To(Equal(0))
		})
	})
	Context("When reconciling a resource with Prometheus monitoring", Ordered, func() {
		const resourceName = "test-prometheus-pilot"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		glideinsetcollection := &gmosv1alpha1.GlideinSetCollection{}
		glideinManagerResource := &gmosv1alpha1.GlideinSetCollection{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
			},
			Spec: gmosv1alpha1.GlideinSetCollectionSpec{
				GlideinManagerUrl: "localhost:8082",
				Prometheus: gmosv1alpha1.PrometheusMonitoringSpec{
					ServiceAccount: "default",
					StorageVolume: &corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "sample-claim",
						},
					},
				},
				GlideinSets: []gmosv1alpha1.GlideinSetSpec{},
			},
		}

		pushGatewayName := types.NamespacedName{
			Name:      resourceName + string(RNPrometheusPushgateway),
			Namespace: "default",
		}

		prometheusName := types.NamespacedName{
			Name:      resourceName + string(RNPrometheus),
			Namespace: "default",
		}

		BeforeAll(func() {
			By("creating the custom resource for the Kind GlideinSetCollection with Prometheus Config")
			err := k8sClient.Get(ctx, typeNamespacedName, glideinsetcollection)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, glideinManagerResource)).To(Succeed())
			}
		})
		AfterAll(func() {
			resource := &gmosv1alpha1.GlideinSetCollection{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance GlideinSetCollection")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GlideinSetCollectionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should create a Prometheus server", func() {
			resource := &gmosv1alpha1.GlideinSetCollection{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a PushGateway Deployment and Service")
			pushGatewayDep := appsv1.Deployment{}
			err = k8sClient.Get(ctx, pushGatewayName, &pushGatewayDep)
			Expect(err).NotTo(HaveOccurred())
			Expect(pushGatewayDep.Spec.Template.ObjectMeta.Labels).To(HaveKeyWithValue("gmos.chtc.wisc.edu/app", PUSHGATEWAY))

			pushGatewaySvc := corev1.Service{}
			err = k8sClient.Get(ctx, pushGatewayName, &pushGatewaySvc)
			Expect(err).NotTo(HaveOccurred())
			Expect(pushGatewaySvc.Spec.Selector).To(HaveKeyWithValue("gmos.chtc.wisc.edu/app", PUSHGATEWAY))

			By("Creating a configmap for Prometheus that specifies the Pushgateway and Collector as scrape targets")
			promConfigMap := corev1.ConfigMap{}
			err = k8sClient.Get(ctx, prometheusName, &promConfigMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(promConfigMap.Data).To(HaveKey("prometheus.yaml"))

			var cfgMap map[string]interface{}
			err = yaml.Unmarshal([]byte(promConfigMap.Data["prometheus.yaml"]), &cfgMap)
			Expect(err).NotTo(HaveOccurred())
			// TODO might want to make a struct for this rather than mangling anonymous interfaces
			scrapeTargets := cfgMap["scrape_configs"].([]interface{})[0].(map[string]interface{})["static_configs"].([]interface{})
			var pushgatewayTarget = scrapeTargets[0].(map[string]interface{})["targets"].([]interface{})[0].(string)
			var collectorTarget = scrapeTargets[1].(map[string]interface{})["targets"].([]interface{})[0].(string)
			Expect(pushgatewayTarget).To(ContainSubstring(RNPrometheusPushgateway.nameFor(resource)))
			Expect(collectorTarget).To(ContainSubstring(RNCollector.nameFor(resource)))

			By("Creating a Prometheus Deployment and Service")
			promDep := appsv1.Deployment{}
			err = k8sClient.Get(ctx, prometheusName, &promDep)
			Expect(err).NotTo(HaveOccurred())
			Expect(promDep.Spec.Template.ObjectMeta.Labels).To(HaveKeyWithValue("gmos.chtc.wisc.edu/app", PROMETHEUS))

			promSvc := corev1.Service{}
			err = k8sClient.Get(ctx, prometheusName, &promSvc)
			Expect(err).NotTo(HaveOccurred())
			Expect(promSvc.Spec.Selector).To(HaveKeyWithValue("gmos.chtc.wisc.edu/app", PROMETHEUS))

			By("Setting the Prometheus storage volume as specified in the custom resource")
			depPVC := promDep.Spec.Template.Spec.Volumes[1].PersistentVolumeClaim
			expectedPVC := glideinManagerResource.Spec.Prometheus.StorageVolume.PersistentVolumeClaim
			Expect(depPVC).To(Equal(expectedPVC))
		})

		It("Should add a kubelet scrape config when given a non-default service account", func() {
			resource := &gmosv1alpha1.GlideinSetCollection{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())
			resource.Spec.Prometheus.ServiceAccount = "sample-account"

			k8sClient.Update(ctx, resource)

			By("re-reconciling the updated resource")
			controllerReconciler := &GlideinSetCollectionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Adding a service account to the deployment")
			promDep := appsv1.Deployment{}
			err = k8sClient.Get(ctx, prometheusName, &promDep)
			Expect(err).NotTo(HaveOccurred())
			Expect(promDep.Spec.Template.Spec.ServiceAccountName).To(Equal(resource.Spec.Prometheus.ServiceAccount))

			By("Adding adding a Kubernetes Service Discovery scrape config to the prometheus configmap")
			promConfigMap := corev1.ConfigMap{}
			err = k8sClient.Get(ctx, prometheusName, &promConfigMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(promConfigMap.Data).To(HaveKey("prometheus.yaml"))

			var cfgMap map[string]interface{}
			err = yaml.Unmarshal([]byte(promConfigMap.Data["prometheus.yaml"]), &cfgMap)
			Expect(err).NotTo(HaveOccurred())
			// TODO might want to make a struct for this rather than mangling anonymous interfaces
			sdConfigs := cfgMap["scrape_configs"].([]interface{})[0].(map[string]interface{})["kubernetes_sd_configs"].([]interface{})
			sdConfig := sdConfigs[0].(map[string]interface{})["role"].(string)
			Expect(sdConfig).To(Equal("node"))

		})
	})
	Context("When reconciling a resource with Fluentd logging", Ordered, func() {
		const resourceName = "test-fluentd-pilot"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		glideinsetcollection := &gmosv1alpha1.GlideinSetCollection{}
		glideinManagerResource := &gmosv1alpha1.GlideinSetCollection{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
			},
			Spec: gmosv1alpha1.GlideinSetCollectionSpec{
				GlideinManagerUrl: "localhost:8083",
				Fluentd: gmosv1alpha1.FluentdMonitoringSpec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"cpu":    *resource.NewMilliQuantity(100, resource.DecimalSI),
							"memory": *resource.NewMilliQuantity(100, resource.DecimalSI),
						},
					},
					StorageVolume: &corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: "sample-claim",
						},
					},
				},
				GlideinSets: []gmosv1alpha1.GlideinSetSpec{},
			},
		}

		fluentdName := types.NamespacedName{
			Name:      resourceName + "-fluentd",
			Namespace: "default",
		}

		fluentdConfigName := types.NamespacedName{
			Name:      resourceName + "-fluentd-cfg",
			Namespace: "default",
		}

		BeforeAll(func() {
			By("creating the custom resource for the Kind GlideinSetCollection with FluentD Config")
			err := k8sClient.Get(ctx, typeNamespacedName, glideinsetcollection)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, glideinManagerResource)).To(Succeed())
			}
		})

		AfterAll(func() {
			resource := &gmosv1alpha1.GlideinSetCollection{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance GlideinSetCollection")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GlideinSetCollectionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should create a Fluentd server", func() {
			resource := &gmosv1alpha1.GlideinSetCollection{}

			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a Fluentd Deployment and Service")
			fluentdDep := appsv1.Deployment{}
			err = k8sClient.Get(ctx, fluentdName, &fluentdDep)
			Expect(err).NotTo(HaveOccurred())

			fluentdSvc := corev1.Service{}
			err = k8sClient.Get(ctx, fluentdName, &fluentdSvc)
			Expect(err).NotTo(HaveOccurred())

			By("Creating a Fluentd ConfigMap")
			fluentdCfg := corev1.ConfigMap{}
			err = k8sClient.Get(ctx, fluentdConfigName, &fluentdCfg)
			Expect(err).NotTo(HaveOccurred())

			By("Setting the resource requests for the Fluentd Deployment")
			spec := fluentdDep.Spec.Template.Spec.Containers[0]
			baseSpec := glideinManagerResource.Spec.Fluentd
			// Default equality doesn't appear to work here, need to use the custom resource.Quantity equals
			Expect(spec.Resources.Requests.Cpu().Equal(*baseSpec.Resources.Requests.Cpu())).Should(BeTrue())
			Expect(spec.Resources.Requests.Memory().Equal(*baseSpec.Resources.Requests.Memory())).Should(BeTrue())

			By("Setting the storage volume for the Fluentd Deployment")
			volumes := fluentdDep.Spec.Template.Spec.Volumes
			Expect(volumes[1].PersistentVolumeClaim.ClaimName).To(Equal(baseSpec.StorageVolume.PersistentVolumeClaim.ClaimName))
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
		glideinsetcollection := &gmosv1alpha1.GlideinSetCollection{}
		glideinManagerResource := &gmosv1alpha1.GlideinSetCollection{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
			},
			Spec: gmosv1alpha1.GlideinSetCollectionSpec{
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
			By("creating the custom resource for the Kind GlideinSetCollection with one GlideinSets")
			err := k8sClient.Get(ctx, typeNamespacedName, glideinsetcollection)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, glideinManagerResource)).To(Succeed())
			}
		})

		AfterAll(func() {
			resource := &gmosv1alpha1.GlideinSetCollection{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance GlideinSetCollection")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &GlideinSetCollectionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create a GlideinSet", func() {
			glideinSet := &gmosv1alpha1.GlideinSet{}
			err := k8sClient.Get(ctx, glideinSetNamespacedName, glideinSet)
			Expect(err).NotTo(HaveOccurred())
			// Expect that the proper fields were copied from the GlideinSetCollection into the GlideinSet
			By("Using the schema from the parent GlideinSetCollection")
			spec := glideinSet.Spec
			baseSpec := glideinManagerResource.Spec

			Expect(spec.ParentName).Should(Equal(resourceName))
			Expect(spec.GlideinManagerUrl).Should(Equal(baseSpec.GlideinManagerUrl))
			Expect(spec.Size).Should(Equal(baseSpec.GlideinSets[0].Size))
			Expect(spec.PriorityClassName).Should(Equal(baseSpec.GlideinSets[0].PriorityClassName))
			Expect(spec.NodeSelector).Should(Equal(baseSpec.GlideinSets[0].NodeSelector))

			// Default equality doesn't appear to work here, need to use the custom resource.Quantity equals
			Expect(spec.Resources.Requests.Cpu().Equal(*baseSpec.GlideinSets[0].Resources.Requests.Cpu())).Should(BeTrue())
			Expect(spec.Resources.Requests.Memory().Equal(*baseSpec.GlideinSets[0].Resources.Requests.Memory())).Should(BeTrue())
		})
		It("should modify GlideinSets when the base spec is changed", func() {
			pilotSet := gmosv1alpha1.GlideinSetCollection{}
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
			controllerReconciler := &GlideinSetCollectionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			glideinSet := &gmosv1alpha1.GlideinSet{}
			err = k8sClient.Get(ctx, glideinSetNamespacedName, glideinSet)
			Expect(err).NotTo(HaveOccurred())
			// Expect that the proper fields were copied from the GlideinSetCollection into the GlideinSet
			By("Propegating the updates to the schema from the parent GlideinSetCollection")
			spec := glideinSet.Spec

			Expect(spec.GlideinManagerUrl).Should(Equal(newManagerUrl))
			Expect(spec.Size).Should(Equal(newSize))
			// Default equality doesn't appear to work here, need to use the custom resource.Quantity equals
			Expect(spec.Resources.Requests.Cpu().Equal(*newResources.Requests.Cpu())).Should(BeTrue())
			Expect(spec.Resources.Requests.Memory().Equal(*newResources.Requests.Memory())).Should(BeTrue())
		})

		It("should delete GlideinSets when removed from the base spec", func() {
			pilotSet := gmosv1alpha1.GlideinSetCollection{}
			err := k8sClient.Get(ctx, typeNamespacedName, &pilotSet)
			Expect(err).NotTo(HaveOccurred())
			pilotSet.Spec.GlideinSets = []gmosv1alpha1.GlideinSetSpec{}

			err = k8sClient.Update(ctx, &pilotSet)
			Expect(err).NotTo(HaveOccurred())
			By("Re-reconciling the new spec for the resource")
			controllerReconciler := &GlideinSetCollectionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			glideinSets := gmosv1alpha1.GlideinSetList{}
			matchingLabels := client.MatchingLabels(map[string]string{"app.kubernetes.io/instance": resourceName})
			err = k8sClient.List(ctx, &glideinSets, client.InNamespace("default"), matchingLabels)

			Expect(err).NotTo(HaveOccurred())
			Expect(len(glideinSets.Items)).To(Equal(0))
		})
	})
})
