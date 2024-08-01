package controller

import (
	"bytes"
	"context"
	"fmt"

	"crypto/rand"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	restclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	clientConfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"

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
								Name: pilotSet.Name + "-collector-cfg",
							},
						},
					},
				}, {
					Name: "collector-sigkey-staging",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: pilotSet.Name + "-collector-sigkey",
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

// Utility function to run 'condor_token_create' in the Collector pod
func ExecInCollector(ctx context.Context, pilotSet *gmosv1alpha1.GlideinManagerPilotSet) error {
	log := log.FromContext(ctx)
	cfg, err := clientConfig.GetConfig()
	if err != nil {
		return err
	}

	client, err := restclient.NewForConfig(cfg)
	if err != nil {
		return err
	}

	// Find the collector pod for the pilotSet based on label selector
	pods, err := client.CoreV1().Pods(pilotSet.Namespace).List(ctx,
		metav1.ListOptions{
			LabelSelector: fmt.Sprintf("gmos.chtc.wisc.edu/app=collector, app.kubernetes.io/instance=%v", pilotSet.Name),
		})
	if err != nil {
		return err
	}
	if len(pods.Items) != 1 {
		return fmt.Errorf("expected 1 collector pod for %v, found %v", pilotSet.Name, len(pods.Items))
	}
	pod := pods.Items[0]
	log.Info(fmt.Sprintf("Found pod with name: %+v", pod.Name))

	// Exec into the pod to run condor_token_create
	cmd := []string{"condor_token_create", "-identity", "glidein@cluster.local", "-key", "NAMESPACE"}
	req := client.CoreV1().RESTClient().Post().Namespace(pilotSet.Namespace).
		Resource("pods").Name(pod.Name).SubResource("exec").VersionedParams(&corev1.PodExecOptions{
		Command: cmd,
		Stdout:  true,
		Stderr:  true,
	}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return err
	}

	outbuf := bytes.Buffer{}
	errbuf := bytes.Buffer{}
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &outbuf,
		Stderr: &errbuf,
	}); err != nil {
		return err
	}

	log.Info(fmt.Sprintf("Stdout: %v", outbuf.String()))
	log.Info(fmt.Sprintf("Stderr: %v", errbuf.String()))

	return nil
}
