package content

import (
	"context"
	"path"

	godigest "github.com/opencontainers/go-digest"
)

const (
	SystemContentRepo = "hauler"

	K3sRef           = "k3s"
	Rke2Ref          = "rke2"
	FleetChartRef    = "fleet"
	FleetCRDChartRef = "fleet-crd"
	UnknownRef       = "unknown"
)

type Oci interface {
	Relocate(context.Context, string) error

	Remove(context.Context, string) error
}

// NewSystemRef will return a reference for n:ref within the system repo
func NewSystemRef(name string, reference string) string {
	var sep string
	if _, err := godigest.Parse(reference); err != nil {
		sep = ":"
	} else {
		sep = "@"
	}

	if reference == "" {
		reference = "latest"
	}

	return path.Join(SystemContentRepo, name) + sep + reference
}
