package store

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/audit"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/store"
)

// artifactType derives a human-readable content type for an artifact the same way `store info`
// does: from the manifest's config media type, since AddArtifact stores every non-image-command
// artifact (files, charts) under the same "kind" annotation and can't distinguish them
func artifactType(ctx context.Context, s *store.Layout, desc ocispec.Descriptor) string {
	switch {
	case desc.Annotations[consts.KindAnnotationName] == consts.KindAnnotationSigs:
		return "sigs"
	case desc.Annotations[consts.KindAnnotationName] == consts.KindAnnotationAtts:
		return "atts"
	case desc.Annotations[consts.KindAnnotationName] == consts.KindAnnotationSboms:
		return "sbom"
	case strings.HasPrefix(desc.Annotations[consts.KindAnnotationName], consts.KindAnnotationReferrers):
		return "referrer"
	case desc.MediaType == consts.OCIImageIndexSchema, desc.MediaType == consts.DockerManifestListSchema2:
		return "image"
	}

	rc, err := s.Fetch(ctx, desc)
	if err != nil {
		return "image"
	}
	defer rc.Close()

	var m ocispec.Manifest
	if err := json.NewDecoder(rc).Decode(&m); err != nil {
		return "image"
	}

	switch m.Config.MediaType {
	case consts.ChartConfigMediaType:
		return "chart"
	case consts.FileLocalConfigMediaType, consts.FileHttpConfigMediaType, consts.FileDirectoryConfigMediaType:
		return "file"
	default:
		return "image"
	}
}

func formatReference(ref string) string {
	tagIdx := strings.LastIndex(ref, ":")
	if tagIdx == -1 {
		return ref
	}

	dashIdx := strings.Index(ref[tagIdx+1:], "-")
	if dashIdx == -1 {
		return ref
	}

	dashIdx = tagIdx + 1 + dashIdx

	base := ref[:dashIdx]
	suffix := ref[dashIdx+1:]

	if base == "" || suffix == "" {
		return ref
	}

	return fmt.Sprintf("%s [%s]", base, suffix)
}

func RemoveCmd(ctx context.Context, o *flags.RemoveOpts, s *store.Layout, ref string, ro *flags.CliRootOpts, rso *flags.StoreRootOpts) error {
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
		return fmt.Errorf("reference [%s] not found in store (use `hauler store info` to list store contents)", ref)
	}

	if len(matches) >= 1 {
		l.Infof("found %d matching references:", len(matches))
		for _, m := range matches {
			l.Infof("  - [%s]", formatReference(m.reference))
		}
	}

	if !o.Force {
		fmt.Printf("  ↳ are you sure you want to remove [%d] artifact(s) from the store? (yes/no) ", len(matches))

		reader := bufio.NewReader(os.Stdin)

		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("failed to read response: [%w]... please answer 'yes' or 'no'", err)
		}

		response := strings.ToLower(strings.TrimSpace(line))

		switch response {
		case "yes", "y":
			l.Infof("starting to remove artifacts from store...")
		case "no", "n":
			l.Infof("successfully cancelled removal of artifacts from store")
			return nil
		case "":
			return fmt.Errorf("failed to read response... please answer 'yes' or 'no'")
		default:
			return fmt.Errorf("invalid response [%s]... please answer 'yes' or 'no'", response)
		}
	}

	// remove artifact(s)
	for _, m := range matches {
		if err := s.RemoveArtifact(ctx, m.reference, m.desc); err != nil {
			return fmt.Errorf("failed to remove artifact [%s]: %w", formatReference(m.reference), err)
		}

		if auditLevel(ro) != "none" {
			cleanRef := m.desc.Annotations[consts.ContainerdImageNameKey]
			if cleanRef == "" {
				cleanRef = m.desc.Annotations[ocispec.AnnotationRefName]
			}
			e := audit.Entry{
				StoreID:   s.StoreID,
				Store:     s.Root,
				Type:      artifactType(ctx, s, m.desc),
				Command:   "store remove",
				Args:      []string{ref},
				Reference: cleanRef,
				Digest:    m.desc.Digest.String(),
			}
			if auditLevel(ro) == "verbose" {
				sys := audit.BuildSystem()
				g := audit.BuildGlobal(ro, rso)
				e.System = &sys
				e.Global = &g
				e.Flags = map[string]any{
					"force": o.Force,
				}
			}
			if err := audit.Append(ro.HaulerDir, e); err != nil {
				l.Warnf("failed to write audit entry: %v", err)
			}
			l.Debugf("generated audit id of [%s]", audit.ID())
		} else {
			l.Debugf("generated audit id of [none]")
		}

		l.Infof("successfully removed [%s] of type [%s] with digest [%s]", formatReference(m.reference), m.desc.MediaType, m.desc.Digest.String())
	}

	// clean up unreferenced blobs
	l.Infof("cleaning up all unreferenced blobs...")
	removedCount, removedSize, err := s.CleanUp(ctx)
	if err != nil {
		l.Warnf("garbage collection failed: [%v]", err)
	} else if removedCount > 0 {
		l.Infof("successfully removed [%d] unreferenced blobs [freed %d bytes]", removedCount, removedSize)
	}

	return nil
}
