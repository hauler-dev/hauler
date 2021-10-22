package content

import (
	"context"

	"github.com/google/go-containerregistry/pkg/name"
)

type Oci interface {
	// Copy relocates content to an OCI compliant registry given a name.Reference
	Copy(ctx context.Context, reference name.Reference) error
}
