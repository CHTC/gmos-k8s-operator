// Yaml schema for GlideinManagerPilotSet config provided by a Glidein Manager Git Repo
package controller

import (
	gmosv1alpha1 "github.com/chtc/gmos-k8s-operator/api/v1alpha1"
)

type PilotSetManifiest struct {
	Version   string                                 `yaml:"version"`
	Manifests []gmosv1alpha1.PilotSetNamespaceConfig `yaml:"manifests"`
}
