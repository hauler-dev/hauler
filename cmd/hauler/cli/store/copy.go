package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/containerd/containerd/remotes"
	"github.com/containerd/errdefs"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/internal/mapper"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/retry"
	"hauler.dev/go/hauler/pkg/store"
)

func CopyCmd(ctx context.Context, o *flags.CopyOpts, s *store.Layout, targetRef string, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	if o.Username != "" || o.Password != "" {
		return fmt.Errorf("--username/--password have been deprecated, please use 'hauler login'")
	}

	if !s.IndexExists() {
		return fmt.Errorf("store index not found: run 'hauler store add/sync/load' first")
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
			// Skip cosign sig/att/sbom artifacts — they're registry-only metadata,
			// not extractable as files or charts.
			kind := desc.Annotations[consts.KindAnnotationName]
			switch kind {
			case consts.KindAnnotationSigs, consts.KindAnnotationAtts, consts.KindAnnotationSboms:
				l.Debugf("skipping cosign artifact [%s] for directory target", reference)
				return nil
			}

			// Handle different media types
			switch desc.MediaType {
			case ocispec.MediaTypeImageIndex, consts.DockerManifestListSchema2:
				// Multi-platform index - process each child manifest
				rc, err := s.Fetch(ctx, desc)
				if err != nil {
					l.Warnf("failed to fetch index [%s]: %v", reference, err)
					return nil
				}

				var index ocispec.Index
				if err := json.NewDecoder(rc).Decode(&index); err != nil {
					if cerr := rc.Close(); cerr != nil {
						l.Warnf("failed to close index reader for [%s]: %v", reference, cerr)
					}
					l.Warnf("failed to decode index for [%s]: %v", reference, err)
					return nil
				}

				// Close rc immediately after decoding - we're done reading from it
				if cerr := rc.Close(); cerr != nil {
					l.Warnf("failed to close index reader for [%s]: %v", reference, cerr)
				}

				// Process each manifest in the index
				for _, manifestDesc := range index.Manifests {
					manifestRC, err := s.Fetch(ctx, manifestDesc)
					if err != nil {
						l.Warnf("failed to fetch child manifest: %v", err)
						continue
					}

					var m ocispec.Manifest
					if err := json.NewDecoder(manifestRC).Decode(&m); err != nil {
						manifestRC.Close()
						l.Warnf("failed to decode child manifest: %v", err)
						continue
					}
					manifestRC.Close()

					// Skip images - only extract files and charts
					if m.Config.MediaType == consts.DockerConfigJSON ||
						m.Config.MediaType == ocispec.MediaTypeImageConfig {
						l.Debugf("skipping image manifest in index [%s]", reference)
						continue
					}

					// Create mapper and extract
					mapperStore, err := mapper.FromManifest(m, components[1])
					if err != nil {
						l.Warnf("failed to create mapper for child: %v", err)
						continue
					}

					// Note: We can't call s.Copy with manifestDesc because it's not in the nameMap
					// Instead, we need to manually push through the mapper
					if err := extractManifestContent(ctx, s, manifestDesc, m, mapperStore); err != nil {
						l.Warnf("failed to extract child: %v", err)
						continue
					}

					l.Debugf("extracted child manifest from index [%s]", reference)
				}

			case ocispec.MediaTypeImageManifest, consts.DockerManifestSchema2:
				// Single-platform manifest
				rc, err := s.Fetch(ctx, desc)
				if err != nil {
					l.Warnf("failed to fetch [%s]: %v", reference, err)
					return nil
				}

				var m ocispec.Manifest
				if err := json.NewDecoder(rc).Decode(&m); err != nil {
					rc.Close()
					l.Warnf("failed to decode manifest for [%s]: %v", reference, err)
					return nil
				}

				// Skip images - only extract files and charts for directory targets
				if m.Config.MediaType == consts.DockerConfigJSON ||
					m.Config.MediaType == ocispec.MediaTypeImageConfig {
					rc.Close()
					l.Debugf("skipping image [%s] for directory target", reference)
					return nil
				}

				// Create a mapper store based on the manifest type
				mapperStore, err := mapper.FromManifest(m, components[1])
				if err != nil {
					rc.Close()
					l.Warnf("failed to create mapper for [%s]: %v", reference, err)
					return nil
				}

				// Copy/extract the content
				_, err = s.Copy(ctx, reference, mapperStore, "")
				if err != nil {
					rc.Close()
					l.Warnf("failed to extract [%s]: %v", reference, err)
					return nil
				}
				rc.Close()

				l.Debugf("extracted [%s] to directory", reference)

			default:
				l.Debugf("skipping unsupported media type [%s] for [%s]", desc.MediaType, reference)
			}

			return nil
		})
		if err != nil {
			return err
		}

	case "registry":
		l.Debugf("identified registry target reference of [%s]", components[1])
		registryOpts := content.RegistryOptions{
			PlainHTTP: o.PlainHTTP,
			Insecure:  o.Insecure,
		}

		// Pre-build a map from base ref → image manifest digest so that sig/att/sbom
		// descriptors (which store the base image ref, not the cosign tag) can be routed
		// to the correct destination tag using the cosign tag convention.
		refDigest := make(map[string]string)
		_ = s.Walk(func(_ string, desc ocispec.Descriptor) error {
			kind := desc.Annotations[consts.KindAnnotationName]
			if kind == consts.KindAnnotationImage || kind == consts.KindAnnotationIndex {
				if baseRef := desc.Annotations[ocispec.AnnotationRefName]; baseRef != "" {
					refDigest[baseRef] = desc.Digest.String()
				}
			}
			return nil
		})

		sigExts := map[string]string{
			consts.KindAnnotationSigs:  ".sig",
			consts.KindAnnotationAtts:  ".att",
			consts.KindAnnotationSboms: ".sbom",
		}

		var fatalErr error
		err := s.Walk(func(reference string, desc ocispec.Descriptor) error {
			if fatalErr != nil {
				return nil
			}
			baseRef := desc.Annotations[ocispec.AnnotationRefName]
			if baseRef == "" {
				return nil
			}
			if o.Only != "" && !strings.Contains(baseRef, o.Only) {
				l.Debugf("skipping [%s] (not matching --only filter)", baseRef)
				return nil
			}

			// For sig/att/sbom descriptors, derive the cosign tag from the parent
			// image's manifest digest rather than using AnnotationRefName directly.
			destRef := baseRef
			kind := desc.Annotations[consts.KindAnnotationName]
			if ext, isSigKind := sigExts[kind]; isSigKind {
				if imgDigest, ok := refDigest[baseRef]; ok {
					digestTag := strings.ReplaceAll(imgDigest, ":", "-")
					repo := baseRef
					if colon := strings.LastIndex(baseRef, ":"); colon != -1 {
						repo = baseRef[:colon]
					}
					destRef = repo + ":" + digestTag + ext
				}
			}

			toRef, err := content.RewriteRefToRegistry(destRef, components[1])
			if err != nil {
				l.Warnf("failed to rewrite ref [%s]: %v", baseRef, err)
				return nil
			}
			l.Infof("%s", destRef)
			// A fresh target per artifact gives each push its own in-memory status
			// tracker. Containerd's tracker keys blobs by digest only (not repo),
			// so a shared tracker would mark shared blobs as "already exists" after
			// the first image, skipping the per-repository blob link creation that
			// Docker Distribution requires for manifest validation.
			target := content.NewRegistryTarget(components[1], registryOpts)
			var pushed ocispec.Descriptor
			if err := retry.Operation(ctx, o.StoreRootOpts, ro, func() error {
				var copyErr error
				pushed, copyErr = s.Copy(ctx, reference, target, toRef)
				return copyErr
			}); err != nil {
				if !ro.IgnoreErrors {
					fatalErr = err
				}
				return nil
			}
			l.Infof("%s: digest: %s size: %d", toRef, pushed.Digest, pushed.Size)
			return nil
		})
		if fatalErr != nil {
			return fatalErr
		}
		if err != nil {
			return err
		}

	default:
		return fmt.Errorf("detecting protocol from [%s]", targetRef)
	}

	l.Infof("copied artifacts to [%s]", components[1])
	return nil
}

