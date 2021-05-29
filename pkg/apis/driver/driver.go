package driver

const (
	EtcPath = "/etc/rancher"
	VarBasePath = "/var/lib/rancher"
	ExecutableBin = "bin"
)

type Driver interface {
	Name() string
	Images() []string

	ReleaseArtifactsURL() string
	AutodeployManifestsPath() string
	PreloadImagesPath() string
	AnonymousStaticPath() string

	VarPath() string
}

type DriverConfig struct {
	NodeName string `yaml:"node-name"`
	KubeConfigMode string `yaml:"write-kubeconfig-mode"`
	NodeLabels []string `yaml:"node-label"`
}