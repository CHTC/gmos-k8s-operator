package controller

import (
	"encoding/base64"
	"errors"
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
)

// ResourceCreator implementation that creates the Deployment that holds a set of Glidein pods
type PilotSetDeploymentCreator struct {
}

// Set the Spec for a deployment to contain a single "sleep" container with volumes and mounts
// for a set of config files
func (*PilotSetDeploymentCreator) SetResourceValue(
	r Reconciler, resource metav1.Object, dep *appsv1.Deployment) error {
	labelsMap := labelsForPilotSet(resource.GetName())
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
							SecretName: RNData.NameFor(resource),
						},
					},
				}, {
					Name: "gmos-secrets",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: RNTokens.NameFor(resource),
						},
					},
				}, {
					Name: "collector-tokens",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: RNCollectorTokens.NameFor(resource),
						},
					},
				},
				},
			},
		},
	}
	return nil
}

const EMPTY_MAP_KEY string = "sample.cfg"

type EmptySecretCreator struct {
}

func (*EmptySecretCreator) SetResourceValue(
	r Reconciler, resource metav1.Object, secret *corev1.Secret) error {
	secret.Data = map[string][]byte{
		EMPTY_MAP_KEY: {},
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

func readManifestForNamespace(gitUpdate gmosClient.RepoUpdate, namespace string) (gmosv1alpha1.PilotSetNamespaceConfig, error) {
	manifest := &PilotSetManifiest{}
	data, err := os.ReadFile(filepath.Join(gitUpdate.Path, "glidein-manifest.yaml"))
	if err != nil {
		return gmosv1alpha1.PilotSetNamespaceConfig{}, err
	}

	if err := yaml.Unmarshal(data, manifest); err != nil {
		return gmosv1alpha1.PilotSetNamespaceConfig{}, err
	}
	for _, config := range manifest.Manifests {
		if config.Namespace == namespace {
			return config, nil
		}
	}
	return gmosv1alpha1.PilotSetNamespaceConfig{}, fmt.Errorf("no config found for namespace %v in manifest %+v", namespace, manifest)
}

// ResourceUpdater implementation that updates a Deployment based on changes
// in its parent custom resource
type DeploymentPilotSetUpdater struct {
	glideinSet *gmosv1alpha1.GlideinSet
}

func (du *DeploymentPilotSetUpdater) UpdateResourceValue(r Reconciler, dep *appsv1.Deployment) (bool, error) {
	updateSpec := du.glideinSet.Spec
	dep.Spec.Replicas = &updateSpec.Size
	dep.Spec.Template.Spec.PriorityClassName = updateSpec.PriorityClassName
	dep.Spec.Template.Spec.Containers[0].Resources = updateSpec.Resources
	dep.Spec.Template.Spec.Tolerations = updateSpec.Tolerations
	dep.Spec.Template.Spec.NodeSelector = updateSpec.NodeSelector
	// at least one thing should have changed if we're re-reconciling, so return true
	return true, nil
}

// ResourceUpdater implementation that updates a Secret's data based on the
// updated contents of config files in Git
type DataSecretGitUpdater struct {
	manifest *gmosv1alpha1.PilotSetNamespaceConfig
}

func (du *DataSecretGitUpdater) UpdateResourceValue(r Reconciler, sec *corev1.Secret) (bool, error) {
	// update a label on the deployment
	manifest := du.manifest

	listing, err := os.ReadDir(filepath.Join(manifest.RepoPath, manifest.Volume.Src))
	if err != nil && errors.Is(err, os.ErrNotExist) {
		// Corner case where operator hasn't cloned local copies yet, nothing to do
		return false, nil

	} else if err != nil {
		return false, err
	}

	fileMap := make(map[string][]byte)
	for _, entry := range listing {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(manifest.RepoPath, manifest.Volume.Src, entry.Name()))
		if err != nil {
			return false, err
		}
		fileMap[entry.Name()] = data
	}
	sec.Data = fileMap

	return true, nil
}

// ResourceUpdater implementation that updates a Secret's value based on the
// updated contents of a Secret in the glidein manager
type TokenSecretValueUpdater struct {
	secSource *gmosv1alpha1.PilotSetSecretSource
	secValue  *gmosClient.SecretValue
}

func (du *TokenSecretValueUpdater) UpdateResourceValue(r Reconciler, sec *corev1.Secret) (bool, error) {
	// update a label on the deployment
	// TODO assumes a single key in the token secret
	if len(sec.Data) > 2 {
		// TODO is this validation needed?
		// return false, fmt.Errorf("token secret for namespace %v has %v keys (expected <=2)", sec.Namespace, len(sec.Data))
	}
	val, err := base64.StdEncoding.DecodeString(du.secValue.Value)
	if err != nil {
		return false, err
	}
	// Token mount parameters
	if du.secSource.Dst != "" {
		_, tokenName := path.Split(du.secSource.Dst)
		sec.Data[tokenName] = val
	}
	if sec.Labels == nil {
		sec.Labels = make(map[string]string)
	}
	sec.Labels["gmos.chtc.wisc.edu/secret-version"] = du.secValue.Version

	// Since we're not fully recreating the map, ensure that the initial placeholder key pair is removed
	delete(sec.Data, EMPTY_MAP_KEY)

	return true, nil
}

// ResourceUpdater implementation that updates a Deployment based on the
// updated contents of a manifest file in Git
type DeploymentGitUpdater struct {
	manifest *gmosv1alpha1.PilotSetNamespaceConfig
}

// Helper function to check that a pointer is both non nil and equal to a value
func nilSafeEq[T comparable](a *T, b T) bool {
	return a != nil && *a == b
}

// Check field-for-field whether the deployment state described in the latest version
// of the glidein manager config differs from the current state of the deployment.
// TODO there is probably a more efficient way to do this
func deploymentWasUpdated(dep *appsv1.Deployment, config *gmosv1alpha1.PilotSetNamespaceConfig) bool {
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
	if len(container.Env) == len(config.Env)+1 {
		for i := range config.Env {
			updated = updated || container.Env[i].Name != config.Env[i].Name ||
				container.Env[i].Value != config.Env[i].Value
		}
	} else {
		updated = true
	}

	return updated
}

func (du *DeploymentGitUpdater) UpdateResourceValue(r Reconciler, dep *appsv1.Deployment) (bool, error) {
	dep.Spec.Template.ObjectMeta.Labels["gmos.chtc.wisc.edu/git-hash"] = du.manifest.CurrentCommit
	// update a label on the deployment
	manifest := du.manifest

	if !deploymentWasUpdated(dep, manifest) {
		return false, nil
	}

	// Launch parameters
	dep.Spec.Template.Spec.Containers[0].Image = manifest.Image
	dep.Spec.Template.Spec.Containers[0].Command = manifest.Command

	// Data mount parameters
	if manifest.Volume.Src != "" && manifest.Volume.Dst != "" {
		dep.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath = manifest.Volume.Dst
	}

	// Token mount parameters
	if manifest.SecretSource.SecretName != "" && manifest.SecretSource.Dst != "" {
		tokenDir, _ := path.Split(manifest.SecretSource.Dst)
		dep.Spec.Template.Spec.Containers[0].VolumeMounts[1].MountPath = tokenDir
	}

	// Environment variables
	newEnv := make([]corev1.EnvVar, len(manifest.Env)+1)
	for i, val := range manifest.Env {
		newEnv[i] = corev1.EnvVar{Name: val.Name, Value: val.Value}
	}
	newEnv[len(manifest.Env)] = corev1.EnvVar{Name: "LOCAL_POOL", Value: fmt.Sprintf("%s%s.%s.svc.cluster.local", dep.Name, RNCollector, dep.Namespace)}
	dep.Spec.Template.Spec.Containers[0].Env = newEnv

	// Security context parameters
	dep.Spec.Template.Spec.SecurityContext.RunAsUser = &manifest.Security.User
	dep.Spec.Template.Spec.SecurityContext.RunAsGroup = &manifest.Security.Group
	dep.Spec.Template.Spec.SecurityContext.FSGroup = &manifest.Security.Group
	dep.Spec.Template.Spec.SecurityContext.RunAsNonRoot = &[]bool{manifest.Security.User != 0}[0]
	return true, nil
}

type GlideinSetCreator struct {
	spec *gmosv1alpha1.GlideinSetSpec
}

func (gc *GlideinSetCreator) SetResourceValue(
	r Reconciler, resource metav1.Object, glideinSet *gmosv1alpha1.GlideinSet) error {
	labelsMap := labelsForPilotSet(resource.GetName())
	glideinSet.ObjectMeta.Labels = labelsMap
	glideinSet.Spec = *gc.spec
	return nil
}

func (gu *GlideinSetCreator) UpdateResourceValue(r Reconciler, glideinSet *gmosv1alpha1.GlideinSet) (bool, error) {
	glideinSet.Spec = *gu.spec
	return false, nil
}

// ResourceUpdater implementation that updates a GlideinSet based on the
// updated contents of a manifest file in Git
type GlideinSetGitUpdater struct {
	gitUpdate *gmosClient.RepoUpdate
}

func (gu *GlideinSetGitUpdater) UpdateResourceValue(r Reconciler, glideinSet *gmosv1alpha1.GlideinSet) (bool, error) {
	config, err := readManifestForNamespace(*gu.gitUpdate, glideinSet.Namespace)
	if err != nil {
		return false, err
	}
	glideinSet.RemoteManifest = &config

	glideinSet.RemoteManifest.RepoPath = gu.gitUpdate.Path
	glideinSet.RemoteManifest.CurrentCommit = gu.gitUpdate.CurrentCommit
	glideinSet.RemoteManifest.PreviousCommit = gu.gitUpdate.PreviousCommit

	return true, nil
}
