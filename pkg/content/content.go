package content

import (
	"context"
	"fmt"
	"path"

	"github.com/google/go-containerregistry/pkg/name"
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

type Content interface {
	Relocate(context.Context, string, ...Option) error

	Remove(context.Context, string) error
}

type Oci interface {
	Copy(ctx context.Context, reference name.Reference) error
}

type Option func(*options)

type options struct {
	Reference   string
	Annotations map[string]string
}

func makeOptions(opts ...Option) (*options, error) {
	o := &options{}
	return o, nil
}

// makeReference builds the contents desired relocated reference
func (o options) makeReference(registry, n, digest string) (name.Reference, error) {
	var rawref string
	if n != "" {
		rawref = n
	} else {
		rawref = o.Reference
	}

	return name.ParseReference(fmt.Sprintf("%s@%s", rawref, digest), name.WithDefaultRegistry(registry))
}

func WithReference(reference string) Option {
	return func(o *options) {
		o.Reference = reference
	}
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
