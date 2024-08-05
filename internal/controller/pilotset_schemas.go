package controller

import (
	"encoding/base64"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Generic interface for a struct that contains a method which updates the structure of a
// Kubernetes Resource
type ResourceUpdater[T client.Object] interface {
	UpdateResourceValue(*GlideinManagerPilotSetReconciler, T) (bool, error)
}

// Generic interface for a struct that creates a Kubernetes resource that
// doesn't yet exist
type ResourceCreator[T client.Object] interface {
	SetResourceValue(*GlideinManagerPilotSetReconciler, *gmosv1alpha1.GlideinManagerPilotSet, T) error
}

type PilotSetDeploymentCreator struct {
}

func (*PilotSetDeploymentCreator) SetResourceValue(
	r *GlideinManagerPilotSetReconciler, pilotSet *gmosv1alpha1.GlideinManagerPilotSet, dep *appsv1.Deployment) error {
	labelsMap := labelsForPilotSet(pilotSet.Name)
	labelsMap["gmos.chtc.wisc.edu/app"] = "pilot"

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
				SecurityContext: &corev1.PodSecurityContext{
					// Set a default non-root user
					RunAsNonRoot: &[]bool{true}[0],
					RunAsUser:    &[]int64{10000}[0],
					RunAsGroup:   &[]int64{10000}[0],
				},
				Containers: []corev1.Container{{
					Image:           "ubuntu:22.04",
					Name:            "sleeper",
					ImagePullPolicy: corev1.PullIfNotPresent,
					// Ensure restrictive context for the container
					// More info: https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted
					SecurityContext: &corev1.SecurityContext{
						Privileged: &[]bool{true}[0],
					},
					VolumeMounts: []corev1.VolumeMount{{
						Name:      "gmos-data",
						MountPath: "/mnt/gmos-data",
					}, {
						Name:      "gmos-secrets",
						MountPath: "/mnt/gmos-secrets",
					}, {
						Name:      "collector-tokens",
						MountPath: "/mnt/collector-tokens",
					},
					},
					Command: []string{"sleep", "120"},
				}},
				Volumes: []corev1.Volume{{
					Name: "gmos-data",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: RNData.NameFor(pilotSet),
						},
					},
				}, {
					Name: "gmos-secrets",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: RNTokens.NameFor(pilotSet),
						},
					},
				}, {
					Name: "collector-tokens",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: RNGlideinTokens.NameFor(pilotSet),
						},
					},
				},
				},
			},
		},
	}
	return nil
}

type EmptySecretCreator struct {
}

func (*EmptySecretCreator) SetResourceValue(
	r *GlideinManagerPilotSetReconciler, pilotSet *gmosv1alpha1.GlideinManagerPilotSet, secret *corev1.Secret) error {
	secret.Data = map[string][]byte{
		"sample.cfg": {},
	}
	return nil
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

// ResourceUpdater implementation that updates a Deployment based on changes
// in its parent custom resource
type DeploymentPilotSetUpdater struct {
	pilotSet *gmosv1alpha1.GlideinManagerPilotSet
}

func (du *DeploymentPilotSetUpdater) UpdateResourceValue(r *GlideinManagerPilotSetReconciler, dep *appsv1.Deployment) (bool, error) {
	// TODO
	updated := false
	if *dep.Spec.Replicas != du.pilotSet.Spec.Size {
		dep.Spec.Replicas = &du.pilotSet.Spec.Size
		updated = true
	}
	return updated, nil
}

// ResourceUpdater implementation that updates a Secret's data based on the
// updated contents of config files in Git
type DataSecretGitUpdater struct {
	gitUpdate *gmosClient.RepoUpdate
}

func (du *DataSecretGitUpdater) UpdateResourceValue(r *GlideinManagerPilotSetReconciler, sec *corev1.Secret) (bool, error) {
	// update a label on the deployment
	config, err := readManifestForNamespace(*du.gitUpdate, sec.Namespace)
	if err != nil {
		return false, err
	}

	listing, err := os.ReadDir(filepath.Join(du.gitUpdate.Path, config.Volume.Src))
	if err != nil {
		return false, err
	}

	fileMap := make(map[string][]byte)
	for _, entry := range listing {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(du.gitUpdate.Path, config.Volume.Src, entry.Name()))
		if err != nil {
			return false, err
		}
		fileMap[entry.Name()] = data
	}
	sec.Data = fileMap
	return true, nil
}

// ResourceUpdater implementation that updates a Secret's data key based on the
// updated contents of a manifest file in Git
type TokenSecretGitUpdater struct {
	gitUpdate *gmosClient.RepoUpdate
}

