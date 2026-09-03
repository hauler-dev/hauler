package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/v2/core/remotes"
	"github.com/containerd/errdefs"
	"github.com/google/go-containerregistry/pkg/authn"
	goname "github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	ggcrtransport "github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/google/uuid"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"hauler.dev/go/hauler/v2/internal/version"
	"hauler.dev/go/hauler/v2/pkg/artifacts"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/content"
	"hauler.dev/go/hauler/v2/pkg/layer"
	href "hauler.dev/go/hauler/v2/pkg/reference"
)

type Layout struct {
	*content.OCI
	Root      string
	StoreID   string
	haulerDir string
	cache     layer.Cache

	// blobConcurrency overrides the OCI layout's default blob-write
	// concurrency ceiling (content.OCI.blobSem) when > 0. Set via
	// WithBlobConcurrency; must be known before content.NewOCI is called, so
	// NewLayout applies opts before constructing the OCI store.
	blobConcurrency int
}

type Options func(*Layout)

func WithCache(c layer.Cache) Options {
	return func(l *Layout) {
		l.cache = c
	}
}

// WithHaulerDir records this store in <haulerDir>/stores.json so it can
// later be referenced by StoreID. If unset, NewLayout skips the inventory
func WithHaulerDir(dir string) Options {
	return func(l *Layout) {
		l.haulerDir = dir
	}
}

// WithBlobConcurrency overrides the OCI layout's default blob-write
// concurrency ceiling. See content.WithBlobConcurrency for the mechanics and
// why the floor in flags.BlobConcurrencyFor matters.
func WithBlobConcurrency(n int) Options {
	return func(l *Layout) {
		l.blobConcurrency = n
	}
}

func NewLayout(rootdir string, opts ...Options) (*Layout, error) {
	l := &Layout{
		Root:    rootdir,
		StoreID: loadOrCreateStoreID(rootdir),
	}

	for _, opt := range opts {
		opt(l)
	}

	var ociOpts []content.OCIOption
	if l.blobConcurrency > 0 {
		ociOpts = append(ociOpts, content.WithBlobConcurrency(l.blobConcurrency))
	}
	ociStore, err := content.NewOCI(rootdir, ociOpts...)
	if err != nil {
		return nil, err
	}
	if err := ociStore.LoadIndex(); err != nil {
		return nil, err
	}
	l.OCI = ociStore

	if l.haulerDir != "" {
		updateStoreInventory(l.haulerDir, l.StoreID, rootdir)
	}

	return l, nil
}

type storeMetadata struct {
	StoreID       string `json:"store-id"`
	HaulerVersion string `json:"hauler-version"`
}

