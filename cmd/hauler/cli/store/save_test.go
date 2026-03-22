package store

// save_test.go covers writeExportsManifest and SaveCmd.
//
// IMPORTANT: SaveCmd calls os.Chdir(storeDir) and defers os.Chdir back. Do
// NOT call t.Parallel() on any SaveCmd test, and always use absolute paths for
// StoreDir and FileName so they remain valid after the chdir.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hauler.dev/go/hauler/internal/flags"
	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/pkg/archives"
	"hauler.dev/go/hauler/pkg/consts"
)

// manifestEntry mirrors tarball.Descriptor for asserting manifest.json contents.
type manifestEntry struct {
	Config   string   `json:"Config"`
	RepoTags []string `json:"RepoTags"`
	Layers   []string `json:"Layers"`
}

// readManifestJSON reads and unmarshals manifest.json from the given OCI layout dir.
func readManifestJSON(t *testing.T, dir string) []manifestEntry {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, consts.ImageManifestFile))
	if err != nil {
		t.Fatalf("readManifestJSON: %v", err)
	}
	var entries []manifestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("readManifestJSON unmarshal: %v", err)
	}
	return entries
}

// newSaveOpts builds a SaveOpts pointing at storeDir with an absolute archive path.
func newSaveOpts(storeDir, archivePath string) *flags.SaveOpts {
	return &flags.SaveOpts{
		StoreRootOpts: defaultRootOpts(storeDir),
		FileName:      archivePath,
	}
}

// --------------------------------------------------------------------------
// writeExportsManifest unit tests
// --------------------------------------------------------------------------

func TestWriteExportsManifest(t *testing.T) {
	ctx := newTestContext(t)

	t.Run("no platform filter includes all platforms", func(t *testing.T) {
		host, rOpts := newLocalhostRegistry(t)
		seedIndex(t, host, "test/multiarch", "v1", rOpts...)

		s := newTestStore(t)
		if err := s.AddImage(ctx, host+"/test/multiarch:v1", ""); err != nil {
			t.Fatalf("AddImage: %v", err)
		}

		if err := writeExportsManifest(ctx, s.Root, ""); err != nil {
			t.Fatalf("writeExportsManifest: %v", err)
		}

		entries := readManifestJSON(t, s.Root)
		if len(entries) < 2 {
			t.Errorf("expected >=2 entries (all platforms), got %d", len(entries))
		}
	})

	t.Run("linux/amd64 filter yields single entry", func(t *testing.T) {
		host, rOpts := newLocalhostRegistry(t)
		seedIndex(t, host, "test/multiarch", "v2", rOpts...)

		s := newTestStore(t)
		if err := s.AddImage(ctx, host+"/test/multiarch:v2", ""); err != nil {
			t.Fatalf("AddImage: %v", err)
		}

		if err := writeExportsManifest(ctx, s.Root, "linux/amd64"); err != nil {
			t.Fatalf("writeExportsManifest: %v", err)
		}

		entries := readManifestJSON(t, s.Root)
		if len(entries) != 1 {
			t.Errorf("expected 1 entry for linux/amd64, got %d", len(entries))
		}
	})

	t.Run("chart artifact excluded via config media type check", func(t *testing.T) {
		s := newTestStore(t)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()

		o := newAddChartOpts(chartTestdataDir, "")
		if err := AddChartCmd(ctx, o, s, "rancher-cluster-templates-0.5.2.tgz", rso, ro); err != nil {
			t.Fatalf("AddChartCmd: %v", err)
		}

		if err := writeExportsManifest(ctx, s.Root, ""); err != nil {
			t.Fatalf("writeExportsManifest: %v", err)
		}

		entries := readManifestJSON(t, s.Root)
		if len(entries) != 0 {
			t.Errorf("expected 0 entries (chart excluded from manifest.json), got %d", len(entries))
		}
	})
}

func TestWriteExportsManifest_SkipsNonImages(t *testing.T) {
	ctx := newTestContext(t)

	url := seedFileInHTTPServer(t, "skip.sh", "#!/bin/sh\necho skip")
	s := newTestStore(t)
	if err := storeFile(ctx, s, v1.File{Path: url}); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	if err := writeExportsManifest(ctx, s.Root, ""); err != nil {
		t.Fatalf("writeExportsManifest: %v", err)
	}

	entries := readManifestJSON(t, s.Root)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for file-only store, got %d", len(entries))
	}
}

// --------------------------------------------------------------------------
// SaveCmd integration tests
// Do NOT use t.Parallel() — SaveCmd calls os.Chdir.
// --------------------------------------------------------------------------

