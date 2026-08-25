package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	goname "github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/mod/semver"
	"gopkg.in/yaml.v3"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/reference"
	"hauler.dev/go/hauler/v2/pkg/store"
)

// manifestImage, manifestChart, and manifestFile mirror the relevant fields of
// v1.Image/v1.Chart/v1.File, but keep only what can be confidently recovered from the
// store's metadata and use "omitempty" throughout (unlike the api types, which most
// callers unmarshal rather than marshal) so the generated manifest stays readable
// instead of listing every unset flag.
type manifestImage struct {
	Name     string `yaml:"name"`
	Platform string `yaml:"platform,omitempty"`
	Rewrite  string `yaml:"rewrite,omitempty"`
}

type manifestChart struct {
	Name    string `yaml:"name"`
	RepoURL string `yaml:"repoURL,omitempty"`
	Version string `yaml:"version,omitempty"`
	Rewrite string `yaml:"rewrite,omitempty"`
}

type manifestFile struct {
	Path string `yaml:"path"`
	Name string `yaml:"name,omitempty"`
}

type manifestMetadata struct {
	Name string `yaml:"name"`
}

type manifestDoc struct {
	APIVersion string           `yaml:"apiVersion"`
	Kind       string           `yaml:"kind"`
	Metadata   manifestMetadata `yaml:"metadata"`
	Spec       interface{}      `yaml:"spec"`
}

