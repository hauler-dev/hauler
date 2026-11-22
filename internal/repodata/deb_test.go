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

	if _, err := GenerateDebRepo(t.Context(), dir, DefaultDebSuite, "."); err != nil {
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
	if !containsAll(release, "Suite: stable", "Architectures: amd64", "Components: main", "\nMD5Sum:\n", "\nSHA1:\n", "\nSHA256:\n") {
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

func TestGenerateDebRepo_MultilineDescription(t *testing.T) {
	dir := t.TempDir()

	order := []string{"Package", "Version", "Architecture", "Maintainer", "Description"}
	fields := map[string]string{
		"Package":      "hello",
		"Version":      "1.0-1",
		"Architecture": "amd64",
		"Maintainer":   "Test <test@example.com>",
		"Description":  "a test package\n first paragraph\n .\n second paragraph",
	}
	if err := os.WriteFile(filepath.Join(dir, "hello_1.0-1_amd64.deb"), newDebFixture(t, order, fields), 0o644); err != nil {
		t.Fatalf("failed to write deb fixture: %v", err)
	}

	if _, err := GenerateDebRepo(t.Context(), dir, DefaultDebSuite, "."); err != nil {
		t.Fatalf("GenerateDebRepo failed: %v", err)
	}

	packagesBytes, err := os.ReadFile(filepath.Join(dir, "dists", "stable", "main", "binary-amd64", "Packages"))
	if err != nil {
		t.Fatalf("failed to read Packages: %v", err)
	}
	packages := string(packagesBytes)

	want := "Description: a test package\n first paragraph\n .\n second paragraph\nFilename: hello_1.0-1_amd64.deb\n"
	if !strings.Contains(packages, want) {
		t.Errorf("Packages does not keep the description folded inside the stanza:\n%s", packages)
	}
	// a blank line inside the stanza would split it and leave Filename in a stanza with no Package
	if strings.Count(strings.TrimRight(packages, "\n"), "\n\n") != 0 {
		t.Errorf("Packages has a blank line inside the only stanza:\n%s", packages)
	}
}

func TestGenerateDebRepo_ArchAll(t *testing.T) {
	dir := t.TempDir()

	order := []string{"Package", "Version", "Architecture", "Maintainer", "Description"}
	for _, pkg := range []struct{ name, arch string }{{"hello", "amd64"}, {"world", "arm64"}, {"docs", "all"}} {
		fields := map[string]string{
			"Package":      pkg.name,
			"Version":      "1.0-1",
			"Architecture": pkg.arch,
			"Maintainer":   "Test <test@example.com>",
			"Description":  "a test package",
		}
		name := pkg.name + "_1.0-1_" + pkg.arch + ".deb"
		if err := os.WriteFile(filepath.Join(dir, name), newDebFixture(t, order, fields), 0o644); err != nil {
			t.Fatalf("failed to write deb fixture: %v", err)
		}
	}

	if _, err := GenerateDebRepo(t.Context(), dir, DefaultDebSuite, "."); err != nil {
		t.Fatalf("GenerateDebRepo failed: %v", err)
	}

	for arch, want := range map[string][]string{
		"amd64": {"Package: hello", "Package: docs"},
		"arm64": {"Package: world", "Package: docs"},
		"all":   {"Package: docs"},
	} {
		b, err := os.ReadFile(filepath.Join(dir, "dists", "stable", "main", "binary-"+arch, "Packages"))
		if err != nil {
			t.Fatalf("failed to read binary-%s Packages: %v", arch, err)
		}
		if !containsAll(string(b), want...) {
			t.Errorf("binary-%s Packages missing %v:\n%s", arch, want, b)
		}
	}
	amd64, _ := os.ReadFile(filepath.Join(dir, "dists", "stable", "main", "binary-amd64", "Packages"))
	if strings.Contains(string(amd64), "Package: world") {
		t.Errorf("binary-amd64 Packages lists an arm64 package:\n%s", amd64)
	}
}

func TestGenerateDebRepo_NoDebFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to write notes.txt: %v", err)
	}

	if _, err := GenerateDebRepo(t.Context(), dir, DefaultDebSuite, "."); err != nil {
		t.Fatalf("GenerateDebRepo failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "dists")); !os.IsNotExist(err) {
		t.Error("expected no dists directory when no .deb files are present")
	}
}

// TestGenerateDebRepo_Suite verifies a folder's debs become their own suite with Filename relative to the repo root, and that one suite can index several folders.
func TestGenerateDebRepo_Suite(t *testing.T) {
	dir := t.TempDir()

	order := []string{"Package", "Version", "Architecture", "Maintainer", "Description"}
	for _, rel := range []string{"bookworm/hello_1.0-1_amd64.deb", "hello_2.0-1_amd64.deb", "stable/world_1.0-1_amd64.deb"} {
		fields := map[string]string{"Package": "hello", "Version": "1.0-1", "Architecture": "amd64", "Maintainer": "Test <test@example.com>", "Description": "a test package"}
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o755); err != nil {
			t.Fatalf("failed to create folder: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, rel), newDebFixture(t, order, fields), 0o644); err != nil {
			t.Fatalf("failed to write deb fixture: %v", err)
		}
	}

	if count, err := GenerateDebRepo(t.Context(), dir, "bookworm", "bookworm"); err != nil || count != 1 {
		t.Fatalf("GenerateDebRepo(bookworm) = %d, %v... want 1 package", count, err)
	}
	if count, err := GenerateDebRepo(t.Context(), dir, DefaultDebSuite, ".", "stable"); err != nil || count != 2 {
		t.Fatalf("GenerateDebRepo(stable) = %d, %v... want 2 packages", count, err)
	}

	bookworm, err := os.ReadFile(filepath.Join(dir, "dists", "bookworm", "main", "binary-amd64", "Packages"))
	if err != nil {
		t.Fatalf("failed to read bookworm Packages: %v", err)
	}
	if !strings.Contains(string(bookworm), "Filename: bookworm/hello_1.0-1_amd64.deb\n") || strings.Contains(string(bookworm), "hello_2.0-1") {
		t.Errorf("bookworm Packages should list only bookworm/ with its folder in Filename:\n%s", bookworm)
	}
	release, err := os.ReadFile(filepath.Join(dir, "dists", "bookworm", "Release"))
	if err != nil || !containsAll(string(release), "Suite: bookworm\n", "Codename: bookworm\n") {
		t.Errorf("bookworm Release missing its suite (%v):\n%s", err, release)
	}

	stable, err := os.ReadFile(filepath.Join(dir, "dists", "stable", "main", "binary-amd64", "Packages"))
	if err != nil || !containsAll(string(stable), "Filename: hello_2.0-1_amd64.deb\n", "Filename: stable/world_1.0-1_amd64.deb\n") || strings.Contains(string(stable), "bookworm/") {
		t.Errorf("stable Packages should list the top level deb and stable/ only (%v):\n%s", err, stable)
	}
}

// TestGenerateDebRepo_SkipsUnreadable verifies a file that isn't a real deb is left out with a warning instead of failing the whole repo.
func TestGenerateDebRepo_SkipsUnreadable(t *testing.T) {
	dir := t.TempDir()
	order := []string{"Package", "Version", "Architecture", "Maintainer", "Description"}
	fields := map[string]string{"Package": "hello", "Version": "1.0-1", "Architecture": "amd64", "Maintainer": "Test <test@example.com>", "Description": "a test package"}
	if err := os.WriteFile(filepath.Join(dir, "hello_1.0-1_amd64.deb"), newDebFixture(t, order, fields), 0o644); err != nil {
		t.Fatalf("failed to write deb fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.deb"), []byte("not a package"), 0o644); err != nil {
		t.Fatalf("failed to write broken.deb: %v", err)
	}

	if count, err := GenerateDebRepo(t.Context(), dir, DefaultDebSuite, "."); err != nil || count != 1 {
		t.Fatalf("GenerateDebRepo = %d, %v... want 1 package and no error", count, err)
	}
	packages, err := os.ReadFile(filepath.Join(dir, "dists", "stable", "main", "binary-amd64", "Packages"))
	if err != nil || !strings.Contains(string(packages), "Package: hello") || strings.Contains(string(packages), "broken.deb") {
		t.Errorf("Packages should list hello and skip broken.deb (%v):\n%s", err, packages)
	}
}
