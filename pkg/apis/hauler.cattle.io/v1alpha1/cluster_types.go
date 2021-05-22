package v1alpha1

import (
	"bufio"
	"fmt"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
)

const (
	DriverRKE2 = "rke2"
	DriverK3S = "k3s"

	HaulerBin = "hauler/bin"

	DriverVarPath = "/var/lib/rancher"
	DriverEtcPath = "/etc/rancher"
)

type DriverType string

func NewDefaultCluster(driver string) *Cluster {

	c := Cluster{
		Metadata: metav1.ObjectMeta{
			Name: "hauler",
		},
		Arch: "amd64", 		// TODO: Not used anywhere yet
		PreloadImages: []string{
			"registry:2.7.1",
			"plndr/kube-vip:0.3.4",
			"gitea/gitea:1.14.1-rootless",
		},
		AutodeployManifests: []string{},
	}

	switch driver {
	case DriverK3S:
		d := K3SDriver{
			Version:    "v1.21.1+k3s1",
			ReleaseURL: "https://github.com/k3s-io/k3s/releases/download",
			Config: K3SConfig{
				NodeName:       "hauler",
				KubeConfigMode: "0644",
				NodeLabels:     []string{"name=hauler"},
			},
		}

		c.Driver = &d

	case DriverRKE2:
		d := RKE2Driver{
			Version:    "v1.20.7+rke2r1",
			ReleaseURL: "https://github.com/rancher/rke2/releases/download",
		}

		c.Driver = &d
	}

	return &c
}

type Cluster struct {
	metav1.TypeMeta   `json:",inline"`
	Metadata metav1.ObjectMeta `json:"metadata,omitempty"`

	Driver Driver

	Arch string
	PreloadImages []string `mapstructure:"images,omitempty" ,json:"images,omitempty"`
	AutodeployManifests []string `mapstructure:"manifests,omitempty" ,json:"manifests,omitempty"`
}

type Driver interface {
	String() string
	ExecutableURL() string
	Images() []string
	ReleaseImagesURL() string
	MarshalConfig() ([]byte, error)
}

type K3SDriver struct {
	Version string
	ReleaseURL string
	Config K3SConfig
}

// K3SConfig
// TODO: Would really like to import this from k3s-io but we don't make that easy... so just do the important ones knowing we missed some
type K3SConfig struct {
	NodeName string `yaml:"node-name"`
	KubeConfigMode string `yaml:"write-kubeconfig-mode"`
	NodeLabels []string `yaml:"node-label"`
}

func (c K3SConfig) MarshalConfig() ([]byte, error) {
	return yaml.Marshal(c)
}

type RKE2Driver struct {
	Version string
	ReleaseURL string
	Config RKE2Config
}

// RKE2config
// TODO: Would really like to import this from k3s-io but we don't make that easy... so just do the important ones knowing we missed some
type RKE2Config struct {
	NodeName string `yaml:"node-name"`
	KubeConfigMode string `yaml:"write-kubeconfig-mode"`
	NodeLabels []string `yaml:"node-label"`
}

type Manifest struct {}

func (d K3SDriver) String() string {
	return "k3s"
}
func (d K3SDriver) ExecutableURL() string {
	return fmt.Sprintf("%s/%s/%s", d.ReleaseURL, d.Version, d.String())
}
func (d K3SDriver) Images() []string {
	var images []string
	resp, err := http.Get(fmt.Sprintf("%s/%s/%s-images.txt", d.ReleaseURL, d.Version, d.String()))
	if err != nil {
		fmt.Errorf("to get list of images: %w", err)
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		images = append(images, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		fmt.Errorf("reading list of images: %w", err)
	}
	return images
}
func (d K3SDriver) ReleaseImagesURL() string {
	return fmt.Sprintf("%s/%s/%s-airgap-images-amd64.tar.zst", d.ReleaseURL, d.Version, d.String())
}
func (d K3SDriver) MarshalConfig() ([]byte, error) {
	return yaml.Marshal(&d.Config)
}

func (d RKE2Driver) String() string {
	return "rke2"
}
func (d RKE2Driver) ExecutableURL() string {
	return fmt.Sprintf("%s/%s/%s.linux-amd64", d.ReleaseURL, d.Version, d.String())
}
func (d RKE2Driver) Images() []string {
	// TODO
	return nil
}
func (d RKE2Driver) ReleaseImagesURL() string {
	return fmt.Sprintf("%s/%s/%s-images.linux-amd64.tar.zst", d.ReleaseURL, d.Version, d.String())
}
func (d RKE2Driver) MarshalConfig() ([]byte, error) {
	return yaml.Marshal(d.Config)
}

func ImagePreloadPath(d Driver) string {
	return fmt.Sprintf("%s/agent/images", d.String())
}
func AutodeployManifestPath(d Driver) string {
	return fmt.Sprintf("%s/server/manifests", d.String())
}
func AnonymousPath(d Driver) string {
	return fmt.Sprintf("%s/server/static/charts", d.String())
}
func FS(d Driver) []string {
	return []string{
		HaulerBin,
		ImagePreloadPath(d),
		AutodeployManifestPath(d),
		AnonymousPath(d),
	}
}