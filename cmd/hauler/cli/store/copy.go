package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/internal/mapper"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content"
	"hauler.dev/go/hauler/pkg/cosign"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/store"
)

func CopyCmd(ctx context.Context, o *flags.CopyOpts, s *store.Layout, targetRef string, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	if o.Username != "" || o.Password != "" {
		return fmt.Errorf("--username/--password have been deprecated, please use 'hauler login'")
	}

	components := strings.SplitN(targetRef, "://", 2)
	switch components[0] {
	case "dir":
		l.Debugf("identified directory target reference of [%s]", components[1])

		// Create destination directory if it doesn't exist
		if err := os.MkdirAll(components[1], 0755); err != nil {
			return fmt.Errorf("failed to create destination directory: %w", err)
		}

		// For directory targets, extract files and charts (not images)
		err := s.Walk(func(reference string, desc ocispec.Descriptor) error {
			// Fetch the descriptor
			rc, err := s.Fetch(ctx, desc)
			if err != nil {
				l.Warnf("failed to fetch [%s]: %v", reference, err)
				return nil // Continue with other items
			}
			defer rc.Close()

			// Decode the manifest
			var m ocispec.Manifest
			if err := json.NewDecoder(rc).Decode(&m); err != nil {
				l.Warnf("failed to decode manifest for [%s]: %v", reference, err)
				return nil // Continue with other items
			}

			// Skip images - only extract files and charts for directory targets
			if m.Config.MediaType == consts.DockerConfigJSON ||
				m.Config.MediaType == consts.OCIManifestSchema1 {
				l.Debugf("skipping image [%s] for directory target", reference)
				return nil
			}

			// Create a mapper store based on the manifest type
			mapperStore, err := mapper.FromManifest(m, components[1])
			if err != nil {
				l.Warnf("failed to create mapper for [%s]: %v", reference, err)
				return nil // Continue with other items
			}

			// Copy/extract the content
			_, err = s.Copy(ctx, reference, mapperStore, "")
			if err != nil {
				l.Warnf("failed to extract [%s]: %v", reference, err)
				return nil // Continue with other items
			}

			l.Debugf("extracted [%s] to directory", reference)
			return nil
		})
		if err != nil {
			return err
		}

	case "registry":
		l.Debugf("identified registry target reference of [%s]", components[1])
		ropts := content.RegistryOptions{
			Insecure:  o.Insecure,
			PlainHTTP: o.PlainHTTP,
		}

		err := cosign.LoadImages(ctx, s, components[1], o.Only, ropts, ro)
		if err != nil {
			return err
		}

	default:
		return fmt.Errorf("detecting protocol from [%s]", targetRef)
	}

	l.Infof("copied artifacts to [%s]", components[1])
	return nil
}
