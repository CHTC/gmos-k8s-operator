package controller

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const PROMETHEUS_CONFIG_YAML = `
global:
  scrape_interval: 10s
  evaluation_interval: 10s
rule_files:
  - /etc/prometheus/prometheus.rules
alerting:
  alertmanagers: []
scrape_configs:
  - job_name: 'pushgateway'
    honor_labels: true
    static_configs:
    - targets: [%v.%v.svc.cluster.local:9091]
`

const PROMETHEUS = "prometheus"
const PUSHGATEWAY = "prometheus-pushgateway"
const PROMETHEUS_PORT = 9090
const PUSHGATEWAY_PORT = 9091

type PrometheusConfigMapCreator struct {
}

func (pc *PrometheusConfigMapCreator) SetResourceValue(
	r Reconciler, resource metav1.Object, config *corev1.ConfigMap) error {
	config.Data = map[string]string{
		"prometheus.yaml": fmt.Sprintf(PROMETHEUS_CONFIG_YAML, RNPrometheusPushgateway.NameFor(resource), resource.GetNamespace()),
	}
	return nil
}

type PrometheusDeploymentCreator struct {
}

func (pc *PrometheusDeploymentCreator) SetResourceValue(
	r Reconciler, resource metav1.Object, dep *appsv1.Deployment) error {
	labelsMap := labelsForPilotSet(resource.GetName())
	labelsMap["gmos.chtc.wisc.edu/app"] = PROMETHEUS

	dep.Spec = appsv1.DeploymentSpec{
		Replicas: &[]int32{1}[0],
		Selector: &metav1.LabelSelector{
			MatchLabels: labelsMap,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labelsMap,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Image:           "prom/prometheus",
					Name:            PROMETHEUS,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Args: []string{
						"--config.file=/etc/prometheus/prometheus.yaml",
						"--storage.tsdb.path=/prometheus/",
					},
					Ports: []corev1.ContainerPort{{
						ContainerPort: PROMETHEUS_PORT,
					}},
					VolumeMounts: []corev1.VolumeMount{{
						Name:      "prometheus-config",
						MountPath: "/etc/prometheus",
					}, {
						Name:      "prometheus-storage",
						MountPath: "/prometheus/",
					},
					},
				}},
				Volumes: []corev1.Volume{{
					Name: "prometheus-config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: RNPrometheusConfig.NameFor(resource),
							},
						},
					},
				}, {
					Name: "prometheus-storage",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				},
			},
		},
	}
	return nil
}

type PrometheusServiceCreator struct {
}

func (pc *PrometheusServiceCreator) SetResourceValue(
	r Reconciler, resource metav1.Object, svc *corev1.Service) error {
	labelsMap := labelsForPilotSet(resource.GetName())
	svc.Labels = labelsMap
	svc.Spec = corev1.ServiceSpec{
		Selector: map[string]string{
			"gmos.chtc.wisc.edu/app": PROMETHEUS,
		},
		Ports: []corev1.ServicePort{
			{
				Name:       PROMETHEUS,
				Protocol:   "TCP",
				Port:       PROMETHEUS_PORT,
				TargetPort: intstr.FromInt(PROMETHEUS_PORT),
			},
		},
	}
	return nil
}

type PrometheusPushgatewayDeploymentCreator struct {
}

func (pc *PrometheusPushgatewayDeploymentCreator) SetResourceValue(
	r Reconciler, resource metav1.Object, dep *appsv1.Deployment) error {
	labelsMap := labelsForPilotSet(resource.GetName())
	labelsMap["gmos.chtc.wisc.edu/app"] = PUSHGATEWAY

	dep.Spec = appsv1.DeploymentSpec{
		Replicas: &[]int32{1}[0],
		Selector: &metav1.LabelSelector{
			MatchLabels: labelsMap,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labelsMap,
			},
			Spec: corev1.PodSpec{
				// TODO things like persistent storage
				Containers: []corev1.Container{{
					Image:           "prom/pushgateway",
					Name:            PUSHGATEWAY,
					ImagePullPolicy: corev1.PullIfNotPresent,
				},
				}},
		},
	}
	return nil
}

type PrometheusPushgatewayServiceCreator struct {
}

func (pc *PrometheusPushgatewayServiceCreator) SetResourceValue(
	r Reconciler, resource metav1.Object, svc *corev1.Service) error {
	labelsMap := labelsForPilotSet(resource.GetName())
	svc.Labels = labelsMap
	svc.Spec = corev1.ServiceSpec{
		Selector: map[string]string{
			"gmos.chtc.wisc.edu/app": PUSHGATEWAY,
		},
		Ports: []corev1.ServicePort{
			{
				Name:       PUSHGATEWAY,
				Protocol:   "TCP",
				Port:       PUSHGATEWAY_PORT,
				TargetPort: intstr.FromInt(PUSHGATEWAY_PORT),
			},
		},
	}
	return nil
}
