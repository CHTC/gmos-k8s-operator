package controller

import (
	"fmt"
	"os"

	gmosClient "github.com/chtc/gmos-client/client"
	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (r *GlideinManagerPilotSetReconciler) makeDeploymentForPilotSet(pilotSet *gmosv1alpha1.GlideinManagerPilotSet) (*appsv1.Deployment, error) {
	labelsMap := labelsForPilotSet(pilotSet.Name)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pilotSet.Name,
			Namespace: pilotSet.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &[]int32{1}[0],
			Selector: &metav1.LabelSelector{
				MatchLabels: labelsMap,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labelsMap,
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						// IMPORTANT: seccomProfile was introduced with Kubernetes 1.19
						// If you are looking for to produce solutions to be supported
						// on lower versions you must remove this option.
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{{
						Image:           "ubuntu:22.04",
						Name:            "sleeper",
						ImagePullPolicy: corev1.PullIfNotPresent,
						// Ensure restrictive context for the container
						// More info: https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted
						SecurityContext: &corev1.SecurityContext{
							RunAsNonRoot:             &[]bool{true}[0],
							RunAsUser:                &[]int64{1001}[0],
							AllowPrivilegeEscalation: &[]bool{false}[0],
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{
									"ALL",
								},
							},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "gmos-data",
							MountPath: "/mnt/gmos-data",
						}},
						Command: []string{"sleep", "120"},
					}},
					Volumes: []corev1.Volume{{
						Name: "gmos-data",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: pilotSet.Name,
							},
						},
					}},
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(pilotSet, dep, r.Scheme); err != nil {
		return nil, err
	}
	return dep, nil
}

func (r *GlideinManagerPilotSetReconciler) updateDeploymentForPilotSet(dep *appsv1.Deployment, pilotSet *gmosv1alpha1.GlideinManagerPilotSet) (bool, error) {
	// TODO
	updated := false
	if *dep.Spec.Replicas != pilotSet.Spec.Size {
		dep.Spec.Replicas = &pilotSet.Spec.Size
		updated = true
	}

	return updated, nil
}

func (r *GlideinManagerPilotSetReconciler) makeSecretForPilotSet(pilotSet *gmosv1alpha1.GlideinManagerPilotSet) (*corev1.Secret, error) {
	cmap := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pilotSet.Name,
			Namespace: pilotSet.Namespace,
		},
		Data: map[string][]byte{
			"sample.cfg": {},
		},
	}

	if err := ctrl.SetControllerReference(pilotSet, cmap, r.Scheme); err != nil {
		return nil, err
	}
	return cmap, nil
}

func labelsForPilotSet(name string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "GlideinManagerPilotSet",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/created-by": "controller-manager",
	}
}

func (r *GlideinManagerPilotSetReconciler) updateSecretFromGitCommit(sec *corev1.Secret, gitUpdate gmosClient.RepoUpdate) error {
	// update a label on the deployment
	fileMap := make(map[string][]byte)
	listing, err := os.ReadDir(gitUpdate.Path)
	if err != nil {
		return err
	}
	for _, entry := range listing {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(fmt.Sprintf("%v/%v", gitUpdate.Path, entry.Name()))
		if err != nil {
			return err
		}
		fileMap[entry.Name()] = data
	}
	sec.Data = fileMap
	return nil
}

func (r *GlideinManagerPilotSetReconciler) updateDeploymentFromGitCommit(dep *appsv1.Deployment, gitUpdate gmosClient.RepoUpdate) error {
	dep.Spec.Template.ObjectMeta.Labels["git-hash"] = gitUpdate.CurrentCommit
	return nil
}
