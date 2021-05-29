package driver

import (
	"fmt"
	"github.com/rancherfederal/hauler/pkg/util"
	"net/http"
	"path/filepath"
)

const (
	K3sDefaultReleasesURL = "https://github.com/k3s-io/k3s/releases/download"
	K3sDefaultVersion = "v1.21.1+k3s1"
	K3sExecutable = "k3s"
)

const (
	k3sDriverName = "k3s"
)

type K3sDriver struct {
	Version string `yaml:"version"`

	Config K3sConfig
}

type K3sConfig struct {
	DriverConfig `yaml:"config,inline"`
}

func (k K3sDriver) Name() string { return k3sDriverName }
func (k K3sDriver) Images() []string {
	//TODO: Don't panic!!
	resp, err := http.Get(fmt.Sprintf("%s/%s/%s-images.txt", K3sDefaultReleasesURL, k.Version, k.Name()))
	if err != nil {
		panic("failed getting images")
	}
	defer resp.Body.Close()

	images, err := util.LinesToSlice(resp.Body)
	if err != nil {
		panic("failed getting images")
	}

	return images
}

func (k K3sDriver) ReleaseArtifactsURL() string { return fmt.Sprintf("%s/%s", K3sDefaultReleasesURL, k.Version) }
func (k K3sDriver) PreloadImagesPath() string { return "agent/images" }
func (k K3sDriver) AutodeployManifestsPath() string { return "server/manifests" }
func (k K3sDriver) AnonymousStaticPath() string { return "server/static" }
func (k K3sDriver) VarPath() string { return filepath.Join(VarBasePath, k.Name()) }