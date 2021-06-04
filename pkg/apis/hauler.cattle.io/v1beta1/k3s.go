package v1beta1

import (
	"fmt"
	"github.com/rancherfederal/hauler/pkg/util"
	"net/http"
	"net/url"
	"os"
)

const (
	K3sDriverName = "k3s"
	K3sExecutable = "k3s"

	K3sDefaultReleasesURL = "https://github.com/k3s-io/k3s/releases/download"
	K3sDefaultVersion = "v1.21.1+k3s1"
)

type K3sDriver struct {
	Type string `json:"type"`
	Version string `json:"version"`

	Config K3sConfig `json:"config"`
}

type K3sConfig struct {
	NodeName string `json:"node-name"`
	Selinux bool `json:"selinux"`
	KubeConfigMode os.FileMode `json:"write-kubeconfig-mode"`
}

func (k K3sDriver) Name() string { return K3sDriverName }
func (k K3sDriver) Images() ([]string, error) {
	u, err := url.Parse(fmt.Sprintf("%s/%s/%s-images.txt", K3sDefaultReleasesURL, k.Version, k.Name))
	if err != nil {
		return nil, fmt.Errorf("error building %s url", k.Name)
	}

	resp, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return util.LinesToSlice(resp.Body)
}