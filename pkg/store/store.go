package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/remotes"
	"github.com/containerd/errdefs"
	"github.com/google/go-containerregistry/pkg/authn"
	gname "github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"hauler.dev/go/hauler/pkg/artifacts"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content"
	"hauler.dev/go/hauler/pkg/layer"
)

type Layout struct {
	*content.OCI
	Root  string
	cache layer.Cache
}

type Options func(*Layout)

func WithCache(c layer.Cache) Options {
	return func(l *Layout) {
		l.cache = c
	}
}

func NewLayout(rootdir string, opts ...Options) (*Layout, error) {
	ociStore, err := content.NewOCI(rootdir)
	if err != nil {
		return nil, err
	}

	if err := ociStore.LoadIndex(); err != nil {
		return nil, err
	}

	l := &Layout{
		Root: rootdir,
		OCI:  ociStore,
	}

	for _, opt := range opts {
		opt(l)
	}

	return l, nil
}

// AddArtifact adds an artifacts.OCI to the store
//
//	The method to achieve this is to save artifact.OCI to a temporary directory in an OCI layout compatible form.  Once
//	saved, the entirety of the layout is copied to the store (which is just a registry).  This allows us to not only use
//	strict types to define generic content, but provides a processing pipeline suitable for extensibility.  In the
//	future we'll allow users to define their own content that must adhere either by artifact.OCI or simply an OCI layout.
func (l *Layout) AddArtifact(ctx context.Context, oci artifacts.OCI, ref string) (ocispec.Descriptor, error) {
	if l.cache != nil {
		cached := layer.OCICache(oci, l.cache)
		oci = cached
	}

	// Write manifest blob
	m, err := oci.Manifest()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	mdata, err := json.Marshal(m)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	if err := l.writeBlobData(mdata); err != nil {
		return ocispec.Descriptor{}, err
	}

	// Write config blob
	cdata, err := oci.RawConfig()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	if err := l.writeBlobData(cdata); err != nil {
		return ocispec.Descriptor{}, err
	}

	// write blob layers concurrently
	layers, err := oci.Layers()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	var g errgroup.Group
	for _, lyr := range layers {
		lyr := lyr
		g.Go(func() error {
			return l.writeLayer(lyr)
		})
	}
	if err := g.Wait(); err != nil {
		return ocispec.Descriptor{}, err
	}

	// Build index
	idx := ocispec.Descriptor{
		MediaType: string(m.MediaType),
		Digest:    digest.FromBytes(mdata),
		Size:      int64(len(mdata)),
		Annotations: map[string]string{
			consts.KindAnnotationName: consts.KindAnnotationImage,
			ocispec.AnnotationRefName: ref,
		},
		URLs:     nil,
		Platform: nil,
	}

	return idx, l.OCI.AddIndex(idx)
}

// AddArtifactCollection .
func (l *Layout) AddArtifactCollection(ctx context.Context, collection artifacts.OCICollection) ([]ocispec.Descriptor, error) {
	cnts, err := collection.Contents()
	if err != nil {
		return nil, err
	}

	var descs []ocispec.Descriptor
	for ref, oci := range cnts {
		desc, err := l.AddArtifact(ctx, oci, ref)
		if err != nil {
			return nil, err
		}
		descs = append(descs, desc)
	}
	return descs, nil
}