// CreateManifestCmd walks the store's OCI index (and the manifests/configs it
// references) to reconstruct a hauler content manifest capable of recreating the
// store's contents via `hauler store sync`. It groups discovered content into
// Images/Charts/Files documents and writes them to o.Output, or to stdout when
// o.Output is empty.
func CreateManifestCmd(ctx context.Context, o *flags.CreateManifestOpts, s *store.Layout) error {
	l := log.FromContext(ctx)

	toStdout := o.Output == ""
	if toStdout {
		l.SetLevel("fatal")
	}

	// Warn when the store predates the provenance metadata this command relies on
	// to faithfully reconstruct the manifest. Written to stderr so it stays visible
	// even in stdout mode (where the logger is silenced and stdout carries the YAML).
	if version, err := readStoreHaulerVersion(s.Root); err != nil || storeLacksProvenance(version) {
		fmt.Fprintln(os.Stderr, "WARNING: The version of Hauler used to create this store did not include provenance metadata to reconstruct the manifest. Please confirm the generated manifest is accurate.")
	}

	// Advise reviewing the output regardless of provenance. Written to stderr so it
	// stays visible even in stdout mode (where the logger is silenced and stdout
	// carries the YAML).
	fmt.Fprintln(os.Stderr, "INFO: Always confirm the accuracy of the generated manifest before recreating store.")

	var images []manifestImage
	var charts []manifestChart
	var files []manifestFile
	chartsMissingRepoURL := false

	if err := s.Walk(func(_ string, desc ocispec.Descriptor) error {
		refName, ok := desc.Annotations[ocispec.AnnotationRefName]
		if !ok {
			return nil
		}

		kind := desc.Annotations[consts.KindAnnotationName]
		switch {
		case kind == consts.KindAnnotationSigs, kind == consts.KindAnnotationAtts, kind == consts.KindAnnotationSboms:
			// cosign-related artifacts are rediscovered automatically when the
			// parent image is re-added, so they don't need their own entry.
			return nil
		case strings.HasPrefix(kind, consts.KindAnnotationReferrers):
			return nil
		}

		// Container images (both single-platform and multi-arch indexes) carry the
		// full OCI reference under this annotation; charts and files never do.
		if fullRef, isImage := desc.Annotations[consts.ContainerdImageNameKey]; isImage {
			name := fullRef
			rewrite := ""
			if orig, ok := desc.Annotations[consts.OriginalRefAnnotation]; ok && orig != "" && orig != fullRef {
				// The current ref differs from what was captured at the initial add,
				// meaning --rewrite changed it since. Recover the original, pullable
				// name and reapply the same rewrite so a resync reproduces this exact
				// store layout. If there's no annotation at all (a store from before
				// this was tracked) or it matches fullRef (never rewritten), fullRef
				// is already the right, pullable name.
				rewrite = fullRef
				name = orig
			}

			img := manifestImage{Name: name, Rewrite: rewrite}
			if kind == consts.KindAnnotationImage {
				// Only a single-platform manifest has an unambiguous platform to pin.
				// A stored multi-arch index is left unset so a future sync re-pulls
				// every platform, matching what's actually in the store.
				platform, err := imagePlatform(ctx, s, desc)
				if err != nil {
					l.Warnf("could not determine platform for image [%s]: %v", name, err)
				} else if platform != "" {
					img.Platform = platform
				}
			}
			images = append(images, img)
			return nil
		}

		rc, err := s.Fetch(ctx, desc)
		if err != nil {
			return fmt.Errorf("fetching manifest for [%s]: %w", refName, err)
		}
		defer rc.Close()

		var m ocispec.Manifest
		if err := json.NewDecoder(rc).Decode(&m); err != nil {
			return fmt.Errorf("decoding manifest for [%s]: %w", refName, err)
		}

		ref, err := reference.ParseReference(refName)
		if err != nil {
			return fmt.Errorf("parsing reference [%s]: %w", refName, err)
		}
		name := strings.TrimPrefix(ref.Context().RepositoryStr(), consts.DefaultNamespace+"/")

		switch m.Config.MediaType {
		case consts.ChartConfigMediaType:
			version := ref.Identifier()
			if tag, ok := ref.(goname.Tag); ok {
				version = tag.TagStr()
			}

			repoURL := ""
			rewrite := ""
			if orig, ok := desc.Annotations[consts.OriginalRefAnnotation]; ok && orig != "" {
				origRepoURL, origTotal := decodeOriginalChartRef(orig)
				repoURL = origRepoURL
				if origTotal != "" && origTotal != refName {
					// The current ref differs from what was captured at the initial
					// add, meaning --rewrite changed it since. Recover the original,
					// pullable name/version and reapply the same rewrite so a resync
					// reproduces this exact store layout.
					rewrite = refName
					if origRef, err := reference.ParseReference(origTotal); err == nil {
						name = strings.TrimPrefix(origRef.Context().RepositoryStr(), consts.DefaultNamespace+"/")
						version = origRef.Identifier()
						if tag, ok := origRef.(goname.Tag); ok {
							version = tag.TagStr()
						}
					}
				}
			}

			charts = append(charts, manifestChart{Name: name, RepoURL: repoURL, Version: version, Rewrite: rewrite})
			if repoURL == "" {
				chartsMissingRepoURL = true
			}

		case consts.FileLocalConfigMediaType, consts.FileHttpConfigMediaType, consts.FileDirectoryConfigMediaType:
			path := name
			if orig, ok := desc.Annotations[consts.OriginalRefAnnotation]; ok && orig != "" {
				path = orig
			}
			files = append(files, manifestFile{Path: path, Name: name})

		default:
			l.Warnf("skipping unrecognized artifact [%s] with config media type [%s]", refName, m.Config.MediaType)
		}

		return nil
	}); err != nil {
		return err
	}

	if len(images) == 0 && len(charts) == 0 && len(files) == 0 {
		return fmt.Errorf("store contains no content to build a manifest from")
	}

	base := filepath.Base(s.Root)

	var out strings.Builder
	if len(images) > 0 {
		if err := writeDoc(&out, "", consts.ImagesContentKind, base+"-images", struct {
			Images []manifestImage `yaml:"images"`
		}{images}); err != nil {
			return err
		}
	}
	if len(charts) > 0 {
		header := ""
		if chartsMissingRepoURL {
			header = "# NOTE: repoURL could not be recovered from the store's metadata and must be filled in below.\n"
		}
		if err := writeDoc(&out, header, consts.ChartsContentKind, base+"-charts", struct {
			Charts []manifestChart `yaml:"charts"`
		}{charts}); err != nil {
			return err
		}
	}
	if len(files) > 0 {
		if err := writeDoc(&out, "", consts.FilesContentKind, base+"-files", struct {
			Files []manifestFile `yaml:"files"`
		}{files}); err != nil {
			return err
		}
	}

	if toStdout {
		if _, err := os.Stdout.Write([]byte(out.String())); err != nil {
			return err
		}
		return nil
	}

	if err := os.WriteFile(o.Output, []byte(out.String()), 0o644); err != nil {
		return fmt.Errorf("writing manifest to [%s]: %w", o.Output, err)
	}

	outPath := o.Output
	if abs, err := filepath.Abs(o.Output); err == nil {
		outPath = abs
	}
	l.Infof("wrote manifest with [%d] image(s), [%d] chart(s), [%d] file(s) to [%s]", len(images), len(charts), len(files), outPath)

	return nil
}