func TestSaveCmd(t *testing.T) {
	ctx := newTestContext(t)
	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "test/save", "v1")

	s := newTestStore(t)
	if err := s.AddImage(ctx, host+"/test/save:v1", ""); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	// FileName must be absolute so it remains valid after SaveCmd's os.Chdir.
	archivePath := filepath.Join(t.TempDir(), "haul.tar.zst")
	o := newSaveOpts(s.Root, archivePath)

	if err := SaveCmd(ctx, o, defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("SaveCmd: %v", err)
	}

	fi, err := os.Stat(archivePath)
	if err != nil {
		t.Fatalf("archive stat: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatal("archive is empty")
	}

	// Validate it is a well-formed zst archive by unarchiving it.
	destDir := t.TempDir()
	if err := archives.Unarchive(ctx, archivePath, destDir); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
}

func TestSaveCmd_ContainerdCompatibility(t *testing.T) {
	ctx := newTestContext(t)
	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "test/containerd-compat", "v1")

	s := newTestStore(t)
	if err := s.AddImage(ctx, host+"/test/containerd-compat:v1", ""); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "haul-compat.tar.zst")
	o := newSaveOpts(s.Root, archivePath)
	o.ContainerdCompatibility = true

	if err := SaveCmd(ctx, o, defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("SaveCmd ContainerdCompatibility: %v", err)
	}

	destDir := t.TempDir()
	if err := archives.Unarchive(ctx, archivePath, destDir); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}

	// oci-layout must be absent from the extracted archive.
	ociLayoutPath := filepath.Join(destDir, "oci-layout")
	if _, err := os.Stat(ociLayoutPath); !os.IsNotExist(err) {
		t.Errorf("expected oci-layout to be absent in containerd-compatible archive, got: %v", err)
	}
}

func TestSaveCmd_EmptyStore(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	// SaveCmd uses layout.FromPath which stats index.json — it must exist on
	// disk. A fresh store holds the index only in memory; SaveIndex flushes it.
	if err := s.SaveIndex(); err != nil {
		t.Fatalf("SaveIndex: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "haul-empty.tar.zst")
	o := newSaveOpts(s.Root, archivePath)

	if err := SaveCmd(ctx, o, defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("SaveCmd empty store: %v", err)
	}

	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("archive not created for empty store: %v", err)
	}
}

// --------------------------------------------------------------------------
// parseChunkSize unit tests
// --------------------------------------------------------------------------

func TestParseChunkSize(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{name: "kilobytes", input: "1K", want: 1 << 10},
		{name: "kilobytes long", input: "1KB", want: 1 << 10},
		{name: "megabytes", input: "500M", want: 500 << 20},
		{name: "megabytes long", input: "500MB", want: 500 << 20},
		{name: "gigabytes", input: "2G", want: 2 << 30},
		{name: "gigabytes long", input: "2GB", want: 2 << 30},
		{name: "terabytes", input: "1T", want: 1 << 40},
		{name: "terabytes long", input: "1TB", want: 1 << 40},
		{name: "plain bytes", input: "1024", want: 1024},
		{name: "lowercase", input: "1g", want: 1 << 30},
		{name: "whitespace trimmed", input: " 1G ", want: 1 << 30},
		{name: "zero is invalid", input: "0", wantErr: true},
		{name: "zero with suffix", input: "0M", wantErr: true},
		{name: "negative bytes", input: "-1", wantErr: true},
		{name: "negative with suffix", input: "-1G", wantErr: true},
		{name: "empty string", input: "", wantErr: true},
		{name: "invalid suffix", input: "1X", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseChunkSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseChunkSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseChunkSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// SaveCmd chunk-size integration tests
// Do NOT use t.Parallel() — SaveCmd calls os.Chdir.
// --------------------------------------------------------------------------

func TestSaveCmd_ChunkSize(t *testing.T) {
	ctx := newTestContext(t)
	host, _ := newLocalhostRegistry(t)
	seedImage(t, host, "test/chunksave", "v1")

	s := newTestStore(t)
	if err := s.AddImage(ctx, host+"/test/chunksave:v1", ""); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	archiveDir := t.TempDir()
	archivePath := filepath.Join(archiveDir, "haul-chunked.tar.zst")
	o := newSaveOpts(s.Root, archivePath)
	o.ChunkSize = "1K"

	if err := SaveCmd(ctx, o, defaultRootOpts(s.Root), defaultCliOpts()); err != nil {
		t.Fatalf("SaveCmd with chunk-size: %v", err)
	}

	// original archive must be replaced by chunk files
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Error("original archive should be removed after chunking")
	}

	// at least one chunk must exist
	matches, err := filepath.Glob(filepath.Join(archiveDir, "haul-chunked_*.tar.zst"))
	if err != nil {
		t.Fatalf("glob chunks: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected at least one chunk file, found none")
	}
}

func TestSaveCmd_ChunkSize_Invalid(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)
	if err := s.SaveIndex(); err != nil {
		t.Fatalf("SaveIndex: %v", err)
	}

	o := newSaveOpts(s.Root, filepath.Join(t.TempDir(), "haul.tar.zst"))
	o.ChunkSize = "0"

	if err := SaveCmd(ctx, o, defaultRootOpts(s.Root), defaultCliOpts()); err == nil {
		t.Fatal("SaveCmd: expected error for chunk-size=0, got nil")
	}
}