// AddImage fetches a container image (or full index for multi-arch images) from a remote registry
// and saves it to the store along with any associated signatures, attestations, and SBOMs
// discovered via cosign's tag convention (<digest>.sig, <digest>.att, <digest>.sbom).
// When platform is non-empty and the ref is a multi-arch index, only that platform is fetched.
func (l *Layout) AddImage(ctx context.Context, ref string, platform string, opts ...remote.Option) error {
	allOpts := append([]remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithContext(ctx),
	}, opts...)

	parsedRef, err := gname.ParseReference(ref)
	if err != nil {
		return fmt.Errorf("parsing reference %q: %w", ref, err)
	}

	desc, err := remote.Get(parsedRef, allOpts...)
	if err != nil {
		return fmt.Errorf("fetching descriptor for %q: %w", ref, err)
	}

	var imageDigest v1.Hash

	if idx, idxErr := desc.ImageIndex(); idxErr == nil && platform == "" {
		// Multi-arch image with no platform filter: save the full index.
		imageDigest, err = idx.Digest()
		if err != nil {
			return fmt.Errorf("getting index digest for %q: %w", ref, err)
		}
		if err := l.writeIndex(parsedRef, idx, consts.KindAnnotationIndex); err != nil {
			return err
		}
	} else {
		// Single-platform image, or the caller requested a specific platform.
		imgOpts := append([]remote.Option{}, allOpts...)
		if platform != "" {
			p, err := parsePlatform(platform)
			if err != nil {
				return err
			}
			imgOpts = append(imgOpts, remote.WithPlatform(p))
		}
		img, err := remote.Image(parsedRef, imgOpts...)
		if err != nil {
			return fmt.Errorf("fetching image %q: %w", ref, err)
		}
		imageDigest, err = img.Digest()
		if err != nil {
			return fmt.Errorf("getting image digest for %q: %w", ref, err)
		}
		if err := l.writeImage(parsedRef, img, consts.KindAnnotationImage, ""); err != nil {
			return err
		}
	}

	if err := l.saveRelatedArtifacts(ctx, parsedRef, imageDigest, allOpts...); err != nil {
		return err
	}
	return l.saveReferrers(ctx, parsedRef, imageDigest, allOpts...)
}

// writeImageBlobs writes all blobs for a single image (layers, config, manifest) to the store's
// blob directory. It does not add an entry to the OCI index.
func (l *Layout) writeImageBlobs(img v1.Image) error {
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("getting layers: %w", err)
	}
	var g errgroup.Group
	for _, lyr := range layers {
		lyr := lyr
		g.Go(func() error { return l.writeLayer(lyr) })
	}
	if err := g.Wait(); err != nil {
		return err
	}

	cfgData, err := img.RawConfigFile()
	if err != nil {
		return fmt.Errorf("getting config: %w", err)
	}
	if err := l.writeBlobData(cfgData); err != nil {
		return fmt.Errorf("writing config blob: %w", err)
	}

	manifestData, err := img.RawManifest()
	if err != nil {
		return fmt.Errorf("getting manifest: %w", err)
	}
	return l.writeBlobData(manifestData)
}

// writeImage writes all blobs for img and adds a descriptor entry to the OCI index with the
// given annotationRef and kind. containerdName overrides the io.containerd.image.name annotation;
// if empty it defaults to annotationRef.Name().
func (l *Layout) writeImage(annotationRef gname.Reference, img v1.Image, kind string, containerdName string) error {
	if err := l.writeImageBlobs(img); err != nil {
		return err
	}

	mt, err := img.MediaType()
	if err != nil {
		return fmt.Errorf("getting media type: %w", err)
	}
	hash, err := img.Digest()
	if err != nil {
		return fmt.Errorf("getting digest: %w", err)
	}
	d, err := digest.Parse(hash.String())
	if err != nil {
		return fmt.Errorf("parsing digest: %w", err)
	}
	raw, err := img.RawManifest()
	if err != nil {
		return fmt.Errorf("getting raw manifest size: %w", err)
	}

	if containerdName == "" {
		containerdName = annotationRef.Name()
	}
	desc := ocispec.Descriptor{
		MediaType: string(mt),
		Digest:    d,
		Size:      int64(len(raw)),
		Annotations: map[string]string{
			consts.KindAnnotationName:     kind,
			ocispec.AnnotationRefName:     strings.TrimPrefix(annotationRef.Name(), annotationRef.Context().RegistryStr()+"/"),
			consts.ContainerdImageNameKey: containerdName,
		},
	}
	return l.OCI.AddIndex(desc)
}

