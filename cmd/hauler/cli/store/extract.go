package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	goname "github.com/google/go-containerregistry/pkg/name"
	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/internal/mapper"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/reference"
	"hauler.dev/go/hauler/v2/pkg/store"
)

// isIndexMediaType returns true for OCI and Docker manifest list media types.
func isIndexMediaType(mt string) bool {
	return mt == ocispec.MediaTypeImageIndex || mt == consts.DockerManifestListSchema2
}

// firstLeafManifest walks a (potentially nested) OCI index and returns the
// decoded manifest of the first non-index child. It prefers non-index children
// at each level... if all children are indexes it descends into the first one.
// Returns an error if any nested index or manifest cannot be decoded.
func firstLeafManifest(ctx context.Context, s *store.Layout, idx ocispec.Index) (ocispec.Manifest, error) {
	for {
		if len(idx.Manifests) == 0 {
			return ocispec.Manifest{}, fmt.Errorf("image index has no child manifests")
		}

		// Prefer the first non-index child... fall back to the first child (an index) if all are indexes.
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

		// Cosign sig/att/sbom/referrer descriptors are registry-only metadata —
		// they are never extractable to disk. Skip them silently at debug level,
		// mirroring the same guard in copy.go (directory-target path).
		kind := desc.Annotations[consts.KindAnnotationName]
		if _, ok := consts.SigKindExt(kind); ok {
			l.Debugf("skipping cosign artifact [%s] for extract", reference)
			return nil
		}
		if strings.HasPrefix(kind, consts.KindAnnotationReferrers) {
			l.Debugf("skipping OCI referrer [%s] for extract", reference)
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

		// Container images are written as a docker load compatible tar with one platform
		if isContainerImageManifest(m) {
			return extractImageTar(ctx, o, s, desc.Annotations[ocispec.AnnotationRefName], desc)
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

// extractImageTar writes one platform of a container image in the store to a docker load compatible tar
func extractImageTar(ctx context.Context, o *flags.ExtractOpts, s *store.Layout, ref string, desc ocispec.Descriptor) error {
	l := log.FromContext(ctx)

	p, err := layout.FromPath(s.Root)
	if err != nil {
		return fmt.Errorf("failed to open store [%s]: %w", s.Root, err)
	}
	root, err := p.ImageIndex()
	if err != nil {
		return fmt.Errorf("failed to read store index: %w", err)
	}
	h, err := gv1.NewHash(desc.Digest.String())
	if err != nil {
		return fmt.Errorf("failed to parse digest [%s]: %w", desc.Digest, err)
	}

	var img gv1.Image
	if isIndexMediaType(desc.MediaType) {
		platformStr := o.Platform
		if platformStr == "" {
			platformStr = "linux/" + runtime.GOARCH
			l.Infof("no --platform for multi-platform image [%s]... defaulting to [%s]", ref, platformStr)
		}
		platform, err := parseImagePlatform(platformStr)
		if err != nil {
			return err
		}
		idx, err := root.ImageIndex(h)
		if err != nil {
			return fmt.Errorf("failed to read image index [%s]: %w", ref, err)
		}
		var available []string
		img, err = selectPlatformImage(idx, *platform, &available)
		if err != nil {
			return fmt.Errorf("failed to select platform for [%s]: %w", ref, err)
		}
		if img == nil {
			return fmt.Errorf("image [%s] has no platform matching [%s]... available platform(s) [%s]", ref, platformStr, strings.Join(available, ", "))
		}
	} else {
		img, err = root.Image(h)
		if err != nil {
			return fmt.Errorf("failed to read image [%s]: %w", ref, err)
		}
		if o.Platform != "" {
			if err := checkImagePlatform(img, o.Platform); err != nil {
				return fmt.Errorf("image [%s]: %w", ref, err)
			}
		}
	}

	tagRef, err := imageTarRef(ref, desc)
	if err != nil {
		return fmt.Errorf("failed to parse image name [%s]: %w", ref, err)
	}

	if o.DestinationDir != "" {
		if err := os.MkdirAll(o.DestinationDir, 0o755); err != nil {
			return fmt.Errorf("failed to create output directory [%s]: %w", o.DestinationDir, err)
		}
	}
	out := filepath.Join(o.DestinationDir, imageTarName(ref))
	if err := tarball.WriteToFile(out, tagRef, img); err != nil {
		return fmt.Errorf("failed to write image tar [%s]: %w", out, err)
	}

	l.Infof("extracted image [%s] to [%s]", ref, out)
	return nil
}

// selectPlatformImage returns the first image in idx (or a nested index) matching want, or nil when none match
func selectPlatformImage(idx gv1.ImageIndex, want gv1.Platform, available *[]string) (gv1.Image, error) {
	im, err := idx.IndexManifest()
	if err != nil {
		return nil, err
	}
	for _, d := range im.Manifests {
		switch {
		case d.MediaType.IsIndex():
			nested, err := idx.ImageIndex(d.Digest)
			if err != nil {
				return nil, err
			}
			img, err := selectPlatformImage(nested, want, available)
			if err != nil || img != nil {
				return img, err
			}
		case d.MediaType.IsImage() && d.Platform != nil:
			// skip attestation manifests, which docker can't load
			if d.Platform.OS == "unknown" && d.Platform.Architecture == "unknown" {
				continue
			}
			*available = append(*available, d.Platform.String())
			if d.Platform.Satisfies(want) {
				return idx.Image(d.Digest)
			}
		}
	}
	return nil, nil
}

// checkImagePlatform fails when a single-platform image doesn't match the requested platform
func checkImagePlatform(img gv1.Image, platformStr string) error {
	want, err := parseImagePlatform(platformStr)
	if err != nil {
		return err
	}
	cf, err := img.ConfigFile()
	if err != nil {
		return fmt.Errorf("failed to read image config: %w", err)
	}
	if got := cf.Platform(); got != nil && !got.Satisfies(*want) {
		return fmt.Errorf("platform [%s] does not match --platform [%s]", got, platformStr)
	}
	return nil
}

// parseImagePlatform requires os/arch[/variant] like the rest of hauler, since an empty os or arch would match any image
func parseImagePlatform(platformStr string) (*gv1.Platform, error) {
	p, err := gv1.ParsePlatform(platformStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse platform [%s]: %w", platformStr, err)
	}
	if p.OS == "" || p.Architecture == "" {
		return nil, fmt.Errorf("invalid --platform [%s]... expected os/arch[/variant] (i.e. linux/amd64)", platformStr)
	}
	return p, nil
}

// imageTarRef returns the name docker load tags the image with, preferring the registry qualified containerd name
func imageTarRef(ref string, desc ocispec.Descriptor) (goname.Reference, error) {
	name := ref
	if n := desc.Annotations[consts.ContainerdImageNameKey]; n != "" {
		name = reference.NormalizeContainerd(n)
	}
	r, err := goname.ParseReference(name)
	if err != nil {
		return nil, err
	}
	if d, ok := r.(goname.Digest); ok {
		if t, ok := digestRefTag(d); ok {
			return t, nil
		}
	}
	return r, nil
}

// imageTarName builds the tar filename from a store ref (i.e. hauler-dev/library/busybox:stable becomes busybox_stable.tar)
func imageTarName(ref string) string {
	repo, ident := ref, ""
	if at := strings.Index(repo, "@"); at != -1 {
		repo, ident = repo[:at], strings.ReplaceAll(repo[at+1:], ":", "-")
	}
	if c := strings.LastIndex(repo, ":"); c != -1 {
		repo, ident = repo[:c], repo[c+1:]
	}
	return path.Base(repo) + "_" + ident + ".tar"
}
