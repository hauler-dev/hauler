package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"hauler.dev/go/hauler/pkg/consts"
)

// EnsureOCILayout patches index.json to inject cosign metadata and writes oci-layout to produce a valid layout
func EnsureOCILayout(dir string) error {
	idxPath := filepath.Join(dir, ocispec.ImageIndexFile)
	raw, err := os.ReadFile(idxPath)
	if err != nil {
		return fmt.Errorf("read index.json: %w", err)
	}

	var idx ocispec.Index
	if err := json.Unmarshal(raw, &idx); err != nil {
		return fmt.Errorf("parse index.json: %w", err)
	}

	// only mutate the annotations
	for i, desc := range idx.Manifests {
		if desc.Annotations == nil {
			desc.Annotations = make(map[string]string)
		}
		if full, ok := desc.Annotations[consts.ContainerdImageNameAnnotation]; ok {
			desc.Annotations[ocispec.AnnotationRefName] = full
		}
		kind := consts.KindAnnotationImage
		if desc.MediaType == consts.OCIImageIndexSchema {
			kind = consts.KindAnnotationIndex
		}
		desc.Annotations[consts.KindAnnotationName] = kind
		idx.Manifests[i] = desc
	}

	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index.json: %w", err)
	}
	if err := os.WriteFile(idxPath, out, 0644); err != nil {
		return fmt.Errorf("write index.json: %w", err)
	}

	// add the oci-layout file
	layout := []byte(`{"imageLayoutVersion":"1.0.0"}`)
	if err := os.WriteFile(
		filepath.Join(dir, ocispec.ImageLayoutFile),
		layout,
		0644,
	); err != nil {
		return fmt.Errorf("write oci-layout: %w", err)
	}

	return nil
}