// writeIndexBlobs recursively writes all child image blobs for an image index to the store's blob
// directory. It does not write the top-level index manifest or add index entries.
func (l *Layout) writeIndexBlobs(idx v1.ImageIndex) error {
	manifest, err := idx.IndexManifest()
	if err != nil {
		return fmt.Errorf("getting index manifest: %w", err)
	}

	for _, childDesc := range manifest.Manifests {
		// Try as a nested index first, then fall back to a regular image.
		if childIdx, err := idx.ImageIndex(childDesc.Digest); err == nil {
			if err := l.writeIndexBlobs(childIdx); err != nil {
				return err
			}
			raw, err := childIdx.RawManifest()
			if err != nil {
				return fmt.Errorf("getting nested index manifest: %w", err)
			}
			if err := l.writeBlobData(raw); err != nil {
				return err
			}
		} else {
			childImg, err := idx.Image(childDesc.Digest)
			if err != nil {
				return fmt.Errorf("getting child image %v: %w", childDesc.Digest, err)
			}
			if err := l.writeImageBlobs(childImg); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeIndex writes all blobs for an image index (including all child platform images) and adds
// a descriptor entry to the OCI index with the given annotationRef and kind.
func (l *Layout) writeIndex(annotationRef gname.Reference, idx v1.ImageIndex, kind string) error {
	if err := l.writeIndexBlobs(idx); err != nil {
		return err
	}

	raw, err := idx.RawManifest()
	if err != nil {
		return fmt.Errorf("getting index manifest: %w", err)
	}
	if err := l.writeBlobData(raw); err != nil {
		return fmt.Errorf("writing index manifest blob: %w", err)
	}

	mt, err := idx.MediaType()
	if err != nil {
		return fmt.Errorf("getting index media type: %w", err)
	}
	hash, err := idx.Digest()
	if err != nil {
		return fmt.Errorf("getting index digest: %w", err)
	}
	d, err := digest.Parse(hash.String())
	if err != nil {
		return fmt.Errorf("parsing index digest: %w", err)
	}

	desc := ocispec.Descriptor{
		MediaType: string(mt),
		Digest:    d,
		Size:      int64(len(raw)),
		Annotations: map[string]string{
			consts.KindAnnotationName:     kind,
			ocispec.AnnotationRefName:     strings.TrimPrefix(annotationRef.Name(), annotationRef.Context().RegistryStr()+"/"),
			consts.ContainerdImageNameKey: annotationRef.Name(),
		},
	}
	return l.OCI.AddIndex(desc)
}

// saveReferrers discovers and saves OCI 1.1 referrers for the image identified by ref/hash.
// This captures cosign v3 new-bundle-format signatures/attestations stored as OCI referrers
// (via the subject field) rather than the legacy sha256-<hex>.sig/.att/.sbom tag convention.
// go-containerregistry handles both the native referrers API and the tag-based fallback.
// Missing referrers and fetch errors are logged at debug level and silently skipped.
func (l *Layout) saveReferrers(ctx context.Context, ref gname.Reference, hash v1.Hash, opts ...remote.Option) error {
	log := zerolog.Ctx(ctx)

	imageDigestRef, err := gname.NewDigest(ref.Context().String() + "@" + hash.String())
	if err != nil {
		log.Debug().Err(err).Msgf("saveReferrers: could not construct digest ref for %s", ref.Name())
		return nil
	}

	idx, err := remote.Referrers(imageDigestRef, opts...)
	if err != nil {
		// Most registries that don't support the referrers API return 404; not an error.
		log.Debug().Err(err).Msgf("no OCI referrers found for %s@%s", ref.Name(), hash)
		return nil
	}

	idxManifest, err := idx.IndexManifest()
	if err != nil {
		log.Debug().Err(err).Msgf("saveReferrers: could not read referrers index for %s", ref.Name())
		return nil
	}

	for _, referrerDesc := range idxManifest.Manifests {
		digestRef, err := gname.NewDigest(ref.Context().String() + "@" + referrerDesc.Digest.String())
		if err != nil {
			log.Debug().Err(err).Msgf("saveReferrers: could not construct digest ref for referrer %s", referrerDesc.Digest)
			continue
		}

		img, err := remote.Image(digestRef, opts...)
		if err != nil {
			log.Debug().Err(err).Msgf("saveReferrers: could not fetch referrer manifest %s", referrerDesc.Digest)
			continue
		}

		// Embed the referrer manifest digest in the kind annotation so that multiple
		// referrers for the same base image each get a unique entry in the OCI index.
		kind := consts.KindAnnotationReferrers + "/" + referrerDesc.Digest.Hex
		if err := l.writeImage(ref, img, kind, ""); err != nil {
			return fmt.Errorf("saving OCI referrer %s for %s: %w", referrerDesc.Digest, ref.Name(), err)
		}
		log.Debug().Msgf("saved OCI referrer %s (%s) for %s", referrerDesc.Digest, string(referrerDesc.ArtifactType), ref.Name())
	}
	return nil
}

// saveRelatedArtifacts discovers and saves cosign-compatible signature, attestation, and SBOM
// artifacts for the image identified by ref/hash. Missing artifacts are silently skipped.
func (l *Layout) saveRelatedArtifacts(ctx context.Context, ref gname.Reference, hash v1.Hash, opts ...remote.Option) error {
	// Cosign tag convention: "sha256:hexvalue" → "sha256-hexvalue.sig" / ".att" / ".sbom"
	tagPrefix := strings.ReplaceAll(hash.String(), ":", "-")

	related := []struct {
		tag  string
		kind string
	}{
		{tagPrefix + ".sig", consts.KindAnnotationSigs},
		{tagPrefix + ".att", consts.KindAnnotationAtts},
		{tagPrefix + ".sbom", consts.KindAnnotationSboms},
	}

	for _, r := range related {
		artifactRef, err := gname.ParseReference(ref.Context().String() + ":" + r.tag)
		if err != nil {
			continue
		}
		img, err := remote.Image(artifactRef, opts...)
		if err != nil {
			// Artifact doesn't exist at this registry; skip silently.
			continue
		}
		if err := l.writeImage(ref, img, r.kind, ""); err != nil {
			return fmt.Errorf("saving %s for %s: %w", r.kind, ref.Name(), err)
		}
	}
	return nil
}

// parsePlatform parses a platform string in "os/arch[/variant]" format into a v1.Platform.
func parsePlatform(s string) (v1.Platform, error) {
	parts := strings.SplitN(s, "/", 3)
	if len(parts) < 2 {
		return v1.Platform{}, fmt.Errorf("invalid platform %q: expected os/arch[/variant]", s)
	}
	p := v1.Platform{OS: parts[0], Architecture: parts[1]}
	if len(parts) == 3 {
		p.Variant = parts[2]
	}
	return p, nil
}

// Flush is a fancy name for delete-all-the-things, in this case it's as trivial as deleting oci-layout content
//
//	This can be a highly destructive operation if the store's directory happens to be inline with other non-store contents
//	To reduce the blast radius and likelihood of deleting things we don't own, Flush explicitly deletes oci-layout content only
func (l *Layout) Flush(ctx context.Context) error {
	blobs := filepath.Join(l.Root, ocispec.ImageBlobsDir)
	if err := os.RemoveAll(blobs); err != nil {
		return err
	}

	index := filepath.Join(l.Root, ocispec.ImageIndexFile)
	if err := os.RemoveAll(index); err != nil {
		return err
	}

	layout := filepath.Join(l.Root, ocispec.ImageLayoutFile)
	if err := os.RemoveAll(layout); err != nil {
		return err
	}

	return nil
}

// Copy will copy a given reference to a given content.Target
//
//	This is essentially a replacement for oras.Copy, custom implementation for content stores
func (l *Layout) Copy(ctx context.Context, ref string, to content.Target, toRef string) (ocispec.Descriptor, error) {
	// Resolve the source descriptor
	desc, err := l.OCI.Resolve(ctx, ref)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to resolve reference: %w", err)
	}

	// Get fetcher and pusher
	fetcher, err := l.OCI.Fetcher(ctx, ref)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to get fetcher: %w", err)
	}

	pusher, err := to.Pusher(ctx, toRef)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("failed to get pusher: %w", err)
	}

	// Recursively copy the descriptor graph (matches oras.Copy behavior)
	if err := l.copyDescriptorGraph(ctx, desc, fetcher, pusher); err != nil {
		return ocispec.Descriptor{}, err
	}

	return desc, nil
}

