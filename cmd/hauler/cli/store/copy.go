package store

import (
	"context"
	"fmt"
	"strings"

	"oras.land/oras-go/pkg/content"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/cosign"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/store"
)

func CopyCmd(ctx context.Context, o *flags.CopyOpts, s *store.Layout, targetRef string) error {
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
