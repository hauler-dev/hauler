package artifact

import v1 "github.com/google/go-containerregistry/pkg/v1"

type Config interface {
	// Raw returns the config bytes
	Raw() ([]byte, error)

	Descriptor() (v1.Descriptor, error)
}
