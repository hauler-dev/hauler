package store

import (
	"io"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

const (
	haulerAnnotationVendorName = "hauler"

	// The image repository without the registry base path
	AnnotationRepository = "org.rancherfederal.hauler.image.repo"
)

// DistributionStore represents a abstract store that can serve distribution contents
type DistributionStore interface {
	GetBlob(name.Reference, v1.Hash) (io.ReadCloser, error)

	GetImageManifest(string, string) (v1.Descriptor, io.ReadCloser, error)

	WriteBlob(v1.Hash, io.Reader, string) error
	PatchBlob(io.Reader, int64, int64, string) (int64, error)
	GetBlobWritePath(string) (*os.File, string, error)

	WriteManifest(*v1.Manifest) error

}

// FSStore represents a filesystem store that can add and remove oci contents
type FSStore interface {
	Add(name.Reference, ...remote.Option) error

	Remove() error
}
