package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"oras.land/oras-go/pkg/content"

	"github.com/rancherfederal/hauler/pkg/cosign"
	"github.com/rancherfederal/hauler/pkg/store"

	"github.com/rancherfederal/hauler/pkg/log"
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

	components := strings.SplitN(targetRef, "://", 2)
	switch components[0] {
	case "dir":
		l.Debugf("identified directory target reference")
		fs := content.NewFile(components[1])
		defer fs.Close()

		_, err := s.CopyAll(ctx, fs, nil)
		if err != nil {
			return err
		}

	case "registry":
		l.Debugf("identified registry target reference")
		ropts := content.RegistryOptions{
			Username:  o.Username,
			Password:  o.Password,
			Insecure:  o.Insecure,
			PlainHTTP: o.PlainHTTP,
		}
		
		if ropts.Username != "" {
			err := cosign.RegistryLogin(ctx, s, components[1], ropts)
			if err != nil {
				return err
			}
		}

		err := cosign.LoadImages(ctx, s, components[1], ropts)
		if err != nil {
			return err
		}

	default:
		return fmt.Errorf("detecting protocol from [%s]", targetRef)
	}

	l.Infof("copied artifacts to [%s]", components[1])
	return nil
}
