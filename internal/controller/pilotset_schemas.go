package controller

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	gmosClient "github.com/chtc/gmos-client/client"
	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
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
						}, {
							Name:      "gmos-secrets",
							MountPath: "/mnt/gmos-secrets",
						},
						},
						Command: []string{"sleep", "120"},
					}},
					Volumes: []corev1.Volume{{
						Name: "gmos-data",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: pilotSet.Name + "-data",
							},
						},
					}, {
						Name: "gmos-secrets",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: pilotSet.Name + "-tokens",
							},
						},
					},
					},
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

func (r *GlideinManagerPilotSetReconciler) makeSecretForPilotSet(pilotSet *gmosv1alpha1.GlideinManagerPilotSet, suffix string) (*corev1.Secret, error) {
	cmap := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pilotSet.Name + suffix,
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

func readManifestForNamespace(gitUpdate gmosClient.RepoUpdate, namespace string) (PilotSetNamespaceConfig, error) {
	manifest := PilotSetManifiest{}
	data, err := os.ReadFile(filepath.Join(gitUpdate.Path, "glidein-manifest.yaml"))
	if err != nil {
		return PilotSetNamespaceConfig{}, err
	}

	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return PilotSetNamespaceConfig{}, err
	}
	for _, config := range manifest.Manifests {
		if config.Namespace == namespace {
			return config, nil
		}
	}
	return PilotSetNamespaceConfig{}, fmt.Errorf("no config found for namespace %v in manifest %+v", namespace, manifest)
}

func (r *GlideinManagerPilotSetReconciler) updateDataSecretSchema(sec *corev1.Secret, gitUpdate gmosClient.RepoUpdate) error {
	// update a label on the deployment
	config, err := readManifestForNamespace(gitUpdate, sec.Namespace)
	if err != nil {
		return err
	}

	listing, err := os.ReadDir(filepath.Join(gitUpdate.Path, config.Volume.Src))
	if err != nil {
		return err
	}

	fileMap := make(map[string][]byte)
	for _, entry := range listing {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(gitUpdate.Path, config.Volume.Src, entry.Name()))
		if err != nil {
			return err
		}
		fileMap[entry.Name()] = data
	}
	sec.Data = fileMap
	return nil
}

func (r *GlideinManagerPilotSetReconciler) updateTokenSecretSchema(sec *corev1.Secret, gitUpdate gmosClient.RepoUpdate) error {
	// update a label on the deployment
	config, err := readManifestForNamespace(gitUpdate, sec.Namespace)
	if err != nil {
		return err
	}
	fmt.Printf("%+v\n", config)
	if config.SecretSource.SecretName == "" || config.SecretSource.Dst == "" {
		return nil
	}

	tokenMap := make(map[string][]byte)
	tokenMap[config.SecretSource.SecretName] = []byte("Hello, world!")
	sec.Data = tokenMap
	return nil
}

func (r *GlideinManagerPilotSetReconciler) updateDeploymentSchema(dep *appsv1.Deployment, gitUpdate gmosClient.RepoUpdate) (bool, error) {
	dep.Spec.Template.ObjectMeta.Labels["git-hash"] = gitUpdate.CurrentCommit
	// update a label on the deployment
	config, err := readManifestForNamespace(gitUpdate, dep.Namespace)
	if err != nil {
		return false, err
	}
	// Todo error handling here
	container := dep.Spec.Template.Spec.Containers[0]

	// check whether any fields in the deployment were updated
	updated := container.Image != config.Image || container.VolumeMounts[0].MountPath != config.Volume.Dst ||
		container.VolumeMounts[1].MountPath != config.SecretSource.Dst
	if len(container.Command) == len(config.Command) {
		for i := range container.Command {
			updated = updated || container.Command[i] != config.Command[i]
		}
	} else {
		updated = true
	}
	if len(container.Env) == len(config.Env) {
		for i := range container.Env {
			updated = updated || container.Env[i].Name != config.Env[i].Name ||
				container.Env[i].Value != config.Env[i].Value
		}
	} else {
		updated = true
	}

	if updated {
		// Launch parameters
		dep.Spec.Template.Spec.Containers[0].Image = config.Image
		dep.Spec.Template.Spec.Containers[0].Command = config.Command

		// Data mount parameters
		if config.Volume.Src != "" && config.Volume.Dst != "" {
			dep.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath = config.Volume.Dst
		}

		if config.SecretSource.SecretName != "" && config.SecretSource.Dst != "" {
			tokenDir, tokenName := path.Split(config.SecretSource.Dst)
			dep.Spec.Template.Spec.Containers[0].VolumeMounts[1].MountPath = tokenDir

			dep.Spec.Template.Spec.Volumes[1].VolumeSource.Secret.Items = []corev1.KeyToPath{{
				Key:  config.SecretSource.SecretName,
				Path: tokenName,
			}}
		}

		newEnv := make([]corev1.EnvVar, len(config.Env))
		for i, val := range config.Env {
			newEnv[i] = corev1.EnvVar{Name: val.Name, Value: val.Value}
		}
		dep.Spec.Template.Spec.Containers[0].Env = newEnv
	}
	return updated, nil
}
