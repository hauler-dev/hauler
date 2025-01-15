package store

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"slices"

	referencev3 "github.com/distribution/distribution/v3/reference"
	"github.com/google/go-containerregistry/pkg/name"
	libv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	imagev1 "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/archives"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/log"
)

// SaveCmd
// TODO: Just use mholt/archiver for now, even though we don't need most of it
func SaveCmd(ctx context.Context, o *flags.SaveOpts, outputFile string) error {
	l := log.FromContext(ctx)

	storeDir := o.StoreDir

	if storeDir == "" {
		storeDir = os.Getenv(consts.HaulerStoreDir)
	}

	if storeDir == "" {
		storeDir = consts.DefaultStoreName
	}

	// Maps to handle compression and archival types
	compressionMap := archives.CompressionMap
	archivalMap := archives.ArchivalMap

	// TODO: Support more formats?
	// Select the correct compression and archival type based on user input
	compression := compressionMap["zst"]
	archival := archivalMap["tar"]

	absOutputfile, err := filepath.Abs(outputFile)
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(storeDir); err != nil {
		return err
	}

	// create the manifest.json file
	if err := writeExportsManifest(ctx, ".", o.Platform); err != nil {
		return err
	}

	// create the archive
	err = archives.Archive(ctx, ".", absOutputfile, compression, archival)
	if err != nil {
		return err
	}

	l.Infof("saved store [%s] -> [%s]", storeDir, absOutputfile)
	return nil
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
		l.Debugf("descriptor [%s] >>> %s", desc.Digest.String(), desc.MediaType)
		if artifactType := types.MediaType(desc.ArtifactType); artifactType != "" && !artifactType.IsImage() && !artifactType.IsIndex() {
			l.Debugf("descriptor [%s] <<< SKIPPING ARTIFACT [%q]", desc.Digest.String(), desc.ArtifactType)
			continue
		}
		if desc.Annotations != nil {
			// we only care about images that cosign has added to the layout index
			if kind, hasKind := desc.Annotations[consts.KindAnnotationName]; hasKind {
				if refName, hasRefName := desc.Annotations["io.containerd.image.name"]; hasRefName {
					// branch on image (aka image manifest) or image index
					switch kind {
					case consts.KindAnnotationImage:
						if err := x.record(ctx, idx, desc, refName); err != nil {
							return err
						}
					case consts.KindAnnotationIndex:
						l.Debugf("index [%s]: digest=%s, type=%s, size=%d", refName, desc.Digest.String(), desc.MediaType, desc.Size)

						// when no platform is provided, warn the user of potential mismatch on import
						if platform.String() == "" {
							l.Warnf("index [%s]: provide an export platform to prevent potential platform mismatch on import", refName)
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
								// check if platform is provided, if so, skip anything that doesn't match
								if platform.String() != "" {
									if ixd.Platform.Architecture != platform.Architecture || ixd.Platform.OS != platform.OS {
										l.Warnf("index [%s]: digest=%s, platform=%s/%s: does not match the supplied platform, skipping", refName, desc.Digest.String(), ixd.Platform.OS, ixd.Platform.Architecture)
										continue
									}
								}

								// skip 'unknown' platforms... docker hates
								if ixd.Platform.Architecture == "unknown" && ixd.Platform.OS == "unknown" {
									l.Warnf("index [%s]: digest=%s, platform=%s/%s: skipping 'unknown/unknown' platform", refName, desc.Digest.String(), ixd.Platform.OS, ixd.Platform.Architecture)
									continue
								}

								if err := x.record(ctx, iix, ixd, refName); err != nil {
									return err
								}
							}
						}
					default:
						l.Debugf("descriptor [%s] <<< SKIPPING KIND [%q]", desc.Digest.String(), kind)
					}
				}
			}
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

	config, err := image.ConfigName()
	if err != nil {
		return err
	}

	xd, recorded := x.records[digest]
	if !recorded {
		// record one export record per digest
		x.digests = append(x.digests, digest)
		xd = tarball.Descriptor{
			Config:   path.Join(imagev1.ImageBlobsDir, config.Algorithm, config.Hex),
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
			xd.Layers = append(xd.Layers[:], path.Join(imagev1.ImageBlobsDir, xl.Algorithm, xl.Hex))
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
