package driver

import (
	"context"
	"fmt"
)

type Driver interface {
	Name() string
	Version() string
	Template() []byte

	Images(ctx context.Context) ([]string, error)

	BinaryFetchURL() string
}

// NewDriver will return a new concrete Driver type given a kind
// TODO: Add configs
func NewDriver(kind string, version string) (Driver, error) {
	var d Driver
	switch kind {
	case "rke2":
		// TODO
	case "k3s":
		if version == "" {
			version = k3sDefaultVersion
		}

		d = K3s{
			version: version,
		}
	default:
		return nil, fmt.Errorf("%s not a recognized driver", kind)
	}

	return d, nil
}
