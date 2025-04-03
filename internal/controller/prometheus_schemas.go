package controller

import (
	"bytes"

	"text/template"

	"github.com/chtc/gmos-k8s-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Struct for string formatting the Prometheus Config file
type PrometheusConfigTemplate struct {
	Namespace string

	ServiceAccount  string
	ResourceName    string
	PushGateway     string
	PushGatewayPort int

	Collector     string
	CollectorPort int
}

const PROMETHEUS_CONFIG_YAML = `
global:
  scrape_interval: 10s
  evaluation_interval: 10s
rule_files:
  - /etc/prometheus/prometheus.rules
alerting:
  alertmanagers: []
scrape_configs:
  {{ if and (.ServiceAccount) (not (eq .ServiceAccount "default")) }}
  - job_name: 'node-cadvisor'
    kubernetes_sd_configs:
      - role: node
    scheme: https
    tls_config:
      ca_file: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
    bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
    relabel_configs:
      - target_label: __address__
        replacement: kubernetes.default.svc:443
      - source_labels: [__meta_kubernetes_node_name]
        regex: (.+)
        target_label: __metrics_path__
        replacement: /api/v1/nodes/${1}/proxy/metrics/cadvisor
    metric_relabel_configs:
      - source_labels: [pod]
        regex: {{.ResourceName}}-.*
        action: keep
  {{ end }}
  - job_name: 'pushgateway'
    honor_labels: true
    static_configs:
    - targets: [{{.PushGateway}}.{{.Namespace}}.svc.cluster.local:{{.PushGatewayPort}}]
    - targets: [{{.Collector}}.{{.Namespace}}.svc.cluster.local:{{.CollectorPort}}]
`

const PROMETHEUS = "prometheus"
const PUSHGATEWAY = "prometheus-pushgateway"
const PROMETHEUS_PORT = 9090
const PUSHGATEWAY_PORT = 9091

func getPrometheusConfig(resource metav1.Object, monitoring v1alpha1.PrometheusMonitoringSpec) (string, error) {
	tmpl, err := template.New("prometheusConfig").Parse(PROMETHEUS_CONFIG_YAML)
	if err != nil {
		return "", err
	}
	configVars := PrometheusConfigTemplate{
		Namespace:      resource.GetNamespace(),
		ResourceName:   RNBase.nameFor(resource),
		ServiceAccount: monitoring.ServiceAccount,

		PushGateway:     RNPrometheusPushgateway.nameFor(resource),
		PushGatewayPort: PUSHGATEWAY_PORT,
		Collector:       RNCollector.nameFor(resource),
		CollectorPort:   9618, // TODO
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, configVars); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type PrometheusConfigMapEditor struct {
	pilotSet *v1alpha1.GlideinManagerPilotSet
}

func (pe *PrometheusConfigMapEditor) setResourceValue(
	r Reconciler, resource metav1.Object, config *corev1.ConfigMap) error {

	configYaml, err := getPrometheusConfig(pe.pilotSet, pe.pilotSet.Spec.Prometheus)
	if err != nil {
		return err
	}
	config.Data = map[string]string{
		"prometheus.yaml": configYaml,
	}
	return nil
}

func (pe *PrometheusConfigMapEditor) updateResourceValue(r Reconciler, config *corev1.ConfigMap) (bool, error) {
	configYaml, err := getPrometheusConfig(pe.pilotSet, pe.pilotSet.Spec.Prometheus)
	if err != nil {
		return false, err
	}
	oldConfig := config.Data["prometheus.yaml"]
	config.Data = map[string]string{
		"prometheus.yaml": configYaml,
	}
	return oldConfig != configYaml, nil
}

type PrometheusDeploymentEditor struct {
	monitoring v1alpha1.PrometheusMonitoringSpec
}

func (pe *PrometheusDeploymentEditor) setResourceValue(
	r Reconciler, resource metav1.Object, dep *appsv1.Deployment) error {
	labelsMap := labelsForPilotSet(resource.GetName())
	labelsMap["gmos.chtc.wisc.edu/app"] = PROMETHEUS

	volumeSource := pe.monitoring.StorageVolume
	if volumeSource == nil {
		volumeSource = &corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}
	}

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
								Name: RNPrometheus.nameFor(resource),
							},
						},
					},
				}, {
					Name:         "prometheus-storage",
					VolumeSource: *volumeSource,
				},
				},
				ServiceAccountName: pe.monitoring.ServiceAccount,
			},
		},
	}
	return nil
}

func (pu *PrometheusDeploymentEditor) updateResourceValue(r Reconciler, dep *appsv1.Deployment) (bool, error) {
	volumeSource := pu.monitoring.StorageVolume
	if volumeSource == nil {
		volumeSource = &corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}
	}
	dep.Spec.Template.Spec.ServiceAccountName = pu.monitoring.ServiceAccount
	dep.Spec.Template.Spec.Volumes[1].VolumeSource = *volumeSource
	return true, nil
}

type PrometheusServiceCreator struct {
}

func (pc *PrometheusServiceCreator) setResourceValue(
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

func (pc *PrometheusPushgatewayDeploymentCreator) setResourceValue(
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

func (pc *PrometheusPushgatewayServiceCreator) setResourceValue(
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
