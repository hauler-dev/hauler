package repodata

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newDebFixture builds a minimal, real ar(1)-format .deb file with the given control stanza fields, in order, and returns its bytes.
func newDebFixture(t *testing.T, order []string, fields map[string]string) []byte {
	t.Helper()

	var control strings.Builder
	for _, key := range order {
		fmt.Fprintf(&control, "%s: %s\n", key, fields[key])
	}

	controlTarGz := newTarGz(t, map[string]string{"./control": control.String()})
	dataTarGz := newTarGz(t, map[string]string{"./usr/share/doc/pkg/README": "hello\n"})

	var buf bytes.Buffer
	buf.WriteString("!<arch>\n")
	writeArEntry(t, &buf, "debian-binary", []byte("2.0\n"))
	writeArEntry(t, &buf, "control.tar.gz", controlTarGz)
	writeArEntry(t, &buf, "data.tar.gz", dataTarGz)

	return buf.Bytes()
}

func writeArEntry(t *testing.T, buf *bytes.Buffer, name string, data []byte) {
	t.Helper()

	header := fmt.Sprintf("%-16s%-12d%-6d%-6d%-8s%-10d`\n", name, 0, 0, 0, "100644", len(data))
	if len(header) != 60 {
		t.Fatalf("ar header for %s is %d bytes, want 60", name, len(header))
	}
	buf.WriteString(header)
	buf.Write(data)
	if len(data)%2 != 0 {
		buf.WriteByte('\n')
	}
}

func newTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)

	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("failed to write tar header for %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write tar content for %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("failed to close tar writer: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("failed to close gzip writer: %v", err)
	}

	return buf.Bytes()
}

func TestGenerateDebRepo(t *testing.T) {
	dir := t.TempDir()

	order := []string{"Package", "Version", "Architecture", "Maintainer", "Description"}
	fields := map[string]string{
		"Package":      "hello",
		"Version":      "1.0-1",
		"Architecture": "amd64",
		"Maintainer":   "Test <test@example.com>",
		"Description":  "a test package",
	}
	debBytes := newDebFixture(t, order, fields)
	if err := os.WriteFile(filepath.Join(dir, "hello_1.0-1_amd64.deb"), debBytes, 0o644); err != nil {
		t.Fatalf("failed to write deb fixture: %v", err)
	}

	if err := GenerateDebRepo(dir); err != nil {
		t.Fatalf("GenerateDebRepo failed: %v", err)
	}

	packagesPath := filepath.Join(dir, "dists", "stable", "main", "binary-amd64", "Packages")
	packagesBytes, err := os.ReadFile(packagesPath)
	if err != nil {
		t.Fatalf("failed to read Packages: %v", err)
	}
	packages := string(packagesBytes)
	if !containsAll(packages,
		"Package: hello",
		"Version: 1.0-1",
		"Architecture: amd64",
		"Filename: hello_1.0-1_amd64.deb",
		"SHA256:",
		"MD5sum:",
	) {
		t.Errorf("Packages missing expected content: %s", packages)
	}

	releasePath := filepath.Join(dir, "dists", "stable", "Release")
	releaseBytes, err := os.ReadFile(releasePath)
	if err != nil {
		t.Fatalf("failed to read Release: %v", err)
	}
	release := string(releaseBytes)
	if !containsAll(release, "Suite: stable", "Architectures: amd64", "Components: main") {
		t.Errorf("Release missing expected content: %s", release)
	}

	// Release's checksum paths must be relative to its own directory (dists/stable/), not to the repo root, or a real apt client double-joins "dists/stable/" onto them and 404s.
	if !strings.Contains(release, " main/binary-amd64/Packages\n") {
		t.Errorf("Release entry for Packages is not relative to dists/stable/: %s", release)
	}
	if strings.Contains(release, "dists/stable/main/binary-amd64/Packages") {
		t.Errorf("Release entry is relative to the repo root instead of dists/stable/: %s", release)
	}
}

func TestGenerateDebRepo_NoDebFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to write notes.txt: %v", err)
	}

	if err := GenerateDebRepo(dir); err != nil {
		t.Fatalf("GenerateDebRepo failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "dists")); !os.IsNotExist(err) {
		t.Error("expected no dists directory when no .deb files are present")
	}
}
