package driver

import (
	"context"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"io"
	"sigs.k8s.io/cli-utils/pkg/object"
)

type Driver interface {
	Name() string

	//TODO: Really want this to just return a usable client
	KubeConfigPath() string

	Images(ctx context.Context) (map[name.Reference]v1.Image, error)

	Binary() (io.ReadCloser, error)

	SystemObjects() []object.ObjMetadata

	Start(io.Writer) error

	DataPath(...string) string

	WriteConfig() error
}

//NewDriver will return a new concrete Driver type given a kind
func NewDriver(driver v1alpha1.Driver) (d Driver) {
	switch driver.Type {
	case "rke2":
	//		TODO
	default:
		d = K3s{
			Version: driver.Version,
			Config: K3sConfig{
				DataDir:        "/var/lib/rancher/k3s",
				KubeConfig:     "/etc/rancher/k3s/k3s.yaml",
				KubeConfigMode: "0644",
				Disable:        nil,
			},
		}
	}

	return
}
