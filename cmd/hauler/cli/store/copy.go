package store

import (
	"context"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"oras.land/oras-go/pkg/content"

	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type CopyOpts struct {
	Target string
}

func (o *CopyOpts) AddFlags(cmd *cobra.Command) {
	_ = cmd.Flags()
}

func CopyCmd(ctx context.Context, o *CopyOpts, s *store.Store, targetRef string) error {
	l := log.FromContext(ctx)
	_ = l

	components := strings.SplitN(targetRef, "://", 2)
	switch components[0] {
	case "dir":
		fs := content.NewFile(components[1])
		defer fs.Close()

		if err := s.Copy(ctx, fs, nil); err != nil {
			return err
		}

	case "registry":
		r, err := content.NewRegistry(content.RegistryOptions{})
		if err != nil {
			return err
		}

		mapperFn := func(reference string) (string, error) {
			ref, err := store.RelocateReference(reference, components[1])
			if err != nil {
				return "", err
			}
			return ref.Name(), nil
		}

		if err := s.Copy(ctx, r, mapperFn); err != nil {
			return err
		}

	default:
		return errors.Errorf("determining target protocol from: [%s]", targetRef)

	}
	return nil
}
