package store

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"slices"

	referencev3 "github.com/distribution/distribution/v3/reference"
	"github.com/google/go-containerregistry/pkg/name"
	libv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/log"
)

// Exports tracks image digests and their tarball descriptors for
// writing an exports manifest (manifest.json) to an OCI layout.
type Exports struct {
	digests []string
	records map[string]tarball.Descriptor
}

// Digests returns the ordered list of image digests.
func (e *Exports) Digests() []string {
	if e == nil {
		return nil
	}
	return e.digests
}

// Records returns a copy of the tarball descriptor map.
// Returns nil if no records exist.
func (e *Exports) Records() map[string]tarball.Descriptor {
	if e == nil || len(e.records) == 0 {
		return nil
	}
	copy := make(map[string]tarball.Descriptor, len(e.records))
	for k, v := range e.records {
		copy[k] = v
	}
	return copy
}

// describe returns the tarball.Manifest for encoding to manifest.json.
func (x *Exports) describe() tarball.Manifest {
	m := make(tarball.Manifest, len(x.digests))
	for i, d := range x.digests {
		m[i] = x.records[d]
	}
	return m
}

// record adds an image descriptor to the exports tracking structure.
func (x *Exports) record(ctx context.Context, index libv1.ImageIndex, desc libv1.Descriptor, refname string) error {
	l := log.FromContext(ctx)

	digest := desc.Digest.String()
	image, err := index.Image(desc.Digest)
	if err != nil {
		return err
	}

	// Verify this is a real container image by inspecting its manifest config media type.
	// Non-image OCI artifacts (Helm charts, files, cosign sigs) use distinct config types.
	manifest, err := image.Manifest()
	if err != nil {
		return err
	}
	if manifest.Config.MediaType != types.DockerConfigJSON && manifest.Config.MediaType != types.OCIConfigJSON {
		l.Debugf("descriptor [%s] <<< SKIPPING NON-IMAGE config media type [%q]", desc.Digest.String(), manifest.Config.MediaType)
		return nil
	}

	config, err := image.ConfigName()
	if err != nil {
		return err
	}

	xd, recorded := x.records[digest]
	if !recorded {
		// record one export record per digest
		x.digests = append(x.digests, digest)
		xd = tarball.Descriptor{
			Config:   path.Join(ocispec.ImageBlobsDir, config.Algorithm, config.Hex),
			RepoTags: []string{},
			Layers:   []string{},
		}

		layers, err := image.Layers()
		if err != nil {
			return err
		}
		for _, layer := range layers {
			xl, err := layer.Digest()
			if err != nil {
				return err
			}
			xd.Layers = append(xd.Layers[:], path.Join(ocispec.ImageBlobsDir, xl.Algorithm, xl.Hex))
		}
	}

	ref, err := name.ParseReference(refname)
	if err != nil {
		return err
	}

	// record tags for the digest, eliminating dupes
	switch tag := ref.(type) {
	case name.Tag:
		named, err := referencev3.ParseNormalizedNamed(refname)
		if err != nil {
			return err
		}
		named = referencev3.TagNameOnly(named)
		repotag := referencev3.FamiliarString(named)
		xd.RepoTags = append(xd.RepoTags[:], repotag)
		slices.Sort(xd.RepoTags)
		xd.RepoTags = slices.Compact(xd.RepoTags)
		ref = tag.Digest(digest)
	}

	l.Debugf("image [%s]: type=%s, size=%d", ref.Name(), desc.MediaType, desc.Size)
	// record export descriptor for the digest
	x.records[digest] = xd

	return nil
}

