package artifacts

import "github.com/google/go-containerregistry/pkg/v1"

// OCI is the bare minimum we need to represent an artifact in an oci layout
//
//	At a high level, it is not constrained by an Image's config, manifests, and layer ordinality
//	This specific implementation fully encapsulates v1.Layer's within a more generic form
type OCI interface {
	MediaType() string

	Manifest() (*v1.Manifest, error)

	RawConfig() ([]byte, error)

	Layers() ([]v1.Layer, error)
}

type OCICollection interface {
	// Contents returns the list of contents in the collection
	Contents() (map[string]OCI, error)
}
