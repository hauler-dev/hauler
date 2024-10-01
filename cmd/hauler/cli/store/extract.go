package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/internal/mapper"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/reference"
	"hauler.dev/go/hauler/pkg/store"
)

func ExtractCmd(ctx context.Context, o *flags.ExtractOpts, s *store.Layout, ref string) error {
	l := log.FromContext(ctx)

	r, err := reference.Parse(ref)
	if err != nil {
		return err
	}

	// use the repository from the context and the identifier from the reference
	repo := r.Context().RepositoryStr() + ":" + r.Identifier()

	found := false
	if err := s.Walk(func(reference string, desc ocispec.Descriptor) error {
		if !strings.Contains(reference, repo) {
			return nil
		}
		found = true

		rc, err := s.Fetch(ctx, desc)
		if err != nil {
			return err
		}
		defer rc.Close()

		var m ocispec.Manifest
		if err := json.NewDecoder(rc).Decode(&m); err != nil {
			return err
		}

		mapperStore, err := mapper.FromManifest(m, o.DestinationDir)
		if err != nil {
			return err
		}

		pushedDesc, err := s.Copy(ctx, reference, mapperStore, "")
		if err != nil {
			return err
		}

		l.Infof("extracted [%s] from store with digest [%s]", pushedDesc.MediaType, pushedDesc.Digest.String())

		return nil
	}); err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("reference [%s] not found in store (hint: use `hauler store info` to list store contents)", ref)
	}

	return nil
}
