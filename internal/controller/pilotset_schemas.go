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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

const EMPTY_MAP_KEY string = "sample.cfg"

type EmptySecretCreator struct {
}

func (*EmptySecretCreator) setResourceValue(
	r Reconciler, resource metav1.Object, secret *corev1.Secret) error {
	secret.Data = map[string][]byte{
		EMPTY_MAP_KEY: {},
	}
	return nil
}

func labelsForPilotSet(name string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "GlideinSetCollection",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/created-by": "controller-manager",
	}
}

func readManifestForNamespace(gitUpdate gmosClient.RepoUpdate, namespace string, name string) (config gmosv1alpha1.PilotSetNamespaceConfig, err error) {
	manifest := &PilotSetManifiest{}
	data, err := os.ReadFile(filepath.Join(gitUpdate.Path, "glidein-manifest.yaml"))
	if err != nil {
		return
	}

	if err = yaml.Unmarshal(data, manifest); err != nil {
		return
	}
	for _, config := range manifest.Manifests {
		if config.Namespace == namespace && (config.Name == "" || config.Name == name) {
			return config, nil
		}
	}
	return config, fmt.Errorf("no config found for namespace %v in manifest %+v", namespace, manifest)
}

type DataSecretTemplate struct {
	metav1.ObjectMeta
	Data map[string][]byte
}

func makeDataSecretTemplate(glideinSet *gmosv1alpha1.GlideinSet) (template DataSecretTemplate, err error) {
	template.ObjectMeta = metav1.ObjectMeta{
		Name:      glideinSet.GetName(),
		Namespace: glideinSet.GetNamespace(),
	}
	manifest := glideinSet.RemoteManifest
	if manifest == nil {
		return
	}

	listing, err := os.ReadDir(filepath.Join(manifest.RepoPath, manifest.Volume.Src))
	if err != nil && errors.Is(err, os.ErrNotExist) {
		// Corner case where operator hasn't cloned local copies yet, nothing to do
		return template, nil

	} else if err != nil {
		return
	}

	template.Data = make(map[string][]byte)
	for _, entry := range listing {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(manifest.RepoPath, manifest.Volume.Src, entry.Name()))
		if err != nil {
			return template, err
		}
		template.Data[entry.Name()] = data
	}

	return
}

// ResourceUpdater implementation that updates a Secret's value based on the
// updated contents of a Secret in the glidein manager
type TokenSecretValueUpdater struct {
	secSource *gmosv1alpha1.PilotSetSecretSource
	secValue  *gmosClient.SecretValue
}

func (du *TokenSecretValueUpdater) updateResourceValue(r Reconciler, sec *corev1.Secret) (bool, error) {
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
	manifest     *gmosv1alpha1.PilotSetNamespaceConfig
	collectorUrl string
}

type GlideinSetCreator struct {
	spec *gmosv1alpha1.GlideinSetSpec
}

func (gc *GlideinSetCreator) setResourceValue(
	r Reconciler, resource metav1.Object, glideinSet *gmosv1alpha1.GlideinSet) error {
	labelsMap := labelsForPilotSet(resource.GetName())
	glideinSet.ObjectMeta.Labels = labelsMap
	glideinSet.Spec = *gc.spec
	return nil
}

func (gu *GlideinSetCreator) updateResourceValue(r Reconciler, glideinSet *gmosv1alpha1.GlideinSet) (bool, error) {
	glideinSet.Spec = *gu.spec
	return true, nil
}

// ResourceUpdater implementation that updates a GlideinSet based on the
// updated contents of a manifest file in Git
type GlideinSetGitUpdater struct {
	gitUpdate *gmosClient.RepoUpdate
}

func (gu *GlideinSetGitUpdater) updateResourceValue(r Reconciler, glideinSet *gmosv1alpha1.GlideinSet) (bool, error) {
	config, err := readManifestForNamespace(*gu.gitUpdate, glideinSet.Namespace, glideinSet.Spec.Name)
	if err != nil {
		return false, err
	}
	glideinSet.RemoteManifest = &config

	glideinSet.RemoteManifest.RepoPath = gu.gitUpdate.Path
	glideinSet.RemoteManifest.CurrentCommit = gu.gitUpdate.CurrentCommit
	glideinSet.RemoteManifest.PreviousCommit = gu.gitUpdate.PreviousCommit

	return true, nil
}
