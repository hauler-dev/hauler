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

	referencev3 "github.com/distribution/reference"
	goname "github.com/google/go-containerregistry/pkg/name"
	libv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/archives"
	"hauler.dev/go/hauler/v2/pkg/audit"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/reference"
	"hauler.dev/go/hauler/v2/pkg/store"
)

// saves a content store to store archives
func SaveCmd(ctx context.Context, o *flags.SaveOpts, s *store.Layout, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
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

	// Generated files go to a scratch dir and are mapped into the archive root,
	// so save never mutates the live store (the old chdir flow wrote manifest.json
	// into it and --containerd deleted its oci-layout permanently). The one
	// deliberate exception is audit.log: at AuditLevel "standard" or "verbose"
	// the audit.Append call below writes a portable entry to <StoreDir>/audit.log
	// by design (#727) -- see TestSaveCmd_DoesNotMutateStore's audit subtest.
	scratch, err := os.MkdirTemp(rso.TempOverride, consts.DefaultHaulerTempDirName)
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)

	if err := writeExportsManifest(ctx, o.StoreDir, scratch, o.Platform); err != nil {
		return err
	}

	files := map[string]string{
		filepath.Join(scratch, consts.ImageManifestFile): consts.ImageManifestFile,
	}

	// The live store never writes an oci-layout marker on disk (nothing in
	// pkg/content/oci.go creates one), but containerd's OCI import path and
	// the documented archive layout both require it. Generate it into
	// scratch like the other derived files -- only when the store lacks one,
	// so "oci-layout" is never mapped from two different source paths into
	// the same archive entry.
	layoutMarkerPath := filepath.Join(o.StoreDir, ocispec.ImageLayoutFile)
	if _, err := os.Stat(layoutMarkerPath); os.IsNotExist(err) {
		marker, err := json.Marshal(ocispec.ImageLayout{Version: ocispec.ImageLayoutVersion})
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(scratch, ocispec.ImageLayoutFile), marker, 0644); err != nil {
			return err
		}
		files[filepath.Join(scratch, ocispec.ImageLayoutFile)] = ocispec.ImageLayoutFile
	} else if err != nil {
		return err
	}

	if o.ContainerdCompatibility {
		dropped, err := writeContainerdIndexes(o.StoreDir, scratch)
		if err != nil {
			return err
		}
		files[filepath.Join(scratch, ocispec.ImageIndexFile)] = ocispec.ImageIndexFile
		files[filepath.Join(scratch, consts.HaulerIndexFile)] = consts.HaulerIndexFile
		if dropped > 0 {
			l.Warnf("compatibility warning... containerd... excluded [%d] non-image artifacts from index.json (full index preserved as [%s]... requires a hauler version with sidecar support to load)", dropped, consts.HaulerIndexFile)
		}
	} else {
		// Default mode drops nothing, but still normalizes io.containerd.image.name
		// so a default haul of a legacy store (or one that went through --rewrite)
		// carries CRI-resolvable names once oci-layout steers containerd onto the
		// OCI import path (#744 amended §3). No sidecar: nothing was filtered.
		if err := writeDefaultIndex(o.StoreDir, scratch); err != nil {
			return err
		}
		files[filepath.Join(scratch, ocispec.ImageIndexFile)] = ocispec.ImageIndexFile
	}

	entries, err := os.ReadDir(o.StoreDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(o.StoreDir, name)
		if name == consts.ImageManifestFile {
			continue // always regenerated into scratch
		}
		if name == ocispec.ImageIndexFile {
			// Always replaced by the scratch copy -- filtered+normalized in
			// --containerd mode, normalized-only in default mode (see
			// writeContainerdIndexes / writeDefaultIndex).
			continue
		}
		if name == consts.HaulerIndexFile {
			// Skip unconditionally, not just in --containerd mode: a stray sidecar
			// already in the store dir would otherwise ride into a default haul,
			// and store load prefers the sidecar over index.json -- silent data
			// loss (#744 fix 3).
			continue
		}
		if full == absOutputfile {
			// Saving the archive into the store dir: ArchiveFiles removes
			// absOutputfile before reading fileMap, so mapping it here would
			// point at a path FilesFromDisk can no longer find.
			continue
		}
		files[full] = name
	}

	// create the archive
	if err := archives.ArchiveFiles(ctx, files, absOutputfile, compression, archival); err != nil {
		return err
	}

	if o.ChunkSize != "" {
		if o.ContainerdCompatibility == true {
			l.Warnf("compatibility warning... stores split by chunk size must be imported using `hauler store load` to rejoin before import to containerd")
		}
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

	if auditLevel(ro) != "none" {
		e := audit.Entry{
			StoreID:   s.StoreID,
			Store:     s.Root,
			Command:   "store save",
			Reference: o.FileName,
		}
		if auditLevel(ro) == "verbose" {
			sys := audit.BuildSystem()
			g := audit.BuildGlobal(ro, rso)
			e.System = &sys
			e.Global = &g
			e.Flags = map[string]any{
				"platform":   o.Platform,
				"containerd": o.ContainerdCompatibility,
				"chunk-size": o.ChunkSize,
			}
		}
		if err := audit.Append(ro.HaulerDir, e); err != nil {
			l.Warnf("failed to write audit entry: %v", err)
		}
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
	var result int64
	matched := false
	for suffix, mult := range units {
		if strings.HasSuffix(s, suffix) {
			n, err := strconv.ParseInt(strings.TrimSuffix(s, suffix), 10, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid chunk size %q: %w", s, err)
			}
			result = n * mult
			matched = true
			break
		}
	}
	if !matched {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid chunk size %q: %w", s, err)
		}
		result = n
	}
	if result <= 0 {
		return 0, fmt.Errorf("chunk size must be greater than zero, received %q", s)
	}
	return result, nil
}

