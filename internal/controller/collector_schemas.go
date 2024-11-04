package controller

import (
	"crypto/rand"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Schema entries for the collector

// ResourceCreator implementation that creates a Secret containing a token signing key for a Collector
type CollectorSigningKeyCreator struct {
}

func (cc *CollectorSigningKeyCreator) SetResourceValue(
	r Reconciler, resource metav1.Object, secret *corev1.Secret) error {
	keyLen := 256
	b := make([]byte, keyLen)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	secret.Data = map[string][]byte{
		"NAMESPACE": b,
	}
	return nil
}

// ResourceCreator implementation that creates a ConfigMap containing fixed config.d config file
// contents for a Collector
type CollectorConfigMapCreator struct {
}

func (du *CollectorConfigMapCreator) SetResourceValue(
	r Reconciler, resource metav1.Object, config *corev1.ConfigMap) error {
	config.Data = map[string]string{
		"05-daemon.config": "DAEMON_LIST = COLLECTOR",
	}
	return nil
}

// ResourceCreator implementation that creates a new Deployment running a collector, with
// appropriate VolumeMounts for its signing key Secret and config.d ConfigMap.
// Also contains an initContainer that modifies the signing key's file permissions, as
// the default required by the Collector (0600) is not possible directly via VolumeMounts
type CollectorDeploymentCreator struct {
}

func (du *CollectorDeploymentCreator) SetResourceValue(
	r Reconciler, resource metav1.Object, dep *appsv1.Deployment) error {
	labelsMap := labelsForPilotSet(resource.GetName())
	labelsMap["gmos.chtc.wisc.edu/app"] = "collector"

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
				InitContainers: []corev1.Container{{
					Image:           "htcondor/base",
					Name:            "key-permissions",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command: []string{
						"sh", "-c",
						"cp /mnt/sigkeys/* /etc/condor/passwords.d/ && chmod 600 /etc/condor/passwords.d/*",
					},
					VolumeMounts: []corev1.VolumeMount{{
						Name:      "collector-sigkeys",
						MountPath: "/etc/condor/passwords.d/",
					}, {
						Name:      "collector-sigkey-staging",
						MountPath: "/mnt/sigkeys/",
					}},
				}},
				Containers: []corev1.Container{{
					Image:           "htcondor/base",
					Name:            "collector",
					ImagePullPolicy: corev1.PullIfNotPresent,
					VolumeMounts: []corev1.VolumeMount{{
						Name:      "collector-config",
						MountPath: "/etc/condor/config.d/05-daemon.config",
						SubPath:   "05-daemon.config",
					}, {
						Name:      "collector-sigkeys",
						MountPath: "/etc/condor/passwords.d/",
					},
					},
				}},
				Volumes: []corev1.Volume{{
					Name: "collector-config",
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: RNCollectorConfig.NameFor(resource),
							},
						},
					},
				}, {
					Name: "collector-sigkey-staging",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: RNCollectorSigkey.NameFor(resource),
						},
					},
				}, {
					Name: "collector-sigkeys",
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

// ResourceCreator implemetation that creates a Service pointing at
// port 9618 on the Collector
type CollectorServiceCreator struct {
}

func (du *CollectorServiceCreator) SetResourceValue(
	r Reconciler, resource metav1.Object, svc *corev1.Service) error {
	labelsMap := labelsForPilotSet(resource.GetName())
	svc.Labels = labelsMap
	svc.Spec = corev1.ServiceSpec{
		Selector: map[string]string{
			"gmos.chtc.wisc.edu/app": "collector",
		},
		Ports: []corev1.ServicePort{
			{
				Name:       "collector",
				Protocol:   "TCP",
				Port:       9618,
				TargetPort: intstr.FromInt(9618),
			},
		},
	}

	return nil
}

// ResourceUpdater implementation that updates a Secret's data key based on the
// values returned by `condor_token_create` run on a collector
type CollectorTokenSecretUpdater struct {
	token string
	PilotSetReconcileState
}

// Set the collector.tkn
func (ct *CollectorTokenSecretUpdater) UpdateResourceValue(r Reconciler, sec *corev1.Secret) (bool, error) {
	sec.StringData = map[string]string{
		"collector.tkn": ct.token,
	}

	// Since we're not fully recreating the map, ensure that the initial placeholder key pair is removed
	delete(sec.Data, EMPTY_MAP_KEY)

	return true, nil
}