// loadOrCreateStoreID returns the persistent store identity from <rootdir>/store.json,
// creating the file with a fresh UUID on first use.
func loadOrCreateStoreID(rootdir string) string {
	metaPath := filepath.Join(rootdir, consts.DefaultStoreMetadataName)
	if data, err := os.ReadFile(metaPath); err == nil {
		var m storeMetadata
		if uerr := json.Unmarshal(data, &m); uerr == nil && m.StoreID != "" {
			return m.StoreID
		} else if uerr != nil {
			zlog.Warn().Err(uerr).Str("path", metaPath).Msg("failed to parse store metadata... generating new store id")
		} else {
			zlog.Warn().Str("path", metaPath).Msg("store metadata missing store-id... generating new store id")
		}
	}
	m := storeMetadata{
		StoreID:       uuid.New().String(),
		HaulerVersion: version.GetVersionInfo().GitVersion,
	}
	data, err := json.Marshal(m)
	if err != nil {
		zlog.Warn().Err(err).Msg("failed to marshal store metadata... store id will not persist across runs")
		return m.StoreID
	}
	tmp := metaPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		zlog.Warn().Err(err).Str("path", tmp).Msg("failed to write store metadata... store id will not persist across runs")
		return m.StoreID
	}
	if err := os.Rename(tmp, metaPath); err != nil {
		zlog.Warn().Err(err).Str("path", metaPath).Msg("failed to write store metadata... store id will not persist across runs")
	}
	return m.StoreID
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
	if err := l.writeBlobData(ctx, mdata); err != nil {
		return ocispec.Descriptor{}, err
	}

	// Write config blob
	cdata, err := oci.RawConfig()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	if err := l.writeBlobData(ctx, cdata); err != nil {
		return ocispec.Descriptor{}, err
	}

	// write blob layers concurrently
	layers, err := oci.Layers()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	// errgroup.WithContext, not a zero-value Group: Wait() on a zero-value
	// group never cancels siblings on failure, so other layers would keep
	// downloading after one fails. gctx is cancelled the moment any
	// writeLayer errors, which content.OCI.WriteBlob observes (via ctx.Err()
	// and the wrapped reader) to abort in-flight writes promptly.
	g, gctx := errgroup.WithContext(ctx)
	for _, lyr := range layers {
		lyr := lyr
		g.Go(func() error {
			return l.writeLayer(gctx, lyr)
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
// When excludeExtras is true, cosign signatures, attestations, SBOMs, and OCI referrers are skipped.
//
// pinnedDigest, when non-empty, is the digest actually fetched -- ref supplies
// only the name recorded in the index annotations. Callers that verified a
// signature pass the digest they verified, so the bytes stored are provably
// the bytes checked even if the tag moves mid-run. An empty pinnedDigest
// resolves ref normally.
//
// insecureSkipTLSVerify and caFile configure the transport used for every
// registry round trip this call makes (the image itself plus any related
// signatures/attestations/SBOMs/referrers); insecureSkipTLSVerify takes
// precedence over caFile -- see content.BuildTransport.
func (l *Layout) AddImage(ctx context.Context, ref string, platform string, excludeExtras bool, pinnedDigest string, insecureSkipTLSVerify bool, caFile string, opts ...remote.Option) (string, error) {
	tr, err := content.BuildTransport(insecureSkipTLSVerify, caFile)
	if err != nil {
		return "", err
	}

	allOpts := append([]remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithContext(ctx),
		remote.WithTransport(tr),
	}, opts...)

	parsedRef, err := href.ParseReference(ref)
	if err != nil {
		return "", fmt.Errorf("parsing reference %q: %w", ref, err)
	}

	// fetchRef drives the network; parsedRef stays the annotation ref so the
	// store keeps recording the tag a user asked for rather than a digest.
	fetchRef := parsedRef
	if pinnedDigest != "" {
		fetchRef = parsedRef.Context().Digest(pinnedDigest)
	}

	desc, err := remote.Get(fetchRef, allOpts...)
	if err != nil {
		return "", fmt.Errorf("fetching descriptor for %q: %w", ref, err)
	}

	// go-containerregistry already validates content against a digest ref;
	// this restates the invariant locally so a future refactor that switches
	// fetchRef back to a tag fails loudly instead of silently unpinning.
	if pinnedDigest != "" && desc.Digest.String() != pinnedDigest {
		return "", fmt.Errorf("digest mismatch for %q: fetched %s, pinned %s", ref, desc.Digest, pinnedDigest)
	}

	var imageDigest v1.Hash
	// Non-nil only for the full-index path below; collectSubjectDigests walks
	// it to probe every child manifest for cosign artifacts. Left nil for the
	// platform-filtered and single-arch paths, which stay single-subject.
	var savedIdx v1.ImageIndex

	if idx, idxErr := desc.ImageIndex(); idxErr == nil && platform == "" {
		// Multi-arch image with no platform filter: save the full index.
		imageDigest, err = idx.Digest()
		if err != nil {
			return "", fmt.Errorf("getting index digest for %q: %w", ref, err)
		}
		if err := l.writeIndex(ctx, parsedRef, idx, consts.KindAnnotationIndex, ""); err != nil {
			return "", err
		}
		savedIdx = idx
	} else {
		// Single-platform image, or the caller requested a specific platform.
		//
		// Under a platform filter the pinned digest is the index's while the stored
		// digest is the selected child's. The child is content-addressed within the
		// verified index, so the chain of trust holds.
		imgOpts := append([]remote.Option{}, allOpts...)
		if platform != "" {
			p, err := parsePlatform(platform)
			if err != nil {
				return "", err
			}
			imgOpts = append(imgOpts, remote.WithPlatform(p))
		}
		img, err := remote.Image(fetchRef, imgOpts...)
		if err != nil {
			return "", fmt.Errorf("fetching image %q: %w", ref, err)
		}
		imageDigest, err = img.Digest()
		if err != nil {
			return "", fmt.Errorf("getting image digest for %q: %w", ref, err)
		}
		if err := l.writeImage(ctx, parsedRef, img, consts.KindAnnotationImage, "", ""); err != nil {
			return "", err
		}
	}

	if !excludeExtras {
		// One subject for a single-platform store; the index digest plus every
		// child manifest for a full multi-arch store. Serial on purpose: sync
		// already parallelizes across images, and serial probing bounds
		// rate-limit bursts against one registry.
		subjects := []v1.Hash{imageDigest}
		if savedIdx != nil {
			var err error
			subjects, err = collectSubjectDigests(savedIdx)
			if err != nil {
				return "", err
			}
		}
		saved := make(map[string]bool)
		for i, subject := range subjects {
			topLevel := i == 0
			if err := l.saveRelatedArtifacts(ctx, parsedRef, subject, topLevel, saved, allOpts...); err != nil {
				return "", err
			}
			if err := l.saveReferrers(ctx, parsedRef, subject, saved, allOpts...); err != nil {
				return "", err
			}
		}
	}
	return imageDigest.String(), nil
}

// AddLocalImage fetches a container image from the local Docker daemon and saves it to the store.
// No cosign signatures, attestations, SBOMs, or OCI referrers are fetched (registry-only concepts).
func (l *Layout) AddLocalImage(ctx context.Context, ref string) (string, error) {
	parsedRef, err := href.ParseReference(ref)
	if err != nil {
		return "", fmt.Errorf("parsing reference %q: %w", ref, err)
	}

	if err := ensureDockerHost(); err != nil {
		return "", fmt.Errorf("failed to locate Docker daemon socket: %w -- is the Docker daemon running?", err)
	}

	img, err := daemon.Image(parsedRef, daemon.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("failed to fetch image from Docker daemon: %w -- is the Docker daemon running?", err)
	}

	d, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("getting image digest for %q: %w", ref, err)
	}

	if err := l.writeImage(ctx, parsedRef, img, consts.KindAnnotationImage, "", ""); err != nil {
		return "", err
	}
	return d.String(), nil
}

// ensureDockerHost sets DOCKER_HOST if it is not already set and the default
// socket (/var/run/docker.sock) does not exist. Docker Desktop on macOS places
// its socket at ~/.docker/run/docker.sock instead of the default path.
func ensureDockerHost() error {
	if os.Getenv("DOCKER_HOST") != "" {
		return nil
	}

	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	sock := filepath.Join(home, ".docker", "run", "docker.sock")
	if _, err := os.Stat(sock); err != nil {
		return fmt.Errorf("no Docker socket found at /var/run/docker.sock or %s", sock)
	}
	if err := os.Setenv("DOCKER_HOST", "unix://"+sock); err != nil {
		return fmt.Errorf("setting DOCKER_HOST: %w", err)
	}
	return nil
}

// writeImageBlobs writes all blobs for a single image (layers, config, manifest) to the store's
// blob directory. It does not add an entry to the OCI index.
func (l *Layout) writeImageBlobs(ctx context.Context, img v1.Image) error {
	layers, err := img.Layers()
	if err != nil {
		return fmt.Errorf("getting layers: %w", err)
	}

	if stats := imageStatsFromContext(ctx); stats != nil {
		var totalBytes int64
		for _, lyr := range layers {
			size, err := lyr.Size()
			if err != nil {
				return fmt.Errorf("getting layer size: %w", err)
			}
			totalBytes += size
		}
		stats.Layers.Add(int64(len(layers)))
		stats.Bytes.Add(totalBytes)
	}

	// See AddArtifact's identical errgroup.WithContext conversion for why this
	// can't stay a zero-value errgroup.Group.
	g, gctx := errgroup.WithContext(ctx)
	for _, lyr := range layers {
		lyr := lyr
		g.Go(func() error { return l.writeLayer(gctx, lyr) })
	}
	if err := g.Wait(); err != nil {
		return err
	}

	cfgData, err := img.RawConfigFile()
	if err != nil {
		return fmt.Errorf("getting config: %w", err)
	}
	if err := l.writeBlobData(ctx, cfgData); err != nil {
		return fmt.Errorf("writing config blob: %w", err)
	}

	manifestData, err := img.RawManifest()
	if err != nil {
		return fmt.Errorf("getting manifest: %w", err)
	}
	return l.writeBlobData(ctx, manifestData)
}

// writeImage writes all blobs for img and adds a descriptor entry to the OCI index with the
// given annotationRef and kind. containerdName overrides the io.containerd.image.name annotation;
// if empty it defaults to annotationRef.Name(). A non-empty subjectDigest records the base
// image's digest this artifact is a sig/att/sbom/referrer of; empty omits the annotation.
func (l *Layout) writeImage(ctx context.Context, annotationRef goname.Reference, img v1.Image, kind string, containerdName string, subjectDigest string) error {
	if err := l.writeImageBlobs(ctx, img); err != nil {
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
	// Provenance keeps the pre-normalization ref; only the containerd lookup
	// name gets the docker.io form.
	originalRef := containerdName
	containerdName = href.NormalizeContainerd(containerdName)
	desc := ocispec.Descriptor{
		MediaType: string(mt),
		Digest:    d,
		Size:      int64(len(raw)),
		Annotations: map[string]string{
			consts.KindAnnotationName:     kind,
			ocispec.AnnotationRefName:     strings.TrimPrefix(annotationRef.Name(), annotationRef.Context().RegistryStr()+"/"),
			consts.ContainerdImageNameKey: containerdName,
			// Captured once at the initial add so the original, pullable reference
			// survives even if a later --rewrite overwrites the annotations above.
			consts.OriginalRefAnnotation: originalRef,
		},
	}
	if subjectDigest != "" {
		desc.Annotations[consts.SubjectDigestAnnotation] = subjectDigest
	}
	return l.OCI.AddIndex(desc)
}

// collectSubjectDigests returns the index's own digest followed by every
// manifest digest reachable through it, nested indexes included, deduplicated.
// Buildx attestation-manifest children (unknown/unknown) are included on
// purpose: "every manifest in the index" is the contract, and excluding them
// would miss artifacts attached to them.
func collectSubjectDigests(idx v1.ImageIndex) ([]v1.Hash, error) {
	var out []v1.Hash
	seen := make(map[v1.Hash]bool)
	var walk func(v1.ImageIndex) error
	walk = func(ix v1.ImageIndex) error {
		d, err := ix.Digest()
		if err != nil {
			return fmt.Errorf("getting index digest: %w", err)
		}
		if seen[d] {
			return nil
		}
		seen[d] = true
		out = append(out, d)

		manifest, err := ix.IndexManifest()
		if err != nil {
			return fmt.Errorf("getting index manifest: %w", err)
		}
		for _, child := range manifest.Manifests {
			if childIdx, err := ix.ImageIndex(child.Digest); err == nil {
				if err := walk(childIdx); err != nil {
					return err
				}
				continue
			}
			if !seen[child.Digest] {
				seen[child.Digest] = true
				out = append(out, child.Digest)
			}
		}
		return nil
	}
	if err := walk(idx); err != nil {
		return nil, err
	}
	return out, nil
}

// writeIndexBlobs recursively writes all child image blobs for an image index to the store's blob
// directory. It does not write the top-level index manifest or add index entries.
func (l *Layout) writeIndexBlobs(ctx context.Context, idx v1.ImageIndex) error {
	manifest, err := idx.IndexManifest()
	if err != nil {
		return fmt.Errorf("getting index manifest: %w", err)
	}

	for _, childDesc := range manifest.Manifests {
		// Try as a nested index first, then fall back to a regular image.
		if childIdx, err := idx.ImageIndex(childDesc.Digest); err == nil {
			if err := l.writeIndexBlobs(ctx, childIdx); err != nil {
				return err
			}
			raw, err := childIdx.RawManifest()
			if err != nil {
				return fmt.Errorf("getting nested index manifest: %w", err)
			}
			if err := l.writeBlobData(ctx, raw); err != nil {
				return err
			}
		} else {
			childImg, err := idx.Image(childDesc.Digest)
			if err != nil {
				return fmt.Errorf("getting child image %v: %w", childDesc.Digest, err)
			}
			if err := l.writeImageBlobs(ctx, childImg); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeIndex writes all blobs for an image index (including all child platform images) and adds
// a descriptor entry to the OCI index with the given annotationRef and kind. subjectDigest follows
// the same rule as writeImage's -- OCI 1.1 permits subject on image indexes too, so a referrer can
// itself be an index.
func (l *Layout) writeIndex(ctx context.Context, annotationRef goname.Reference, idx v1.ImageIndex, kind string, subjectDigest string) error {
	if err := l.writeIndexBlobs(ctx, idx); err != nil {
		return err
	}

	raw, err := idx.RawManifest()
	if err != nil {
		return fmt.Errorf("getting index manifest: %w", err)
	}
	if err := l.writeBlobData(ctx, raw); err != nil {
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
			consts.ContainerdImageNameKey: href.NormalizeContainerd(annotationRef.Name()),
			// Captured once at the initial add so the original, pullable reference
			// survives even if a later --rewrite overwrites the annotations above.
			consts.OriginalRefAnnotation: annotationRef.Name(),
		},
	}
	if subjectDigest != "" {
		desc.Annotations[consts.SubjectDigestAnnotation] = subjectDigest
	}
	return l.OCI.AddIndex(desc)
}

// isNotFound reports whether err is a definitive "artifact absent" registry
// response: HTTP 404, or error code MANIFEST_UNKNOWN / NAME_UNKNOWN. Only
// these may be treated as "unsigned image" -- a 401/403/429/5xx or timeout is
// a failed retrieval, and swallowing it produces a silently incomplete store
// indistinguishable from an unsigned one.
func isNotFound(err error) bool {
	var terr *ggcrtransport.Error
	if !errors.As(err, &terr) {
		return false
	}
	if terr.StatusCode == http.StatusNotFound {
		return true
	}
	for _, diag := range terr.Errors {
		if diag.Code == ggcrtransport.ManifestUnknownErrorCode || diag.Code == ggcrtransport.NameUnknownErrorCode {
			return true
		}
	}
	return false
}

// RelatedArtifactError reports a failure retrieving a sig/att/sbom/referrer
// for an image that itself was already stored. Callers use errors.As to word
// their messages accurately -- the image is in the store; the run failed on
// its supply-chain artifacts.
type RelatedArtifactError struct {
	Ref     string // base image ref
	Kind    string // dev.hauler kind being retrieved
	Subject string // subject digest ("sha256:<hex>")
	Err     error
}

func (e *RelatedArtifactError) Error() string {
	return fmt.Sprintf("retrieving %s for %s (subject %s): %v", e.Kind, e.Ref, e.Subject, e.Err)
}

func (e *RelatedArtifactError) Unwrap() error { return e.Err }

// saveReferrers discovers and saves OCI 1.1 referrers for the image identified by ref/subject --
// cosign v3 new-bundle-format sigs/attestations stored via the subject field, as opposed to the
// legacy sha256-<hex>.sig/.att/.sbom tag convention.
func (l *Layout) saveReferrers(ctx context.Context, ref goname.Reference, subject v1.Hash, alreadySaved map[string]bool, opts ...remote.Option) error {
	log := zerolog.Ctx(ctx)

	imageDigestRef, err := goname.NewDigest(ref.Context().String() + "@" + subject.String())
	if err != nil {
		return fmt.Errorf("saveReferrers: constructing digest ref for %s: %w", ref.Name(), err)
	}

	// In ggcr v0.22.0, absent referrers never error: the API endpoint tolerates
	// 404/400/406 via the tag-schema fallback, and a missing fallback tag yields
	// empty.Index, nil. So any error here is a real failure; isNotFound is
	// defense in depth against a future ggcr version that surfaces 404 directly.
	idx, err := remote.Referrers(imageDigestRef, opts...)
	if err != nil {
		if isNotFound(err) {
			log.Debug().Err(err).Msgf("no OCI referrers found for %s@%s", ref.Name(), subject)
			return nil
		}
		return &RelatedArtifactError{Ref: ref.Name(), Kind: consts.KindAnnotationReferrers, Subject: subject.String(), Err: err}
	}

	idxManifest, err := idx.IndexManifest()
	if err != nil {
		if isNotFound(err) {
			log.Debug().Err(err).Msgf("saveReferrers: could not read referrers index for %s", ref.Name())
			return nil
		}
		return &RelatedArtifactError{Ref: ref.Name(), Kind: consts.KindAnnotationReferrers, Subject: subject.String(), Err: err}
	}

	for _, referrerDesc := range idxManifest.Manifests {
		digestRef, err := goname.NewDigest(ref.Context().String() + "@" + referrerDesc.Digest.String())
		if err != nil {
			log.Debug().Err(err).Msgf("saveReferrers: could not construct digest ref for referrer %s", referrerDesc.Digest)
			continue
		}

		// Skip referrers already saved via the cosign tag convention to avoid duplicates.
		// Registries like Harbor expose the same manifest via both the .sig/.att/.sbom tags
		// and the OCI Referrers API when the manifest carries a subject field.
		if alreadySaved[referrerDesc.Digest.String()] {
			log.Debug().Msgf("saveReferrers: skipping referrer %s (already saved via tag convention)", referrerDesc.Digest)
			continue
		}

		// Embed the referrer manifest digest in the kind annotation so that multiple
		// referrers for the same base image each get a unique entry in the OCI index.
		//
		// OCI 1.1 permits subject on image indexes too, and Descriptor.Image() on
		// an index digest doesn't fail cleanly: ggcr v0.22.0 silently resolves it
		// to whichever child matches the default platform when one exists,
		// mis-storing the referrer as an unrelated child manifest, and only
		// errors when no child matches. Branch on the descriptor's declared type
		// instead of trusting Image() to fail on an index.
		kind := consts.KindAnnotationReferrers + "/" + referrerDesc.Digest.Hex
		switch referrerDesc.MediaType {
		case types.OCIImageIndex, types.DockerManifestList:
			refIdx, err := remote.Index(digestRef, opts...)
			if err != nil {
				if isNotFound(err) {
					log.Debug().Err(err).Msgf("saveReferrers: referrer index %s vanished", referrerDesc.Digest)
					continue
				}
				return &RelatedArtifactError{Ref: ref.Name(), Kind: kind, Subject: subject.String(), Err: err}
			}
			if err := l.writeIndex(ctx, ref, refIdx, kind, subject.String()); err != nil {
				return fmt.Errorf("saving OCI referrer index %s for %s: %w", referrerDesc.Digest, ref.Name(), err)
			}
		case types.OCIManifestSchema1, types.DockerManifestSchema2:
			img, err := remote.Image(digestRef, opts...)
			if err != nil {
				if isNotFound(err) {
					log.Debug().Err(err).Msgf("saveReferrers: referrer manifest %s vanished", referrerDesc.Digest)
					continue
				}
				return &RelatedArtifactError{Ref: ref.Name(), Kind: kind, Subject: subject.String(), Err: err}
			}
			if err := l.writeImage(ctx, ref, img, kind, "", subject.String()); err != nil {
				return fmt.Errorf("saving OCI referrer %s for %s: %w", referrerDesc.Digest, ref.Name(), err)
			}
		default:
			log.Warn().Msgf("skipping referrer %s for %s: unsupported media type %q", referrerDesc.Digest, ref.Name(), referrerDesc.MediaType)
			continue
		}
		log.Debug().Msgf("saved OCI referrer %s (%s) for %s", referrerDesc.Digest, string(referrerDesc.ArtifactType), ref.Name())
	}
	return nil
}

// saveRelatedArtifacts probes the cosign tag convention for subject and stores
// whatever exists. topLevel selects the kind shape: plain for the stored
// artifact's own digest, subject-suffixed for a child manifest of a multi-arch
// index -- two artifacts with the same ref and plain kind would collide in
// nameMapKey's <ref>-<kind> and the second AddIndex would silently replace the
// first. saved is shared across all subjects of one AddImage call so a
// manifest reachable via both the tag convention and the Referrers API is
// stored once.
func (l *Layout) saveRelatedArtifacts(ctx context.Context, ref goname.Reference, subject v1.Hash, topLevel bool, saved map[string]bool, opts ...remote.Option) error {
	tagPrefix := strings.ReplaceAll(subject.String(), ":", "-")

	related := []struct {
		tag  string
		kind string
	}{
		{tagPrefix + ".sig", consts.KindAnnotationSigs},
		{tagPrefix + ".att", consts.KindAnnotationAtts},
		{tagPrefix + ".sbom", consts.KindAnnotationSboms},
	}

	for _, r := range related {
		artifactRef, err := href.ParseReference(ref.Context().String() + ":" + r.tag)
		if err != nil {
			continue
		}
		img, err := remote.Image(artifactRef, opts...)
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return &RelatedArtifactError{Ref: ref.Name(), Kind: r.kind, Subject: subject.String(), Err: err}
		}
		kind := r.kind
		if !topLevel {
			kind = r.kind + "/" + subject.Hex
		}
		if err := l.writeImage(ctx, ref, img, kind, "", subject.String()); err != nil {
			return fmt.Errorf("saving %s for %s: %w", kind, ref.Name(), err)
		}
		if d, err := img.Digest(); err == nil {
			saved[d.String()] = true
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

		// Append the digest so the target pusher can identify the root descriptor.
		// AnnotationRefName for digest-only images already ends in "@sha256:...", so
		// strip any existing digest first -- a double "@" leaves the image unindexed (#642).
		if desc.Digest.Validate() == nil {
			if at := strings.Index(toRef, "@"); at != -1 {
				toRef = toRef[:at]
			}
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

func (l *Layout) writeBlobData(ctx context.Context, data []byte) error {
	blob := static.NewLayer(data, "") // NOTE: MediaType isn't actually used in the writing
	return l.writeLayer(ctx, blob)
}

// writeLayer writes a single layer's content to the store's blob directory.
// Writes are atomic (temp file + rename), digest-verified, and deduplicated
// across concurrent callers writing the same digest -- see
// content.OCI.WriteBlob for the implementation.
func (l *Layout) writeLayer(ctx context.Context, layer v1.Layer) error {
	d, err := layer.Digest()
	if err != nil {
		return err
	}
	expected, err := digest.Parse(d.String())
	if err != nil {
		return err
	}
	size, err := layer.Size()
	if err != nil {
		return err
	}

	return l.OCI.WriteBlob(ctx, expected, size, func() (io.ReadCloser, error) {
		return layer.Compressed()
	})
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
	blobsPath := filepath.Join(l.Root, ocispec.ImageBlobsDir, digest.Canonical.String())
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