// copyDescriptorGraph recursively copies a descriptor and all its referenced content
// This matches the behavior of oras.Copy by walking the entire descriptor graph
func (l *Layout) copyDescriptorGraph(ctx context.Context, desc ocispec.Descriptor, fetcher remotes.Fetcher, pusher remotes.Pusher) (err error) {
	switch desc.MediaType {
	case ocispec.MediaTypeImageManifest, consts.DockerManifestSchema2:
		// Fetch and parse the manifest
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

		// Copy config blob
		if err := l.copyDescriptor(ctx, manifest.Config, fetcher, pusher); err != nil {
			return fmt.Errorf("failed to copy config: %w", err)
		}

		// Copy all layer blobs
		for _, layer := range manifest.Layers {
			if err := l.copyDescriptor(ctx, layer, fetcher, pusher); err != nil {
				return fmt.Errorf("failed to copy layer: %w", err)
			}
		}

		// Push the manifest itself using the already-fetched data to avoid double-fetching
		if err := l.pushData(ctx, desc, data, pusher); err != nil {
			return fmt.Errorf("failed to push manifest: %w", err)
		}

	case ocispec.MediaTypeImageIndex, consts.DockerManifestListSchema2:
		// Fetch and parse the index
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

		// Recursively copy each child (could be manifest or nested index)
		for _, child := range index.Manifests {
			if err := l.copyDescriptorGraph(ctx, child, fetcher, pusher); err != nil {
				return fmt.Errorf("failed to copy child: %w", err)
			}
		}

		// Push the index itself using the already-fetched data to avoid double-fetching
		if err := l.pushData(ctx, desc, data, pusher); err != nil {
			return fmt.Errorf("failed to push index: %w", err)
		}

	default:
		// For other types (config blobs, layers, etc.), just copy the blob
		if err := l.copyDescriptor(ctx, desc, fetcher, pusher); err != nil {
			return fmt.Errorf("failed to copy descriptor: %w", err)
		}
	}

	return nil
}

