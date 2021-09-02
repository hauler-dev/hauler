package store

import (
	"io"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

const (
	haulerAnnotationVendorName = "hauler"

	// The image repository without the registry base path
	AnnotationRepository = "org.rancherfederal.hauler.image.repo"
)

type Store interface {
	Blob(name.Reference, v1.Hash) (io.ReadCloser, error)

	ImageManifest(string, string) (v1.Descriptor, io.ReadCloser, error)
}
