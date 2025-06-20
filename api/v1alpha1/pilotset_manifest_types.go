package v1alpha1

import "path"

// Config for a single namespace provided by an upstream glidein manager git repo
type PilotSetNamespaceConfig struct {
	Namespace    string               `yaml:"namespace" json:"namespace,omitempty"`
	Image        string               `yaml:"image" json:"image"`
	Security     PilotSetSecurity     `yaml:"security" json:"security,omitempty"`
	Volume       PilotSetVolumeMount  `yaml:"volume" json:"volume,omitempty"`
	SecretSource PilotSetSecretSource `yaml:"secretSource" json:"secretSource,omitempty"`
	Env          []PilotSetEnv        `yaml:"env" json:"env,omitempty"`
	Command      []string             `yaml:"command" json:"command,omitempty"`

	// TODO a bit odd to include a couple k8s only fields here
	RepoPath             string `json:"path"`
	CurrentCommit        string `json:"currentCommit"`
	PreviousCommit       string `json:"previousCommit,omitempty"`
	CurrentSecretVersion string `json:"currentSecretVersion,omitempty"`
}

// Config for which subdirectory of the git repo should be converted into a ConfigMap
// and volume mounted into the Glidein
type PilotSetVolumeMount struct {
	Src string `yaml:"src" json:"src"`
	Dst string `yaml:"dst" json:"dst"`
}

// Config for which secret on the Glidein Manager that should be mounted as a file
// into the Glidein
type PilotSetSecretSource struct {
	SecretName string `yaml:"secretName" json:"secretName"`
	Dst        string `yaml:"dst" json:"dst"`
}

// helper func on PilotSetSecretSource to split the Dst field into path and filename
func (ps PilotSetSecretSource) DstDir() string {
	return path.Dir(ps.Dst)
}

// Config for the user and group that the Glidein container should run as
type PilotSetSecurity struct {
	User  int64 `yaml:"user" json:"user"`
	Group int64 `yaml:"group" json:"group"`
}

// Config for a key/value pair that should be placed into the Glidein's environment
type PilotSetEnv struct {
	Name  string `yaml:"name" json:"name"`
	Value string `yaml:"value" json:"value"`
}