// extractManifestContent extracts a manifest's layers through a mapper target
// This is used for child manifests in indexes that aren't in the store's nameMap
func extractManifestContent(ctx context.Context, s *store.Layout, desc ocispec.Descriptor, m ocispec.Manifest, target content.Target) error {
	// Get a pusher from the target
	pusher, err := target.Pusher(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to get pusher: %w", err)
	}

	// Copy config blob
	if err := copyBlobDescriptor(ctx, s, m.Config, pusher); err != nil {
		return fmt.Errorf("failed to copy config: %w", err)
	}

	// Copy each layer blob
	for _, layer := range m.Layers {
		if err := copyBlobDescriptor(ctx, s, layer, pusher); err != nil {
			return fmt.Errorf("failed to copy layer: %w", err)
		}
	}

	// Copy the manifest itself
	if err := copyBlobDescriptor(ctx, s, desc, pusher); err != nil {
		return fmt.Errorf("failed to copy manifest: %w", err)
	}

	return nil
}

// copyBlobDescriptor copies a single descriptor blob from the store to a pusher
func copyBlobDescriptor(ctx context.Context, s *store.Layout, desc ocispec.Descriptor, pusher remotes.Pusher) (err error) {
	// Fetch the content from the store
	rc, err := s.OCI.Fetch(ctx, desc)
	if err != nil {
		return fmt.Errorf("failed to fetch blob: %w", err)
	}
	defer func() {
		if closeErr := rc.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close reader: %w", closeErr)
		}
	}()

	// Get a writer from the pusher
	writer, err := pusher.Push(ctx, desc)
	if err != nil {
		if errdefs.IsAlreadyExists(err) {
			return nil // content already present on remote
		}
		return fmt.Errorf("failed to push: %w", err)
	}
	defer func() {
		if closeErr := writer.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close writer: %w", closeErr)
		}
	}()

	// Copy the content
	n, err := io.Copy(writer, rc)
	if err != nil {
		return fmt.Errorf("failed to copy content: %w", err)
	}

	// Commit the written content
	if err := writer.Commit(ctx, n, desc.Digest); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	return nil
}