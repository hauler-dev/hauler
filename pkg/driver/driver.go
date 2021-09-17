package driver

import (
	"context"
	"io"

	"sigs.k8s.io/cli-utils/pkg/object"
)

type Driver interface {
	Name() string

	//TODO: Really want this to just return a usable client
	KubeConfigPath() string

	Images(ctx context.Context) ([]string, error)

	BinaryFetchURL() string

	SystemObjects() []object.ObjMetadata

	Start(io.Writer) error

	DataPath(...string) string

	WriteConfig() error
}

//NewDriver will return a new concrete Driver type given a kind
// TODO: Add configs
func NewDriver(driverType string, version string) Driver {
	var d Driver
	switch driverType {
	case "rke2":
	//		TODO
	case "k3s":
		if version == "" {
			version = k3sDefaultVersion
		}

		d = K3s{
			Version: version,
			Config: K3sConfig{
				DataDir:        "/var/lib/rancher/k3s",
				KubeConfig:     "/etc/rancher/k3s/k3s.yaml",
				KubeConfigMode: "0644",
				Disable:        nil,
			},
		}
	default:
	}

	return d
}
