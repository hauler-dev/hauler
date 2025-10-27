package store

import (
	"context"
	"fmt"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/store"
)

func DeleteArtifactCmd(ctx context.Context, s *store.Layout, ref string) error {
	l := log.FromContext(ctx)

	// find artifact to delete
	var foundRef string
	var foundDesc ocispec.Descriptor
	found := false

	if err := s.Walk(func(reference string, desc ocispec.Descriptor) error {
		if !strings.Contains(reference, ref) {
			return nil
		}

		// found match
		found = true
		foundRef = reference
		foundDesc = desc

		return nil // continue walking
	}); err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("reference [%s] not found in store (hint: use `hauler store info` to list store contents)", ref)
	}

	//delete artifact
	if err := s.DeleteArtifact(ctx, foundRef, foundDesc); err != nil {
		return fmt.Errorf("failed to delete artifact: %w", err)
	}

	l.Infof("deleted [%s] with digest [%s]", foundDesc.MediaType, foundDesc.Digest.String())

	return nil
}