type exports struct {
	digests []string
	records map[string]tarball.Descriptor
}

func writeExportsManifest(ctx context.Context, dir string, outDir string, platformStr string) error {
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
			if err := x.record(ctx, idx, desc, refName, "", platform.String() != ""); err != nil {
				return err
			}
		case desc.MediaType.IsIndex():
			l.Debugf("index [%s]: digest=[%s]... type=[%s]... size=[%d]", refName, desc.Digest.String(), desc.MediaType, desc.Size)

			// when no platform is inputted... docker load keeps only one architecture per tag (last wins)
			// containerd and OCI imports include all architectures and are unaffected
			if platform.String() == "" {
				l.Warnf("compatibility warning... docker... multi-arch index [%s] saved without --platform", refName)
				l.Warnf("'docker load' keeps only one architecture per tag (last wins)... use --platform (i.e. linux/amd64) to select one... containerd and OCI imports include all architectures and are unaffected")
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

					if err := x.record(ctx, iix, ixd, refName, desc.Digest.String(), platform.String() != ""); err != nil {
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

	return os.WriteFile(filepath.Join(outDir, consts.ImageManifestFile), buf.Bytes(), 0666)
}

// writeContainerdIndexes writes the two indexes a --containerd haul carries into
// outDir: index.json filtered to image/imageIndex kinds with containerd names
// normalized (containerd's OCI import path reads it -- non-image artifacts would
// break the import, and un-normalized names miss CRI lookups, #744), and a
// byte-identical full copy as consts.HaulerIndexFile so store load loses nothing.
// The store's own files are never modified. Returns how many descriptors were
// excluded from the filtered index.
//
// Kept iff kind is image/imageIndex AND ContainerdImageNameKey is present --
// mirrors writeExportsManifest's predicate. The kind check alone isn't enough:
// store.Layout.AddArtifact tags every artifact it writes, charts and files
// included, with KindAnnotationImage and never sets ContainerdImageNameKey, so
// only real images carry that annotation; the name check is what actually
// separates them out.
func writeContainerdIndexes(storeDir, outDir string) (int, error) {
	data, err := os.ReadFile(filepath.Join(storeDir, ocispec.ImageIndexFile))
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(filepath.Join(outDir, consts.HaulerIndexFile), data, 0666); err != nil {
		return 0, err
	}

	var idx ocispec.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return 0, err
	}
	kept := idx.Manifests[:0:0]
	for _, d := range idx.Manifests {
		kind := d.Annotations[consts.KindAnnotationName]
		if kind != consts.KindAnnotationImage && kind != consts.KindAnnotationIndex {
			continue
		}
		if _, hasName := d.Annotations[consts.ContainerdImageNameKey]; !hasName {
			continue
		}
		normalizeContainerdName(d.Annotations)
		kept = append(kept, d)
	}
	dropped := len(idx.Manifests) - len(kept)
	idx.Manifests = kept

	out, err := json.Marshal(idx)
	if err != nil {
		return 0, err
	}
	return dropped, os.WriteFile(filepath.Join(outDir, ocispec.ImageIndexFile), out, 0666)
}

// normalizeContainerdName rewrites annotations' io.containerd.image.name through
// reference.NormalizeContainerd in place, when present -- the v1-parity fix
// (#744 §3) shared by both writeContainerdIndexes' filtered rewrite and
// writeDefaultIndex's unfiltered one, so archives from stores populated by
// older v2 versions (or run through --rewrite) still carry CRI-resolvable names.
func normalizeContainerdName(annotations map[string]string) {
	if name, ok := annotations[consts.ContainerdImageNameKey]; ok {
		annotations[consts.ContainerdImageNameKey] = reference.NormalizeContainerd(name)
	}
}

// writeDefaultIndex writes a normalized copy of storeDir's index.json into outDir
// for default-mode (non-`--containerd`) saves: every descriptor is kept -- unlike
// writeContainerdIndexes nothing is dropped, so default hauls carry no sidecar --
// but io.containerd.image.name annotations are normalized where present. This
// matters because save also restores the oci-layout marker for default hauls,
// which steers `ctr images import` onto the OCI path where the annotation is
// read verbatim (#744 amended §3). The store's own index.json is untouched.
func writeDefaultIndex(storeDir, outDir string) error {
	data, err := os.ReadFile(filepath.Join(storeDir, ocispec.ImageIndexFile))
	if err != nil {
		return err
	}

	var idx ocispec.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return err
	}
	for i := range idx.Manifests {
		normalizeContainerdName(idx.Manifests[i].Annotations)
	}

	out, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, ocispec.ImageIndexFile), out, 0666)
}