func (du *TokenSecretGitUpdater) UpdateResourceValue(r *GlideinManagerPilotSetReconciler, sec *corev1.Secret) (bool, error) {
	// update a label on the deployment
	config, err := readManifestForNamespace(*du.gitUpdate, sec.Namespace)
	if err != nil {
		return false, err
	}
	SetSecretSourceForNamespace(sec.Namespace, config.SecretSource.SecretName)
	return false, nil
}

// ResourceUpdater implementation that updates a Secret's value based on the
// updated contents of a Secret in the glidein manager
type TokenSecretValueUpdater struct {
	secValue *gmosClient.SecretValue
}

func (du *TokenSecretValueUpdater) UpdateResourceValue(r *GlideinManagerPilotSetReconciler, sec *corev1.Secret) (bool, error) {
	// update a label on the deployment
	// TODO assumes a single key in the token secret
	if len(sec.Data) > 2 {
		return false, fmt.Errorf("token secret for namespace %v has %v keys (expected <=2)", sec.Namespace, len(sec.Data))
	}
	val, err := base64.StdEncoding.DecodeString(du.secValue.Value)
	if err != nil {
		return false, err
	}
	sec.Data["ospool.tkn"] = val
	return true, nil
}

// ResourceUpdater implementation that updates a Deployment based on the
// updated contents of a manifest file in Git
type DeploymentGitUpdater struct {
	gitUpdate *gmosClient.RepoUpdate
}

// Helper function to check that a pointer is both non nil and equal to a value
func nilSafeEq[T comparable](a *T, b T) bool {
	return a != nil && *a == b
}

// Check field-for-field whether the deployment state described in the latest version
// of the glidein manager config differs from the current state of the deployment.
// TODO there is probably a more efficient way to do this
func deploymentWasUpdated(dep *appsv1.Deployment, config PilotSetNamespaceConfig) bool {
	// TODO there are a lot of places where we could dereference a null pointer here...
	container := dep.Spec.Template.Spec.Containers[0]
	securityCtx := dep.Spec.Template.Spec.SecurityContext

	// check whether any fields in the deployment were updated
	updated := container.Image != config.Image || container.VolumeMounts[0].MountPath != config.Volume.Dst ||
		container.VolumeMounts[1].MountPath != config.SecretSource.Dst ||
		!nilSafeEq(securityCtx.RunAsUser, config.Security.User) || !nilSafeEq(securityCtx.RunAsGroup, config.Security.Group)
	// Check whether any arrays were updated
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

	return updated
}

func (du *DeploymentGitUpdater) UpdateResourceValue(r *GlideinManagerPilotSetReconciler, dep *appsv1.Deployment) (bool, error) {
	dep.Spec.Template.ObjectMeta.Labels["gmos.chtc.wisc.edu/git-hash"] = du.gitUpdate.CurrentCommit
	// update a label on the deployment
	config, err := readManifestForNamespace(*du.gitUpdate, dep.Namespace)
	if err != nil {
		return false, err
	}
	if !deploymentWasUpdated(dep, config) {
		return false, nil
	}

	// Launch parameters
	dep.Spec.Template.Spec.Containers[0].Image = config.Image
	dep.Spec.Template.Spec.Containers[0].Command = config.Command

	// Data mount parameters
	if config.Volume.Src != "" && config.Volume.Dst != "" {
		dep.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath = config.Volume.Dst
	}

	// Token mount parameters
	if config.SecretSource.SecretName != "" && config.SecretSource.Dst != "" {
		tokenDir, _ := path.Split(config.SecretSource.Dst)
		dep.Spec.Template.Spec.Containers[0].VolumeMounts[1].MountPath = tokenDir
	}

	// Environment variables
	newEnv := make([]corev1.EnvVar, len(config.Env))
	for i, val := range config.Env {
		newEnv[i] = corev1.EnvVar{Name: val.Name, Value: val.Value}
	}
	dep.Spec.Template.Spec.Containers[0].Env = newEnv

	// Security context parameters
	dep.Spec.Template.Spec.SecurityContext.RunAsUser = &config.Security.User
	dep.Spec.Template.Spec.SecurityContext.RunAsGroup = &config.Security.Group
	dep.Spec.Template.Spec.SecurityContext.FSGroup = &config.Security.Group
	dep.Spec.Template.Spec.SecurityContext.RunAsNonRoot = &[]bool{config.Security.User != 0}[0]
	return true, nil
}
