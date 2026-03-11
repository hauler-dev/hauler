package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/internal/mapper"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/reference"
	"hauler.dev/go/hauler/pkg/store"
)

// isIndexMediaType returns true for OCI and Docker manifest list media types.
func isIndexMediaType(mt string) bool {
	return mt == ocispec.MediaTypeImageIndex || mt == consts.DockerManifestListSchema2
}

// firstLeafManifest walks a (potentially nested) OCI index and returns the
// decoded manifest of the first non-index child. It prefers non-index children
// at each level; if all children are indexes it descends into the first one.
// Returns an error if any nested index or manifest cannot be decoded.
func firstLeafManifest(ctx context.Context, s *store.Layout, idx ocispec.Index) (ocispec.Manifest, error) {
	for {
		if len(idx.Manifests) == 0 {
			return ocispec.Manifest{}, fmt.Errorf("image index has no child manifests")
		}

		// Prefer the first non-index child; fall back to the first child (an index) if all are indexes.
		desc := idx.Manifests[0]
		for _, d := range idx.Manifests {
			if !isIndexMediaType(d.MediaType) {
				desc = d
				break
			}
		}

		rc, err := s.Fetch(ctx, desc)
		if err != nil {
			return ocispec.Manifest{}, err
		}

		if isIndexMediaType(desc.MediaType) {
			var nested ocispec.Index
			err = json.NewDecoder(rc).Decode(&nested)
			rc.Close()
			if err != nil {
				return ocispec.Manifest{}, fmt.Errorf("decoding nested index: %w", err)
			}
			idx = nested
			continue
		}

		var m ocispec.Manifest
		err = json.NewDecoder(rc).Decode(&m)
		rc.Close()
		if err != nil {
			return ocispec.Manifest{}, fmt.Errorf("decoding child manifest: %w", err)
		}
		return m, nil
	}
}

// isContainerImageManifest returns true when the manifest describes a real
// container image — i.e. an OCI/Docker image config with no AnnotationTitle on
// any layer. File artifacts distributed as OCI images always carry AnnotationTitle
// on their layers, so they are NOT considered container images by this check.
func isContainerImageManifest(m ocispec.Manifest) bool {
	switch m.Config.MediaType {
	case consts.DockerConfigJSON, ocispec.MediaTypeImageConfig:
		for _, layer := range m.Layers {
			if _, ok := layer.Annotations[ocispec.AnnotationTitle]; ok {
				return false
			}
		}
		return true
	}
	return false
}

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

		// For image indexes, decoding the index JSON as ocispec.Manifest produces
		// an empty Config.MediaType and nil Layers — causing FromManifest to fall
		// back to Default() mapper, which writes config blobs as sha256:<digest>.bin.
		// Instead, peek at the first child manifest to get real config/layer info.
		var m ocispec.Manifest
		if desc.MediaType == ocispec.MediaTypeImageIndex || desc.MediaType == consts.DockerManifestListSchema2 {
			var idx ocispec.Index
			if err := json.NewDecoder(rc).Decode(&idx); err != nil {
				return err
			}
			if len(idx.Manifests) == 0 {
				l.Warnf("skipping [%s]: image index has no child manifests", reference)
				return nil
			}
			var err error
			m, err = firstLeafManifest(ctx, s, idx)
			if err != nil {
				return err
			}
		} else {
			if err := json.NewDecoder(rc).Decode(&m); err != nil {
				return err
			}
		}

		// Container images (no AnnotationTitle on any layer) are not extractable
		// to disk in a meaningful way — use `hauler store copy` to push to a registry.
		if isContainerImageManifest(m) {
			l.Warnf("skipping [%s]: container images cannot be extracted (use `hauler store copy` to push to a registry)", reference)
			return nil
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
