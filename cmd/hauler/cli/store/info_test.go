package store

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"

	"hauler.dev/go/hauler/v2/internal/flags"
	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/store"
)

// newCapturingContext returns a context carrying a zerolog logger that writes
// directly into buf, so log.FromContext output can be asserted on without the
// os.Stdout redirection tricks that captureStdout uses -- pkg/log.NewLogger
// hardcodes its writer to os.Stdout regardless of the io.Writer passed to it, so
// it cannot be used to capture log output in tests.
func newCapturingContext(buf *bytes.Buffer) context.Context {
	zerolog.SetGlobalLevel(zerolog.InfoLevel) // defensive
	zl := zerolog.New(buf).Level(zerolog.InfoLevel).With().Timestamp().Logger()
	return zl.WithContext(context.Background())
}

func TestByteCountSI(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{999, "999 B"},
		{1000, "1.0 kB"},
		{1500000, "1.5 MB"},
		{1000000000, "1.0 GB"},
	}
	for _, tc := range tests {
		got := byteCountSI(tc.input)
		if got != tc.want {
			t.Errorf("byteCountSI(%d) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestTruncateReference(t *testing.T) {
	longDigest := "sha256:abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"tag ref unchanged", "nginx:latest", "nginx:latest"},
		{"long digest truncated", "nginx@" + longDigest, "nginx@sha256:abcdefabcdef\u2026"},
		{"short digest not truncated", "nginx@sha256:abcdef", "nginx@sha256:abcdef"},
		{"no digest unchanged", "myrepo/myimage:v1", "myrepo/myimage:v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateReference(tc.input)
			if got != tc.want {
				t.Errorf("truncateReference(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestInfoOutputJSON(t *testing.T) {
	items := []item{
		{Reference: "myrepo/myimage:v1", Type: "image", Platform: "linux/amd64", Size: 1024, Layers: 2},
		{Reference: "myrepo/mychart:v1", Type: "chart", Platform: "-", Size: 512, Layers: 1},
	}
	out := infoOutput{
		StorePath: "/tmp/test-store",
		StoreID:   "test-store-id",
		Artifacts: items,
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json.Marshal infoOutput: %v", err)
	}
	var got infoOutput
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal infoOutput: %v\ndata: %s", err, data)
	}
	if got.StorePath != out.StorePath {
		t.Errorf("StorePath = %q, want %q", got.StorePath, out.StorePath)
	}
	if len(got.Artifacts) != len(items) {
		t.Fatalf("Artifacts len = %d, want %d", len(got.Artifacts), len(items))
	}
	for i, want := range items {
		if got.Artifacts[i].Reference != want.Reference {
			t.Errorf("Artifacts[%d].Reference = %q, want %q", i, got.Artifacts[i].Reference, want.Reference)
		}
		if got.Artifacts[i].Type != want.Type {
			t.Errorf("Artifacts[%d].Type = %q, want %q", i, got.Artifacts[i].Type, want.Type)
		}
		if got.Artifacts[i].Size != want.Size {
			t.Errorf("Artifacts[%d].Size = %d, want %d", i, got.Artifacts[i].Size, want.Size)
		}
	}
}

func TestNewItem(t *testing.T) {
	// newItem uses s only for its signature... it does not dereference s in practice.
	// We pass nil to keep tests dependency-free.
	const validRef = "myrepo/myimage:latest"

	makeDesc := func(kindAnnotation string) ocispec.Descriptor {
		desc := ocispec.Descriptor{
			Annotations: map[string]string{
				"io.containerd.image.name": validRef,
			},
		}
		if kindAnnotation != "" {
			desc.Annotations[consts.KindAnnotationName] = kindAnnotation
		}
		return desc
	}
	makeManifest := func(configMediaType string) ocispec.Manifest {
		return ocispec.Manifest{
			Config: ocispec.Descriptor{MediaType: configMediaType},
		}
	}

	tests := []struct {
		name           string
		configMedia    string
		kindAnnotation string
		typeFilter     string
		wantType       string
		wantEmpty      bool
	}{
		{
			name:        "DockerConfigJSON → image",
			configMedia: consts.DockerConfigJSON,
			typeFilter:  "all",
			wantType:    "image",
		},
		{
			name:        "ChartConfigMediaType → chart",
			configMedia: consts.ChartConfigMediaType,
			typeFilter:  "all",
			wantType:    "chart",
		},
		{
			name:        "FileLocalConfigMediaType → file",
			configMedia: consts.FileLocalConfigMediaType,
			typeFilter:  "all",
			wantType:    "file",
		},
		{
			name:           "KindAnnotationSigs → sigs",
			configMedia:    consts.DockerConfigJSON,
			kindAnnotation: consts.KindAnnotationSigs,
			typeFilter:     "all",
			wantType:       "sigs",
		},
		{
			name:           "KindAnnotationAtts → atts",
			configMedia:    consts.DockerConfigJSON,
			kindAnnotation: consts.KindAnnotationAtts,
			typeFilter:     "all",
			wantType:       "atts",
		},
		{
			name:           "KindAnnotationReferrers prefix → referrer",
			configMedia:    consts.DockerConfigJSON,
			kindAnnotation: consts.KindAnnotationReferrers + "/abc123",
			typeFilter:     "all",
			wantType:       "referrer",
		},
		{
			// Suffixed form written for a multi-arch image's child manifest
			// (nameMapKey's <ref>-<kind> stays unique per subject); must label
			// the same as the plain "KindAnnotationSigs → sigs" case above.
			name:           "KindAnnotationSigs suffixed → sigs",
			configMedia:    consts.DockerConfigJSON,
			kindAnnotation: consts.KindAnnotationSigs + "/0123abcd",
			typeFilter:     "all",
			wantType:       "sigs",
		},
		{
			name:           "KindAnnotationSboms suffixed → sbom",
			configMedia:    consts.DockerConfigJSON,
			kindAnnotation: consts.KindAnnotationSboms + "/0123abcd",
			typeFilter:     "all",
			wantType:       "sbom",
		},
		{
			name:        "TypeFilter:image with chart → empty item",
			configMedia: consts.ChartConfigMediaType,
			typeFilter:  "image",
			wantEmpty:   true,
		},
		{
			name:        "TypeFilter:file with image → empty item",
			configMedia: consts.DockerConfigJSON,
			typeFilter:  "file",
			wantEmpty:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			desc := makeDesc(tc.kindAnnotation)
			m := makeManifest(tc.configMedia)
			o := &flags.InfoOpts{TypeFilter: tc.typeFilter}

			got := newItem(nil, desc, m, "linux/amd64", o)
			if tc.wantEmpty {
				if !got.isEmpty() {
					t.Errorf("expected empty item, got %+v", got)
				}
				return
			}
			if got.isEmpty() {
				t.Fatalf("got empty item, want type %q", tc.wantType)
			}
			if got.Type != tc.wantType {
				t.Errorf("got type %q, want %q", got.Type, tc.wantType)
			}
		})
	}
}

func TestResolveDisplayReference(t *testing.T) {
	// ContainerdImageNameKey already holds the fully-qualified reference exactly as
	// computed by rewriteReference (see add.go), so it must be returned verbatim.
	// Re-parsing it through the reference package would re-trigger
	// go-containerregistry's docker hub "library/" normalization for a
	// single-segment repo, undoing a rewrite like "hello-world-custom" back to
	// "library/hello-world-custom".
	t.Run("ContainerdImageNameKey is used verbatim, without re-injecting library/", func(t *testing.T) {
		desc := ocispec.Descriptor{
			Annotations: map[string]string{
				consts.ContainerdImageNameKey: "index.docker.io/hello-world-custom:v2",
				ocispec.AnnotationRefName:     "hello-world-custom:v2",
			},
		}
		got, err := resolveDisplayReference(desc)
		if err != nil {
			t.Fatalf("resolveDisplayReference: %v", err)
		}
		if want := "index.docker.io/hello-world-custom:v2"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("falls back to AnnotationRefName parsed when ContainerdImageNameKey absent", func(t *testing.T) {
		desc := ocispec.Descriptor{
			Annotations: map[string]string{
				ocispec.AnnotationRefName: "hello-world-custom:v2",
			},
		}
		got, err := resolveDisplayReference(desc)
		if err != nil {
			t.Fatalf("resolveDisplayReference: %v", err)
		}
		if want := "hauler/hello-world-custom:v2"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("returns error when fallback ref cannot be parsed", func(t *testing.T) {
		desc := ocispec.Descriptor{Annotations: map[string]string{}}
		if _, err := resolveDisplayReference(desc); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestNewItem_ReferenceUsesContainerdImageNameVerbatim(t *testing.T) {
	desc := ocispec.Descriptor{
		Annotations: map[string]string{
			consts.ContainerdImageNameKey: "index.docker.io/hello-world-custom:v2",
			ocispec.AnnotationRefName:     "hello-world-custom:v2",
		},
	}
	m := ocispec.Manifest{Config: ocispec.Descriptor{MediaType: consts.DockerConfigJSON}}
	o := &flags.InfoOpts{TypeFilter: "all"}

	got := newItem(nil, desc, m, "linux/amd64", o)
	if want := "index.docker.io/hello-world-custom:v2"; got.Reference != want {
		t.Errorf("got Reference %q, want %q", got.Reference, want)
	}
}

func TestFallbackItem_ReferenceUsesContainerdImageNameVerbatim(t *testing.T) {
	desc := ocispec.Descriptor{
		Annotations: map[string]string{
			consts.ContainerdImageNameKey: "index.docker.io/hello-world-custom:v2",
			ocispec.AnnotationRefName:     "hello-world-custom:v2",
		},
	}

	got := fallbackItem(desc, "linux/amd64", store.BlobResult{})
	if want := "index.docker.io/hello-world-custom:v2"; got.Reference != want {
		t.Errorf("got Reference %q, want %q", got.Reference, want)
	}
}

func TestInfoCmd(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	// Seed a file artifact using a local temp file.
	tmpFile := t.TempDir() + "/hello.txt"
	if err := os.WriteFile(tmpFile, []byte("hello hauler"), 0o644); err != nil {
		t.Fatalf("write tmpFile: %v", err)
	}
	fi := v1.File{Path: tmpFile}
	if err := storeFile(ctx, s, fi, defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	baseOpts := func(typeFilter, format string) *flags.InfoOpts {
		return &flags.InfoOpts{
			StoreRootOpts: defaultRootOpts(s.Root),
			OutputFormat:  format,
			TypeFilter:    typeFilter,
		}
	}

	t.Run("TypeFilter:all json", func(t *testing.T) {
		if err := InfoCmd(ctx, baseOpts("all", "json"), s); err != nil {
			t.Errorf("InfoCmd(all, json): %v", err)
		}
	})

	t.Run("TypeFilter:file json", func(t *testing.T) {
		if err := InfoCmd(ctx, baseOpts("file", "json"), s); err != nil {
			t.Errorf("InfoCmd(file, json): %v", err)
		}
	})

	t.Run("TypeFilter:image json", func(t *testing.T) {
		// Store has only a file artifact... image filter returns no items (no error).
		if err := InfoCmd(ctx, baseOpts("image", "json"), s); err != nil {
			t.Errorf("InfoCmd(image, json): %v", err)
		}
	})

	t.Run("TypeFilter:all table", func(t *testing.T) {
		if err := InfoCmd(ctx, baseOpts("all", "table"), s); err != nil {
			t.Errorf("InfoCmd(all, table): %v", err)
		}
	})
}

// captureStdout redirects os.Stdout for the duration of fn and returns everything
// written to it, along with fn's return value.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	fnErr := fn()

	w.Close()
	os.Stdout = oldStdout

	var buf strings.Builder
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	r.Close()

	return buf.String(), fnErr
}

// findManifestDescriptor walks s and returns the descriptor for the first entry
// whose AnnotationRefName contains refSubstring. Fails the test if none is found.
func findManifestDescriptor(t *testing.T, s *store.Layout, refSubstring string) ocispec.Descriptor {
	t.Helper()
	var found ocispec.Descriptor
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		if strings.Contains(desc.Annotations[ocispec.AnnotationRefName], refSubstring) {
			found = desc
		}
		return nil
	}); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if found.Digest == "" {
		t.Fatalf("no artifact found for ref containing %q", refSubstring)
	}
	return found
}

// corruptBlobFile flips the first byte of the blob identified by d, preserving its
// exact length -- a same-length corruption that only a real digest check can catch.
func corruptBlobFile(t *testing.T, root string, d digest.Digest) {
	t.Helper()
	path := filepath.Join(root, ocispec.ImageBlobsDir, d.Algorithm().String(), d.Hex())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read blob %s: %v", d, err)
	}
	corrupted := append([]byte(nil), data...)
	corrupted[0] ^= 0xFF
	if err := os.WriteFile(path, corrupted, 0o644); err != nil {
		t.Fatalf("corrupt blob %s: %v", d, err)
	}
}

// firstLayerDigest reads and parses the manifest blob for desc and returns the
// digest of its first layer. Fails the test if the manifest has no layers.
func firstLayerDigest(t *testing.T, root string, desc ocispec.Descriptor) digest.Digest {
	t.Helper()
	path := filepath.Join(root, ocispec.ImageBlobsDir, desc.Digest.Algorithm().String(), desc.Digest.Hex())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest blob %s: %v", desc.Digest, err)
	}
	var m ocispec.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal manifest %s: %v", desc.Digest, err)
	}
	if len(m.Layers) == 0 {
		t.Fatalf("manifest %s has no layers", desc.Digest)
	}
	return m.Layers[0].Digest
}

// TestInfoCmd_CheckHealthyStore checks that --check on a store with no corruption
// reports an empty artifacts array in JSON output (a failure report shows only
// corrupt artifacts, and there are none here) -- never null, so downstream jq
// pipelines can rely on the array always being present.
func TestInfoCmd_CheckHealthyStore(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	host, opts := newTestRegistry(t)

	seedImage(t, host, "test/healthy", "v1", opts...)
	if _, err := s.AddImage(ctx, host+"/test/healthy:v1", "", true, "", false, "", opts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	tmpFile := t.TempDir() + "/hello.txt"
	if err := os.WriteFile(tmpFile, []byte("hello hauler"), 0o644); err != nil {
		t.Fatalf("write tmpFile: %v", err)
	}
	if err := storeFile(ctx, s, v1.File{Path: tmpFile}, defaultCliOpts(), defaultRootOpts(s.Root)); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	o := &flags.InfoOpts{
		StoreRootOpts: defaultRootOpts(s.Root),
		OutputFormat:  "json",
		TypeFilter:    "all",
		Check:         true,
	}

	out, err := captureStdout(t, func() error { return InfoCmd(ctx, o, s) })
	if err != nil {
		t.Fatalf("InfoCmd: %v\noutput: %s", err, out)
	}

	var got infoOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal output: %v\noutput: %s", err, out)
	}
	if len(got.Artifacts) != 0 {
		t.Errorf("expected zero artifacts in a healthy-store failure report, got %d: %+v", len(got.Artifacts), got.Artifacts)
	}

	// The raw JSON must render the artifacts array as "[]", never "null" --
	// jq pipelines depend on this.
	if !strings.Contains(out, `"artifacts": []`) {
		t.Errorf("expected raw JSON to contain an empty artifacts array (\"artifacts\": []), got: %s", out)
	}
}

// TestInfoCmd_CheckCorruptBlob_HealthySiblingOmitted checks that --check reports
// only the artifact whose blob was corrupted (with non-empty problems), while a
// healthy sibling artifact is omitted entirely from the failure report -- not
// present-with-no-problems, actually absent. InfoCmd returns nil even though
// corruption is found; reporting happens via the JSON payload / failure table and
// a WARN log line, never via a non-zero exit.
func TestInfoCmd_CheckCorruptBlob_HealthySiblingOmitted(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	host, opts := newTestRegistry(t)

	seedImage(t, host, "test/corrupt-a", "v1", opts...)
	if _, err := s.AddImage(ctx, host+"/test/corrupt-a:v1", "", true, "", false, "", opts...); err != nil {
		t.Fatalf("AddImage corrupt-a: %v", err)
	}
	seedImage(t, host, "test/healthy-b", "v1", opts...)
	if _, err := s.AddImage(ctx, host+"/test/healthy-b:v1", "", true, "", false, "", opts...); err != nil {
		t.Fatalf("AddImage healthy-b: %v", err)
	}

	desc := findManifestDescriptor(t, s, "test/corrupt-a")
	victim := firstLayerDigest(t, s.Root, desc)
	corruptBlobFile(t, s.Root, victim)

	o := &flags.InfoOpts{
		StoreRootOpts: defaultRootOpts(s.Root),
		OutputFormat:  "json",
		TypeFilter:    "all",
		Check:         true,
	}

	out, err := captureStdout(t, func() error { return InfoCmd(ctx, o, s) })
	if err != nil {
		t.Fatalf("InfoCmd: %v (corrupt artifacts must be reported via WARN log + JSON payload, not a returned error)\noutput: %s", err, out)
	}
	if out == "" {
		t.Fatal("expected InfoCmd to print output")
	}

	var got infoOutput
	if jerr := json.Unmarshal([]byte(out), &got); jerr != nil {
		t.Fatalf("unmarshal output: %v\noutput: %s", jerr, out)
	}

	if len(got.Artifacts) != 1 {
		t.Fatalf("expected exactly one artifact in the failure report, got %d: %+v", len(got.Artifacts), got.Artifacts)
	}

	var sawCorrupt bool
	for _, a := range got.Artifacts {
		if strings.Contains(a.Reference, "healthy-b") {
			t.Errorf("healthy-b must be omitted entirely from the failure report, got: %+v", a)
		}
		if strings.Contains(a.Reference, "corrupt-a") {
			sawCorrupt = true
			if len(a.Problems) == 0 {
				t.Errorf("corrupt-a: expected non-empty problems")
			}
		}
	}
	if !sawCorrupt {
		t.Error("expected a row for test/corrupt-a")
	}
}

// TestInfoCmd_CheckCorruptManifest_RegressionGuard is the regression guard for the
// bug this feature must not perpetuate: today, a JSON decode error thrown while
// walking the store kills the entire `hauler store info` command. Under --check, a
// corrupted manifest blob must instead degrade to a single "corrupt" row, and
// InfoCmd must still render output and return nil despite the corruption, instead
// of the old hard-failure behavior.
func TestInfoCmd_CheckCorruptManifest_RegressionGuard(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	host, opts := newTestRegistry(t)

	seedImage(t, host, "test/badmanifest", "v1", opts...)
	if _, err := s.AddImage(ctx, host+"/test/badmanifest:v1", "", true, "", false, "", opts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	desc := findManifestDescriptor(t, s, "test/badmanifest")
	corruptBlobFile(t, s.Root, desc.Digest)

	o := &flags.InfoOpts{
		StoreRootOpts: defaultRootOpts(s.Root),
		OutputFormat:  "json",
		TypeFilter:    "all",
		Check:         true,
	}

	out, err := captureStdout(t, func() error { return InfoCmd(ctx, o, s) })
	if err != nil {
		t.Fatalf("InfoCmd: %v (must return nil even when a corrupted manifest is found; reporting is via WARN log + status row)\noutput: %s", err, out)
	}
	if out == "" {
		t.Fatal("expected InfoCmd to still render output despite the corrupted manifest, not abort silently")
	}

	var got infoOutput
	if jerr := json.Unmarshal([]byte(out), &got); jerr != nil {
		t.Fatalf("unmarshal output: %v\noutput: %s", jerr, out)
	}
	if len(got.Artifacts) != 1 {
		t.Fatalf("expected exactly one row, got %d: %+v", len(got.Artifacts), got.Artifacts)
	}
	if len(got.Artifacts[0].Problems) == 0 {
		t.Error("expected non-empty problems for the corrupted manifest row")
	}
}

// TestInfoCmd_NoCheck_OutputUnchanged checks that omitting --check produces
// byte-for-byte the same JSON shape as before this feature existed: no "status" or
// "problems" key appears anywhere in the output (both are omitempty).
func TestInfoCmd_NoCheck_OutputUnchanged(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	host, opts := newTestRegistry(t)

	seedImage(t, host, "test/plain", "v1", opts...)
	if _, err := s.AddImage(ctx, host+"/test/plain:v1", "", true, "", false, "", opts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	o := &flags.InfoOpts{
		StoreRootOpts: defaultRootOpts(s.Root),
		OutputFormat:  "json",
		TypeFilter:    "all",
		// Check intentionally left false (zero value).
	}

	out, err := captureStdout(t, func() error { return InfoCmd(ctx, o, s) })
	if err != nil {
		t.Fatalf("InfoCmd: %v\noutput: %s", err, out)
	}

	if strings.Contains(out, `"status"`) {
		t.Errorf("expected no \"status\" key in output when --check is off, got: %s", out)
	}
	if strings.Contains(out, `"problems"`) {
		t.Errorf("expected no \"problems\" key in output when --check is off, got: %s", out)
	}

	var raw map[string]interface{}
	if jerr := json.Unmarshal([]byte(out), &raw); jerr != nil {
		t.Fatalf("unmarshal output: %v\noutput: %s", jerr, out)
	}
	artifacts, ok := raw["artifacts"].([]interface{})
	if !ok || len(artifacts) == 0 {
		t.Fatalf("expected at least one artifact, got: %+v", raw)
	}
	for _, a := range artifacts {
		am, ok := a.(map[string]interface{})
		if !ok {
			t.Fatalf("artifact entry is not an object: %+v", a)
		}
		if _, present := am["status"]; present {
			t.Errorf("artifact has a \"status\" key when --check is off: %+v", am)
		}
		if _, present := am["problems"]; present {
			t.Errorf("artifact has a \"problems\" key when --check is off: %+v", am)
		}
	}
}

// TestInfoCmd_CheckWithTypeFilter_SkipsFilteredCorruption checks that combining
// --check with --type filters out non-matching artifacts before the integrity check,
// so a corrupt artifact of a filtered-out type produces neither a row nor an error --
// and since the one remaining (image) artifact is healthy, the failure report ends up
// empty entirely.
func TestInfoCmd_CheckWithTypeFilter_SkipsFilteredCorruption(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	host, opts := newTestRegistry(t)

	seedImage(t, host, "test/filterhealthy", "v1", opts...)
	if _, err := s.AddImage(ctx, host+"/test/filterhealthy:v1", "", true, "", false, "", opts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	// Add a real chart from testdata (per repo convention) and corrupt one of its
	// layer blobs -- a non-image artifact that --type image should filter out
	// before ever attempting the integrity check.
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()
	chartOpts := newAddChartOpts(chartTestdataDir, "")
	if err := AddChartCmd(ctx, chartOpts, s, "rancher-cluster-templates-0.5.2.tgz", rso, ro); err != nil {
		t.Fatalf("AddChartCmd: %v", err)
	}
	chartDesc := findManifestDescriptor(t, s, "rancher-cluster-templates")
	chartVictim := firstLayerDigest(t, s.Root, chartDesc)
	corruptBlobFile(t, s.Root, chartVictim)

	o := &flags.InfoOpts{
		StoreRootOpts: defaultRootOpts(s.Root),
		OutputFormat:  "json",
		TypeFilter:    "image",
		Check:         true,
	}

	out, err := captureStdout(t, func() error { return InfoCmd(ctx, o, s) })
	if err != nil {
		t.Fatalf("InfoCmd: %v (the filtered-out corrupt chart must not surface an error)\noutput: %s", err, out)
	}

	var got infoOutput
	if jerr := json.Unmarshal([]byte(out), &got); jerr != nil {
		t.Fatalf("unmarshal output: %v\noutput: %s", jerr, out)
	}

	if len(got.Artifacts) != 0 {
		t.Errorf("expected zero artifacts (the corrupt chart is filtered out, and the remaining image is healthy), got %d: %+v", len(got.Artifacts), got.Artifacts)
	}
}

// TestInfoCmd_CheckHealthyStore_TableFormat_NoTableRendered checks that --check on
// a healthy store, with table output requested, renders no table at all -- only an
// INFO log line reporting that every artifact passed.
func TestInfoCmd_CheckHealthyStore_TableFormat_NoTableRendered(t *testing.T) {
	origLevel := zerolog.GlobalLevel()
	t.Cleanup(func() { zerolog.SetGlobalLevel(origLevel) })

	s := newTestStore(t)
	host, opts := newTestRegistry(t)

	seedImage(t, host, "test/healthytable", "v1", opts...)

	var buf bytes.Buffer
	ctx := newCapturingContext(&buf)

	if _, err := s.AddImage(ctx, host+"/test/healthytable:v1", "", true, "", false, "", opts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	o := &flags.InfoOpts{
		StoreRootOpts: defaultRootOpts(s.Root),
		OutputFormat:  "table",
		TypeFilter:    "all",
		Check:         true,
	}

	stdout, err := captureStdout(t, func() error { return InfoCmd(ctx, o, s) })
	if err != nil {
		t.Fatalf("InfoCmd: %v\nstdout: %s", err, stdout)
	}

	if strings.Contains(stdout, "Reference") {
		t.Errorf("expected no table header on stdout for a healthy store, got: %q", stdout)
	}
	for _, borderChar := range []string{"┌", "├", "└", "│"} {
		if strings.Contains(stdout, borderChar) {
			t.Errorf("expected no table border characters on stdout for a healthy store, got: %q", stdout)
		}
	}

	if !strings.Contains(buf.String(), "passed the integrity check") {
		t.Errorf("expected log output to contain the pass summary, got: %q", buf.String())
	}
}

// TestInfoCmd_CheckCorruptBlob_RemediationHintLogged checks that when --check finds
// corruption, InfoCmd logs a single generic remediation statement telling the user
// they can fix it by removing and re-adding the affected artifact(s) -- not a
// per-artifact list of `hauler store remove <ref>` commands. A generic statement
// avoids walking users into ref-matching edge cases (see e.g. the nameMap-key vs.
// fully-qualified-ref mismatch that made copy-pasted per-ref hints unreliable).
func TestInfoCmd_CheckCorruptBlob_RemediationHintLogged(t *testing.T) {
	origLevel := zerolog.GlobalLevel()
	t.Cleanup(func() { zerolog.SetGlobalLevel(origLevel) })

	s := newTestStore(t)
	host, opts := newTestRegistry(t)

	var buf bytes.Buffer
	ctx := newCapturingContext(&buf)

	seedImage(t, host, "test/remediation", "v1", opts...)
	if _, err := s.AddImage(ctx, host+"/test/remediation:v1", "", true, "", false, "", opts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	desc := findManifestDescriptor(t, s, "test/remediation")
	victim := firstLayerDigest(t, s.Root, desc)
	corruptBlobFile(t, s.Root, victim)

	o := &flags.InfoOpts{
		StoreRootOpts: defaultRootOpts(s.Root),
		OutputFormat:  "table",
		TypeFilter:    "all",
		Check:         true,
	}

	if _, err := captureStdout(t, func() error { return InfoCmd(ctx, o, s) }); err != nil {
		t.Fatalf("InfoCmd: %v", err)
	}

	logOutput := buf.String()
	if strings.Contains(logOutput, "hauler store remove") {
		t.Errorf("expected no per-artifact `hauler store remove` command, got: %q", logOutput)
	}
	if !strings.Contains(logOutput, "remove and re-add") {
		t.Errorf("expected a generic remediation statement mentioning removing and re-adding the artifact, got: %q", logOutput)
	}
	if !strings.Contains(logOutput, "failed the integrity check") {
		t.Errorf("expected a WARN summary with the failure count, got: %q", logOutput)
	}
}

// TestInfoCmd_RemediationHint_DedupedForMultiPlatformImage checks that a
// multi-platform image failing its integrity check on more than one platform still
// produces exactly ONE generic remediation statement, not one per failing platform.
func TestInfoCmd_RemediationHint_DedupedForMultiPlatformImage(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	host, opts := newTestRegistry(t)

	var buf bytes.Buffer
	logCtx := newCapturingContext(&buf)

	idx := seedIndex(t, host, "test/remediation-multiarch", "v1", opts...)
	if _, err := s.AddImage(ctx, host+"/test/remediation-multiarch:v1", "", true, "", false, "", opts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	manifests, err := idx.IndexManifest()
	if err != nil {
		t.Fatalf("idx.IndexManifest: %v", err)
	}
	if len(manifests.Manifests) < 2 {
		t.Fatalf("expected at least 2 platform manifests, got %d", len(manifests.Manifests))
	}
	for _, pm := range manifests.Manifests {
		platImg, err := idx.Image(pm.Digest)
		if err != nil {
			t.Fatalf("idx.Image(%s): %v", pm.Digest, err)
		}
		layers, err := platImg.Layers()
		if err != nil {
			t.Fatalf("platImg.Layers: %v", err)
		}
		if len(layers) == 0 {
			t.Fatalf("platform manifest %s has no layers", pm.Digest)
		}
		ld, err := layers[0].Digest()
		if err != nil {
			t.Fatalf("layer.Digest: %v", err)
		}
		corruptBlobFile(t, s.Root, digest.NewDigestFromHex(ld.Algorithm, ld.Hex))
	}

	o := &flags.InfoOpts{
		StoreRootOpts: defaultRootOpts(s.Root),
		OutputFormat:  "table",
		TypeFilter:    "all",
		Check:         true,
	}

	if _, err := captureStdout(t, func() error { return InfoCmd(logCtx, o, s) }); err != nil {
		t.Fatalf("InfoCmd: %v", err)
	}

	logOutput := buf.String()
	if strings.Contains(logOutput, "hauler store remove") {
		t.Errorf("expected no per-artifact `hauler store remove` command, got: %q", logOutput)
	}
	if n := strings.Count(logOutput, "remove and re-add"); n != 1 {
		t.Errorf("expected exactly 1 generic remediation statement for a multi-platform failure, got %d\nlog output: %s", n, logOutput)
	}
}

// TestInfoCmd_CheckMultipleBadBlobs_OneRowPerProblem checks that an artifact with
// multiple corrupted blobs produces one Problems entry per bad blob in JSON output,
// and one table row per problem (only column 0, Reference, is vertically merged).
func TestInfoCmd_CheckMultipleBadBlobs_OneRowPerProblem(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	host, opts := newTestRegistry(t)

	img := seedImage(t, host, "test/doublebad", "v1", opts...)
	if _, err := s.AddImage(ctx, host+"/test/doublebad:v1", "", true, "", false, "", opts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	layers, err := img.Layers()
	if err != nil {
		t.Fatalf("img.Layers: %v", err)
	}
	if len(layers) < 2 {
		t.Fatalf("expected at least 2 layers, got %d", len(layers))
	}
	for _, l := range layers {
		d, err := l.Digest()
		if err != nil {
			t.Fatalf("layer.Digest: %v", err)
		}
		corruptBlobFile(t, s.Root, digest.NewDigestFromHex(d.Algorithm, d.Hex))
	}

	jsonOpts := &flags.InfoOpts{
		StoreRootOpts: defaultRootOpts(s.Root),
		OutputFormat:  "json",
		TypeFilter:    "all",
		Check:         true,
	}

	out, err := captureStdout(t, func() error { return InfoCmd(ctx, jsonOpts, s) })
	if err != nil {
		t.Fatalf("InfoCmd (json): %v\noutput: %s", err, out)
	}

	var got infoOutput
	if jerr := json.Unmarshal([]byte(out), &got); jerr != nil {
		t.Fatalf("unmarshal output: %v\noutput: %s", jerr, out)
	}
	if len(got.Artifacts) != 1 {
		t.Fatalf("expected exactly one artifact, got %d: %+v", len(got.Artifacts), got.Artifacts)
	}
	if len(got.Artifacts[0].Problems) != 2 {
		t.Errorf("expected 2 problems (one per corrupted layer), got %d: %+v", len(got.Artifacts[0].Problems), got.Artifacts[0].Problems)
	}

	tableOpts := &flags.InfoOpts{
		StoreRootOpts: defaultRootOpts(s.Root),
		OutputFormat:  "table",
		TypeFilter:    "all",
		Check:         true,
	}

	stdout, err := captureStdout(t, func() error { return InfoCmd(ctx, tableOpts, s) })
	if err != nil {
		t.Fatalf("InfoCmd (table): %v\nstdout: %s", err, stdout)
	}
	if n := strings.Count(stdout, "digest mismatch"); n != 2 {
		t.Errorf("expected \"digest mismatch\" to appear twice (one row per problem), got %d occurrences in: %s", n, stdout)
	}
}

// maxLineWidth is a regression trip-wire for any single rendered line of the
// failure table. It is not a strict terminal-width budget: the full row also
// carries Reference/Type/Platform/Digest columns, table border characters, and
// the footer label, all of which vary independently of the Issue column. It is
// set comfortably above the width produced by a correctly-bounded Issue column
// (observed ~125 chars for this test's fixture data) and comfortably below the
// width produced by an unbounded Issue column (observed 471-585 chars before
// this fix), so it fails only when the Issue column regresses to unbounded.
const maxLineWidth = 200

// TestBuildFailureTable_LongIssueIsWrapped guards against the Issue column (built
// from issueText, which can embed an arbitrary filesystem or JSON-decode error)
// blowing out the whole table's layout. It covers both a normal-length issue and
// the realistic worst case: a single unbroken ~180-character token with no spaces
// (e.g. a long filesystem path embedded in an "unreadable: ..." error), which a
// naive *word*-wrapping configuration would fail to wrap at all.
func TestBuildFailureTable_LongIssueIsWrapped(t *testing.T) {
	longPath := "/some/very/long/path/with/no/spaces/that/keeps/going/and/going/and/going/and/going/and/going/and/going/and/going/blob.json"
	if len(longPath) < 100 {
		t.Fatalf("test setup: longPath too short for a meaningful regression check: %d chars", len(longPath))
	}

	items := []item{
		{
			Reference: "repo/normal:v1",
			Type:      "image",
			Platform:  "linux/amd64",
			blobProblems: []store.BlobResult{
				{Digest: "sha256:" + strings.Repeat("a", 64), Status: store.BlobDigestMismatch},
			},
		},
		{
			Reference: "repo/unreadable:v1",
			Type:      "image",
			Platform:  "linux/amd64",
			blobProblems: []store.BlobResult{
				{
					Digest: "sha256:" + strings.Repeat("b", 64),
					Status: store.BlobUnreadable,
					Detail: longPath + ": no such file or directory",
				},
			},
		},
	}

	out, err := captureStdout(t, func() error {
		return buildFailureTable("/store/root", "store-id-123", items...)
	})
	if err != nil {
		t.Fatalf("buildFailureTable: %v", err)
	}

	if !strings.Contains(out, "digest mismatch") {
		t.Errorf("expected normal-length issue %q to appear in output:\n%s", "digest mismatch", out)
	}

	for i, line := range strings.Split(out, "\n") {
		if n := len([]rune(line)); n > maxLineWidth {
			t.Errorf("line %d exceeds max width %d (got %d): %q\nfull output:\n%s", i, maxLineWidth, n, line, out)
		}
	}
}