// WriteExportsManifest writes a manifest.json (Docker-style tarball
// manifest) to the OCI layout at dir. It walks the index, filters by
// kind annotation, and records layers and repo tags for each image.
// When platform is non-empty, only images matching that platform are
// included.
func WriteExportsManifest(ctx context.Context, dir string, platformStr string) error {
	l := log.FromContext(ctx)

	// validate platform format
	platform, err := libv1.ParsePlatform(platformStr)
	if err != nil {
		return err
	}

	oci, err := layout.FromPath(dir)
	if err != nil {
		return err
	}

	idx, err := oci.ImageIndex()
	if err != nil {
		return err
	}

	imx, err := idx.IndexManifest()
	if err != nil {
		return err
	}

	x := &Exports{
		digests: []string{},
		records: map[string]tarball.Descriptor{},
	}

	for _, desc := range imx.Manifests {
		l.Debugf("descriptor [%s] = [%s]", desc.Digest.String(), desc.MediaType)
		if artifactType := types.MediaType(desc.ArtifactType); artifactType != "" && !artifactType.IsImage() && !artifactType.IsIndex() {
			l.Debugf("descriptor [%s] <<< SKIPPING ARTIFACT [%q]", desc.Digest.String(), desc.ArtifactType)
			continue
		}
		// The kind annotation is the only reliable way to distinguish container images from
		// cosign signatures/attestations/SBOMs: those are stored as standard Docker/OCI
		// manifests (same media type as real images) so media type alone is insufficient.
		kind := desc.Annotations[consts.KindAnnotationName]
		if kind != consts.KindAnnotationImage && kind != consts.KindAnnotationIndex {
			l.Debugf("descriptor [%s] <<< SKIPPING KIND [%q]", desc.Digest.String(), kind)
			continue
		}

		refName, hasRefName := desc.Annotations[consts.ContainerdImageNameKey]
		if !hasRefName {
			l.Debugf("descriptor [%s] <<< SKIPPING (no containerd image name)", desc.Digest.String())
			continue
		}

		// Use the descriptor's actual media type to discriminate single-image manifests
		// from multi-arch indexes, rather than relying on the kind string for this.
		switch {
		case desc.MediaType.IsImage():
			if err := x.record(ctx, idx, desc, refName); err != nil {
				return err
			}
		case desc.MediaType.IsIndex():
			l.Debugf("index [%s]: digest=[%s]... type=[%s]... size=[%d]", refName, desc.Digest.String(), desc.MediaType, desc.Size)

			// when no platform is inputted... warn the user of potential mismatch on import for docker
			// required for docker to be able to interpret and load the image correctly
			if platform.String() == "" {
				l.Warnf("compatibility warning... docker... specify platform to prevent potential mismatch on import of index [%s]", refName)
			}

			iix, err := idx.ImageIndex(desc.Digest)
			if err != nil {
				return err
			}
			ixm, err := iix.IndexManifest()
			if err != nil {
				return err
			}
			for _, ixd := range ixm.Manifests {
				if ixd.MediaType.IsImage() {
					if platform.String() != "" {
						if ixd.Platform.Architecture != platform.Architecture || ixd.Platform.OS != platform.OS {
							l.Debugf("index [%s]: digest=[%s], platform=[%s/%s]: does not match the supplied platform... skipping...", refName, desc.Digest.String(), ixd.Platform.OS, ixd.Platform.Architecture)
							continue
						}
					}

					// skip any platforms of 'unknown/unknown'... docker hates
					// required for docker to be able to interpret and load the image correctly
					if ixd.Platform.Architecture == "unknown" && ixd.Platform.OS == "unknown" {
						l.Debugf("index [%s]: digest=[%s], platform=[%s/%s]: matches unknown platform... skipping...", refName, desc.Digest.String(), ixd.Platform.OS, ixd.Platform.Architecture)
						continue
					}

					if err := x.record(ctx, iix, ixd, refName); err != nil {
						return err
					}
				}
			}
		default:
			l.Debugf("descriptor [%s] <<< SKIPPING media type [%q]", desc.Digest.String(), desc.MediaType)
		}
	}

	buf := bytes.Buffer{}
	mnf := x.describe()
	err = json.NewEncoder(&buf).Encode(mnf)
	if err != nil {
		return err
	}

	return oci.WriteFile(consts.ImageManifestFile, buf.Bytes(), 0666)
}
