package content

import (
	"context"
)

type Oci interface {
	Relocate(context.Context, string) error

	Remove(context.Context, string) error
}
