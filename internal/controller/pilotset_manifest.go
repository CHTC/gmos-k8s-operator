package controller

type PilotSetManifiest struct {
	Version   string                    `yaml:"version"`
	Manifests []PilotSetNamespaceConfig `yaml:"manifests"`
}

type PilotSetNamespaceConfig struct {
	Namespace string              `yaml:"namespace"`
	Image     string              `yaml:"image"`
	Volume    PilotSetVolumeMount `yaml:"volume"`
	Env       []PilotSetEnv       `yaml:"env"`
	Command   []string            `yaml:"command"`
}

type PilotSetVolumeMount struct {
	Src string `yaml:"src"`
	Dst string `yaml:"dst"`
}

type PilotSetEnv struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}
