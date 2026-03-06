package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	referencev3 "github.com/distribution/distribution/v3/reference"
	"github.com/google/go-containerregistry/pkg/name"
	libv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/archives"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/log"
)

// saves a content store to store archives
func SaveCmd(ctx context.Context, o *flags.SaveOpts, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	// maps to handle compression and archival types
	compressionMap := archives.CompressionMap
	archivalMap := archives.ArchivalMap

	// select the compression and archival type based parsed filename extension
	compression := compressionMap["zst"]
	archival := archivalMap["tar"]

	absOutputfile, err := filepath.Abs(o.FileName)
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(o.StoreDir); err != nil {
		return err
	}

	// create the manifest.json file
	if err := writeExportsManifest(ctx, ".", o.Platform); err != nil {
		return err
	}

	// strip out the oci-layout file from the haul
	// required for containerd to be able to interpret the haul correctly for all mediatypes and artifactypes
	if o.ContainerdCompatibility {
		if err := os.Remove(filepath.Join(".", ocispec.ImageLayoutFile)); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
		} else {
			l.Warnf("compatibility warning... containerd... removing 'oci-layout' file to support containerd importing of images")
		}
	}

	// create the archive
	err = archives.Archive(ctx, ".", absOutputfile, compression, archival)
	if err != nil {
		return err
	}

	if o.ChunkSize != "" {
		maxBytes, err := parseChunkSize(o.ChunkSize)
		if err != nil {
			return err
		}
		chunks, err := archives.SplitArchive(ctx, absOutputfile, maxBytes)
		if err != nil {
			return err
		}
		for _, c := range chunks {
			l.Infof("saving store [%s] to chunk [%s]", o.StoreDir, filepath.Base(c))
		}
	} else {
		l.Infof("saving store [%s] to archive [%s]", o.StoreDir, o.FileName)
	}

	return nil
}

// parseChunkSize parses a human-readable byte size string (e.g. "1G", "500M", "2GB")
// into a byte count. Suffixes are treated as binary units (1K = 1024).
func parseChunkSize(s string) (int64, error) {
	units := map[string]int64{
		"K": 1 << 10, "KB": 1 << 10,
		"M": 1 << 20, "MB": 1 << 20,
		"G": 1 << 30, "GB": 1 << 30,
		"T": 1 << 40, "TB": 1 << 40,
	}
	s = strings.ToUpper(strings.TrimSpace(s))
	for suffix, mult := range units {
		if strings.HasSuffix(s, suffix) {
			n, err := strconv.ParseInt(strings.TrimSuffix(s, suffix), 10, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid chunk size %q", s)
			}
			return n * mult, nil
		}
	}
	return strconv.ParseInt(s, 10, 64)
}

type exports struct {
	digests []string
	records map[string]tarball.Descriptor
}

func writeExportsManifest(ctx context.Context, dir string, platformStr string) error {
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

	x := &exports{
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

func (x *exports) describe() tarball.Manifest {
	m := make(tarball.Manifest, len(x.digests))
	for i, d := range x.digests {
		m[i] = x.records[d]
	}
	return m
}

func (x *exports) record(ctx context.Context, index libv1.ImageIndex, desc libv1.Descriptor, refname string) error {
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
