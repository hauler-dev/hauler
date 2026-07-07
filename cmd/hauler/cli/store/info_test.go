package store

import (
	"encoding/json"
	"os"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/internal/flags"
	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/consts"
)

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
	// newItem uses s only for its signature; it does not dereference s in practice.
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
			var empty item
			if tc.wantEmpty {
				if got != empty {
					t.Errorf("expected empty item, got %+v", got)
				}
				return
			}
			if got == empty {
				t.Fatalf("got empty item, want type %q", tc.wantType)
			}
			if got.Type != tc.wantType {
				t.Errorf("got type %q, want %q", got.Type, tc.wantType)
			}
		})
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
		// Store has only a file artifact; image filter returns no items (no error).
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
