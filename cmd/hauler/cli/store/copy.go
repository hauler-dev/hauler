package store

import (
	"context"
	"fmt"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/spf13/cobra"
	"oras.land/oras-go/pkg/content"

	"github.com/rancherfederal/hauler/pkg/store"

	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/reference"
)

type CopyOpts struct {
	*RootOpts

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

func CopyCmd(ctx context.Context, o *CopyOpts, s *store.Layout, targetRef string) error {
	l := log.FromContext(ctx)

	var descs []ocispec.Descriptor
	components := strings.SplitN(targetRef, "://", 2)
	switch components[0] {
	case "dir":
		l.Debugf("identified directory target reference")
		fs := content.NewFile(components[1])
		defer fs.Close()

		ds, err := s.CopyAll(ctx, fs, nil)
		if err != nil {
			return err
		}
		descs = ds

	case "registry":
		l.Debugf("identified registry target reference")
		ropts := content.RegistryOptions{
			Username:  o.Username,
			Password:  o.Password,
			Insecure:  o.Insecure,
			PlainHTTP: o.PlainHTTP,
		}
		r, err := content.NewRegistry(ropts)
		if err != nil {
			return err
		}

		mapperFn := func(ref string) (string, error) {
			r, err := reference.Relocate(ref, components[1])
			if err != nil {
				return "", err
			}
			return r.Name(), nil
		}

		ds, err := s.CopyAll(ctx, r, mapperFn)
		if err != nil {
			return err
		}
		descs = ds

	default:
		return fmt.Errorf("detecting protocol from [%s]", targetRef)
	}

	l.Infof("Copied [%d] artifacts to [%s]", len(descs), components[1])
	return nil
}
