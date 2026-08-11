package content

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/errdefs"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

// CopyDescriptorGraph recursively copies desc and everything it references from fetcher to pusher.
func CopyDescriptorGraph(ctx context.Context, desc ocispec.Descriptor, fetcher remotes.Fetcher, pusher remotes.Pusher) (err error) {
	switch desc.MediaType {
	case ocispec.MediaTypeImageManifest, consts.DockerManifestSchema2:
		rc, err := fetcher.Fetch(ctx, desc)
		if err != nil {
			return fmt.Errorf("failed to fetch manifest: %w", err)
		}
		defer func() {
			if closeErr := rc.Close(); closeErr != nil && err == nil {
				err = fmt.Errorf("failed to close manifest reader: %w", closeErr)
			}
		}()

		data, err := io.ReadAll(rc)
		if err != nil {
			return fmt.Errorf("failed to read manifest: %w", err)
		}

		var manifest ocispec.Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return fmt.Errorf("failed to unmarshal manifest: %w", err)
		}

		if err := copyDescriptor(ctx, manifest.Config, fetcher, pusher); err != nil {
			return fmt.Errorf("failed to copy config: %w", err)
		}

		for _, layer := range manifest.Layers {
			if err := copyDescriptor(ctx, layer, fetcher, pusher); err != nil {
				return fmt.Errorf("failed to copy layer: %w", err)
			}
		}

		// push the manifest itself using the already-fetched data to avoid double-fetching
		if err := pushData(ctx, desc, data, pusher); err != nil {
			return fmt.Errorf("failed to push manifest: %w", err)
		}

	case ocispec.MediaTypeImageIndex, consts.DockerManifestListSchema2:
		rc, err := fetcher.Fetch(ctx, desc)
		if err != nil {
			return fmt.Errorf("failed to fetch index: %w", err)
		}
		defer func() {
			if closeErr := rc.Close(); closeErr != nil && err == nil {
				err = fmt.Errorf("failed to close index reader: %w", closeErr)
			}
		}()

		data, err := io.ReadAll(rc)
		if err != nil {
			return fmt.Errorf("failed to read index: %w", err)
		}

		var index ocispec.Index
		if err := json.Unmarshal(data, &index); err != nil {
			return fmt.Errorf("failed to unmarshal index: %w", err)
		}

		for _, child := range index.Manifests {
			if err := CopyDescriptorGraph(ctx, child, fetcher, pusher); err != nil {
				return fmt.Errorf("failed to copy child: %w", err)
			}
		}

		// push the index itself using the already-fetched data to avoid double-fetching
		if err := pushData(ctx, desc, data, pusher); err != nil {
			return fmt.Errorf("failed to push index: %w", err)
		}

	default:
		if err := copyDescriptor(ctx, desc, fetcher, pusher); err != nil {
			return fmt.Errorf("failed to copy descriptor: %w", err)
		}
	}

	return nil
}

// copyDescriptor copies a single descriptor from fetcher to pusher.
func copyDescriptor(ctx context.Context, desc ocispec.Descriptor, fetcher remotes.Fetcher, pusher remotes.Pusher) (err error) {
	rc, err := fetcher.Fetch(ctx, desc)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := rc.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close reader: %w", closeErr)
		}
	}()

	writer, err := pusher.Push(ctx, desc)
	if err != nil {
		if errdefs.IsAlreadyExists(err) {
			zerolog.Ctx(ctx).Debug().Msgf("existing blob: %s", desc.Digest)
			return nil // content already present on remote
		}
		return err
	}
	defer func() {
		if closeErr := writer.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	n, err := io.Copy(writer, rc)
	if err != nil {
		return err
	}

	if err := writer.Commit(ctx, n, desc.Digest); err != nil {
		return err
	}
	zerolog.Ctx(ctx).Debug().Msgf("pushed blob: %s", desc.Digest)
	return nil
}

// pushData pushes already-fetched data to the pusher without re-fetching --
// used once a manifest/index's bytes are already in hand from parsing it.
func pushData(ctx context.Context, desc ocispec.Descriptor, data []byte, pusher remotes.Pusher) (err error) {
	writer, err := pusher.Push(ctx, desc)
	if err != nil {
		if errdefs.IsAlreadyExists(err) {
			return nil // content already present on remote
		}
		return fmt.Errorf("failed to get writer: %w", err)
	}
	defer func() {
		if closeErr := writer.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close writer: %w", closeErr)
		}
	}()

	n, err := io.Copy(writer, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return writer.Commit(ctx, n, desc.Digest)
}
