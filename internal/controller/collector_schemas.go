package controller

import (
	"crypto/rand"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
)

// Schema entries for the collector

type CollectorSigningKeyCreator struct {
}

func (cc *CollectorSigningKeyCreator) SetResourceValue(
	r *GlideinManagerPilotSetReconciler, pilotSet *gmosv1alpha1.GlideinManagerPilotSet, secret *corev1.Secret) error {
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

type CollectorConfigMapCreator struct {
}

func (du *CollectorConfigMapCreator) SetResourceValue(
	r *GlideinManagerPilotSetReconciler, pilotSet *gmosv1alpha1.GlideinManagerPilotSet, config *corev1.ConfigMap) error {
	config.Data = map[string]string{
		"05-daemon.config": "DAEMON_LIST = COLLECTOR",
	}
	return nil
}

type CollectorDeploymentCreator struct {
}

func (du *CollectorDeploymentCreator) SetResourceValue(
	r *GlideinManagerPilotSetReconciler, pilotSet *gmosv1alpha1.GlideinManagerPilotSet, dep *appsv1.Deployment) error {
	labelsMap := labelsForPilotSet(pilotSet.Name)
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
								Name: RNCollectorConfig.NameFor(pilotSet),
							},
						},
					},
				}, {
					Name: "collector-sigkey-staging",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: RNCollectorSigkey.NameFor(pilotSet),
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

// ResourceUpdater implementation that updates a Secret's data key based on the
// values returned by `condor_token_create` run on a collector
type CollectorTokenSecretUpdater struct {
	token string
	PilotSetReconcileState
}

// Set the collector.tkn
func (ct *CollectorTokenSecretUpdater) UpdateResourceValue(r *GlideinManagerPilotSetReconciler, sec *corev1.Secret) (bool, error) {
	sec.StringData = map[string]string{
		"collector.tkn": ct.token,
	}

	// Since we're not fully recreating the map, ensure that the initial placeholder key pair is removed
	delete(sec.Data, EMPTY_MAP_KEY)

	return true, nil
}
