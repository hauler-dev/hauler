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

	Username  string
	Password  string
	Insecure  bool
	PlainHTTP bool
}

func (o *CopyOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.Username, "username", "u", "", "Username when copying to an authenticated remote registry")
	f.StringVarP(&o.Password, "password", "p", "", "Password when copying to an authenticated remote registry")
	f.BoolVar(&o.Insecure, "insecure", false, "Toggle allowing insecure connections when copying to a remote registry")
	f.BoolVar(&o.PlainHTTP, "plain-http", false, "Toggle allowing plain http connections when copying to a remote registry")
}

func CopyCmd(ctx context.Context, o *CopyOpts, s *store.Store, targetRef string) error {
	l := log.FromContext(ctx)

	components := strings.SplitN(targetRef, "://", 2)
	switch components[0] {
	case "dir":
		l.Debugf("identified directory target reference")
		fs := content.NewFile(components[1])
		defer fs.Close()

		if err := s.CopyAll(ctx, fs, nil); err != nil {
			return err
		}

	case "registry":
		l.Debugf("identified registry target reference")
		r, err := content.NewRegistry(content.RegistryOptions{
			Username:  o.Username,
			Password:  o.Password,
			Insecure:  o.Insecure,
			PlainHTTP: o.PlainHTTP,
		})
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

		if err := s.CopyAll(ctx, r, mapperFn); err != nil {
			return err
		}

	default:
		return errors.Errorf("determining target protocol from: [%s]", targetRef)
	}
	return nil
}