// provenanceMinVersion is the first Hauler release whose stores record enough
// provenance metadata for `store create manifest` to faithfully reconstruct
// them. Stores written by earlier versions (or with no recorded version) get a
// best-effort manifest and a warning.
const provenanceMinVersion = "v2.1.0"

// storeVersionMetadata mirrors the subset of store.json this command reads to
// decide whether the store carries reliable provenance metadata.
type storeVersionMetadata struct {
	HaulerVersion string `json:"hauler-version"`
}

// readStoreHaulerVersion returns the "hauler-version" recorded in the store's
// store.json, or an error if the file is missing or unparseable.
func readStoreHaulerVersion(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, consts.DefaultStoreMetadataName))
	if err != nil {
		return "", err
	}
	var m storeVersionMetadata
	if err := json.Unmarshal(data, &m); err != nil {
		return "", err
	}
	return m.HaulerVersion, nil
}

// storeLacksProvenance reports whether a store written by haulerVersion predates
// provenanceMinVersion. An empty or unparseable version is treated as lacking
// provenance. The comparison is by major.minor so that pre-releases of the
// threshold (e.g. v2.1.0-rc1) are not flagged.
func storeLacksProvenance(haulerVersion string) bool {
	v := strings.TrimSpace(haulerVersion)
	if v == "" {
		return true
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return true
	}
	return semver.Compare(semver.MajorMinor(v), semver.MajorMinor(provenanceMinVersion)) < 0
}

func writeDoc(out *strings.Builder, header string, kind string, name string, spec interface{}) error {
	doc := manifestDoc{
		APIVersion: consts.ContentGroup + "/v1",
		Kind:       kind,
		Metadata:   manifestMetadata{Name: name},
		Spec:       spec,
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshaling [%s] manifest: %w", kind, err)
	}
	out.WriteString("---\n")
	out.WriteString(header)
	out.Write(data)
	return nil
}

// imagePlatform returns the "os/arch" of a single-platform image manifest by
// fetching its config blob, or "" if the platform can't be determined.
func imagePlatform(ctx context.Context, s *store.Layout, desc ocispec.Descriptor) (string, error) {
	rc, err := s.Fetch(ctx, desc)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	var m ocispec.Manifest
	if err := json.NewDecoder(rc).Decode(&m); err != nil {
		return "", err
	}

	cfgRc, err := s.FetchManifest(ctx, m)
	if err != nil {
		return "", err
	}
	defer cfgRc.Close()

	var cfg ocispec.Image
	if err := json.NewDecoder(cfgRc).Decode(&cfg); err != nil {
		return "", err
	}
	if cfg.OS == "" || cfg.Architecture == "" {
		return "", nil
	}
	return cfg.OS + "/" + cfg.Architecture, nil
}

// decodeOriginalChartRef splits a value produced by encodeOriginalChartRef (see
// storeChart in add.go) back into its repoURL and "repo:tag" parts. Values with no
// "|" (shouldn't occur once only encodeOriginalChartRef ever writes this annotation
// for charts) are treated as a bare ref with an unknown repoURL.
func decodeOriginalChartRef(v string) (repoURL string, total string) {
	repoURL, total, found := strings.Cut(v, "|")
	if !found {
		return "", v
	}
	return repoURL, total
}
