package artifact

import (
	"github.com/google/go-containerregistry/pkg/v1"
)

// OCI is the bare minimum we need to represent an artifact in an OCI layout
// Oci is a general form of v1.Image that conforms to the OCI artifacts-spec instead of the images-spec
//  At a high level, it is not constrained by an Image's config, manifests, and layer ordinality
//  This specific implementation fully encapsulates v1.Layer's within a more generic form
type OCI interface {
	MediaType() string

	// ManifestData() ([]byte, error)
	Manifest() (*v1.Manifest, error)

	RawConfig() ([]byte, error)

	Layers() ([]v1.Layer, error)
}