func (x *exports) describe() tarball.Manifest {
	m := make(tarball.Manifest, len(x.digests))
	for i, d := range x.digests {
		m[i] = x.records[d]
	}
	return m
}

func (x *exports) record(ctx context.Context, index libv1.ImageIndex, desc libv1.Descriptor, refname string, parentDigest string, platformSpecified bool) error {
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

	ref, err := reference.ParseReference(refname)
	if err != nil {
		return err
	}

	// record tags for the digest, eliminating dupes
	switch tag := ref.(type) {
	case goname.Tag:
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
	case goname.Digest:
		// For digest-only refs, derive a deterministic, docker-valid tag from the
		// digest the user actually pinned (#642, #744). For a multi-arch index that
		// is the PARENT index digest, not this child's; children disambiguate with
		// an os-arch suffix because docker load keeps only the last of duplicate tags.
		named, err := referencev3.ParseNormalizedNamed(tag.Repository.Name())
		if err != nil {
			return err
		}
		familiarRepo := referencev3.FamiliarName(named)
		base := digest
		suffix := ""
		if parentDigest != "" {
			base = parentDigest
			if !platformSpecified && desc.Platform != nil {
				suffix = "-" + desc.Platform.OS + "-" + desc.Platform.Architecture
				if desc.Platform.Variant != "" {
					suffix += "-" + desc.Platform.Variant
				}
			}
		}
		repotag := familiarRepo + ":" + strings.ReplaceAll(base, ":", "-") + suffix
		xd.RepoTags = append(xd.RepoTags[:], repotag)
		slices.Sort(xd.RepoTags)
		xd.RepoTags = slices.Compact(xd.RepoTags)
	}

	l.Debugf("image [%s]: type=%s, size=%d", ref.Name(), desc.MediaType, desc.Size)
	// record export descriptor for the digest
	x.records[digest] = xd

	return nil
}
