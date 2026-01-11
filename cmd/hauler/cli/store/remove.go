package store

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/store"
)

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

func RemoveCmd(ctx context.Context, o *flags.RemoveOpts, s *store.Layout, ref string) error {
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
