// Yaml schema for GlideinManagerPilotSet config provided by a Glidein Manager Git Repo
package controller

type PilotSetManifiest struct {
	Version   string                    `yaml:"version"`
	Manifests []PilotSetNamespaceConfig `yaml:"manifests"`
}

type PilotSetNamespaceConfig struct {
	Namespace    string               `yaml:"namespace"`
	Image        string               `yaml:"image"`
	Security     PilotSetSecurity     `yaml:"security"`
	Volume       PilotSetVolumeMount  `yaml:"volume"`
	SecretSource PilotSetSecretSource `yaml:"secretSource"`
	Env          []PilotSetEnv        `yaml:"env"`
	Command      []string             `yaml:"command"`
}

// Config for which subdirectory of the git repo should be converted into a ConfigMap
// and volume mounted into the Glidein
type PilotSetVolumeMount struct {
	Src string `yaml:"src"`
	Dst string `yaml:"dst"`
}

// Config for which secret on the Glidein Manager that should be mounted as a file
// into the Glidein
type PilotSetSecretSource struct {
	SecretName string `yaml:"secretName"`
	Dst        string `yaml:"dst"`
}

// Config for the user and group that the Glidein container should run as
type PilotSetSecurity struct {
	User  int64 `yaml:"user"`
	Group int64 `yaml:"group"`
}

// Config for a key/value pair that should be placed into the Glidein's environment
type PilotSetEnv struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}
