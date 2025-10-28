package store

import (
	"context"
	"fmt"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/store"
)

func DeleteArtifactCmd(ctx context.Context, o *flags.DeleteArtifactOpts, s *store.Layout, ref string) error {
	l := log.FromContext(ctx)

	// collect matching artifacts
	type match struct {
		reference string
		desc      ocispec.Descriptor
	}
	var matches []match

	if err := s.Walk(func(reference string, desc ocispec.Descriptor) error {
		if !strings.Contains(reference, ref) {
			return nil
		}

		matches = append(matches, match{
			reference: reference,
			desc:      desc,
		})

		return nil // continue walking
	}); err != nil {
		return err
	}

	if len(matches) == 0 {
		return fmt.Errorf("reference [%s] not found in store (hint: use `hauler store info` to list store contents)", ref)
	}

	if len(matches) >= 1 {
		l.Infof("found %d matching references:", len(matches))
		for _, m := range matches {
			l.Infof(" - %s", m.reference)
		}
	}

	if !o.Force {
		fmt.Printf("are you sure you want to delete %d artifact(s) from the store? (yes/no) ", len(matches))

		var response string
		_, err := fmt.Scanln(&response)
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}
		switch response {
		case "yes", "y":
			l.Infof("deleting artifacts from store...")
		case "no", "n":
			l.Infof("deletion cancelled")
			return nil
		default:
			return fmt.Errorf("invalid response '%s' - please answer 'yes' or 'no'", response)
		}
	}

	//delete artifact(s)
	for _, m := range matches {
		if err := s.DeleteArtifact(ctx, m.reference, m.desc); err != nil {
			return fmt.Errorf("failed to delete artifact %s: %w", m.reference, err)
		}

		l.Infof("deleted [%s] of type %s with digest [%s]", m.reference, m.desc.MediaType, m.desc.Digest.String())
	}

	return nil
}