// copyDescriptor copies a single descriptor from source to target
func (l *Layout) copyDescriptor(ctx context.Context, desc ocispec.Descriptor, fetcher remotes.Fetcher, pusher remotes.Pusher) (err error) {
	// Fetch the content
	rc, err := fetcher.Fetch(ctx, desc)
	if err != nil {
		return err
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

	// Copy the content
	n, err := io.Copy(writer, rc)
	if err != nil {
		return err
	}

	// Commit the written content with the expected digest
	if err := writer.Commit(ctx, n, desc.Digest); err != nil {
		return err
	}
	zerolog.Ctx(ctx).Debug().Msgf("pushed blob: %s", desc.Digest)
	return nil
}

// pushData pushes already-fetched data to the pusher without re-fetching.
// This is used when we've already read the data for parsing and want to avoid double-fetching.
func (l *Layout) pushData(ctx context.Context, desc ocispec.Descriptor, data []byte, pusher remotes.Pusher) (err error) {
	// Get a writer from the pusher
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

	// Write the data using io.Copy to handle short writes properly
	n, err := io.Copy(writer, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	// Commit the written content with the expected digest
	return writer.Commit(ctx, n, desc.Digest)
}

// CopyAll performs bulk copy operations on the stores oci layout to a provided target
func (l *Layout) CopyAll(ctx context.Context, to content.Target, toMapper func(string) (string, error)) ([]ocispec.Descriptor, error) {
	var descs []ocispec.Descriptor
	err := l.OCI.Walk(func(reference string, desc ocispec.Descriptor) error {
		// Use the clean reference from annotations (without -kind suffix) as the base
		// The reference parameter from Walk is the nameMap key with format "ref-kind",
		// but we need the clean ref for the destination to avoid double-appending kind
		baseRef := desc.Annotations[ocispec.AnnotationRefName]
		if baseRef == "" {
			return fmt.Errorf("descriptor %s missing required annotation %q", reference, ocispec.AnnotationRefName)
		}
		toRef := baseRef
		if toMapper != nil {
			tr, err := toMapper(baseRef)
			if err != nil {
				return err
			}
			toRef = tr
		}

		// Append the digest to help the target pusher identify the root descriptor
		// Format: "reference@digest" allows the pusher to update its index.json
		if desc.Digest.Validate() == nil {
			toRef = fmt.Sprintf("%s@%s", toRef, desc.Digest)
		}

		desc, err := l.Copy(ctx, reference, to, toRef)
		if err != nil {
			return err
		}

		descs = append(descs, desc)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return descs, nil
}

// Identify is a helper function that will identify a human-readable content type given a descriptor
func (l *Layout) Identify(ctx context.Context, desc ocispec.Descriptor) string {
	rc, err := l.OCI.Fetch(ctx, desc)
	if err != nil {
		return ""
	}
	defer rc.Close()

	m := struct {
		Config struct {
			MediaType string `json:"mediaType"`
		} `json:"config"`
	}{}
	if err := json.NewDecoder(rc).Decode(&m); err != nil {
		return ""
	}

	return m.Config.MediaType
}

func (l *Layout) writeBlobData(data []byte) error {
	blob := static.NewLayer(data, "") // NOTE: MediaType isn't actually used in the writing
	return l.writeLayer(blob)
}

func (l *Layout) writeLayer(layer v1.Layer) error {
	d, err := layer.Digest()
	if err != nil {
		return err
	}

	r, err := layer.Compressed()
	if err != nil {
		return err
	}

	dir := filepath.Join(l.Root, ocispec.ImageBlobsDir, d.Algorithm)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil && !os.IsExist(err) {
		return err
	}

	blobPath := filepath.Join(dir, d.Hex)
	// Skip entirely if something exists, assume layer is present already
	if _, err := os.Stat(blobPath); err == nil {
		return nil
	}

	w, err := os.Create(blobPath)
	if err != nil {
		return err
	}
	defer w.Close()

	_, err = io.Copy(w, r)
	return err
}

// Remove artifact reference from the store
func (l *Layout) RemoveArtifact(ctx context.Context, reference string, desc ocispec.Descriptor) error {
	if err := l.OCI.LoadIndex(); err != nil {
		return err
	}

	l.OCI.RemoveFromIndex(reference)
	return l.OCI.SaveIndex()
}

func (l *Layout) CleanUp(ctx context.Context) (int, int64, error) {
	referencedDigests := make(map[string]bool)

	if err := l.OCI.LoadIndex(); err != nil {
		return 0, 0, fmt.Errorf("failed to load index: %w", err)
	}

	var processManifest func(desc ocispec.Descriptor) error
	processManifest = func(desc ocispec.Descriptor) (err error) {
		if desc.Digest.Validate() != nil {
			return nil
		}

		// mark digest as referenced by existing artifact
		referencedDigests[desc.Digest.Hex()] = true

		// fetch and parse manifests for layer digests
		rc, err := l.OCI.Fetch(ctx, desc)
		if err != nil {
			return nil // skip if can't be read
		}
		defer func() {
			if closeErr := rc.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
		}()

		var manifest struct {
			Config struct {
				Digest digest.Digest `json:"digest"`
			} `json:"config"`
			Layers []struct {
				digest.Digest `json:"digest"`
			} `json:"layers"`
			Manifests []struct {
				Digest    digest.Digest `json:"digest"`
				MediaType string        `json:"mediaType"`
				Size      int64         `json:"size"`
			} `json:"manifests"`
		}

		if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
			return nil
		}

		// handle image manifest
		if manifest.Config.Digest.Validate() == nil {
			referencedDigests[manifest.Config.Digest.Hex()] = true
		}

		for _, layer := range manifest.Layers {
			if layer.Digest.Validate() == nil {
				referencedDigests[layer.Digest.Hex()] = true
			}
		}

		// handle manifest list
		for _, m := range manifest.Manifests {
			if m.Digest.Validate() == nil {
				// mark manifest
				referencedDigests[m.Digest.Hex()] = true
				// process manifest for layers
				manifestDesc := ocispec.Descriptor{
					MediaType: m.MediaType,
					Digest:    m.Digest,
					Size:      m.Size,
				}
				processManifest(manifestDesc) // calls helper func on manifests in list
			}
		}

		return nil
	}

	// walk through artifacts
	if err := l.OCI.Walk(func(reference string, desc ocispec.Descriptor) error {
		return processManifest(desc)
	}); err != nil {
		return 0, 0, fmt.Errorf("failed to walk artifacts: %w", err)
	}

	// read all entries
	blobsPath := filepath.Join(l.Root, "blobs", "sha256")
	entries, err := os.ReadDir(blobsPath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read blobs directory: %w", err)
	}

	// track count and size of deletions
	deletedCount := 0
	var deletedSize int64

	// scan blobs
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		digest := entry.Name()

		if !referencedDigests[digest] {
			blobPath := filepath.Join(blobsPath, digest)
			if info, err := entry.Info(); err == nil {
				deletedSize += info.Size()
			}

			if err := os.Remove(blobPath); err != nil {
				return deletedCount, deletedSize, fmt.Errorf("failed to remove blob %s: %w", digest, err)
			}
			deletedCount++
		}
	}

	return deletedCount, deletedSize, nil
}
