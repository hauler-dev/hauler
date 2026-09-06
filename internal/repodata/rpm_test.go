package repodata

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rpmStringType = 6

// newRPMFixture builds a minimal, real RPM file with just the four tags GetNEVRA requires (name/version/release/arch) and returns its bytes.
func newRPMFixture(t *testing.T, name, version, release, arch string) []byte {
	t.Helper()

	var buf bytes.Buffer

	// Lead: magic, major/minor, type, archnum, 66-byte name, osnum, signature_type, 16 bytes reserved.
	buf.Write([]byte{0xED, 0xAB, 0xEE, 0xDB})
	buf.Write([]byte{3, 0})
	binary.Write(&buf, binary.BigEndian, uint16(0))
	binary.Write(&buf, binary.BigEndian, uint16(1))
	nameField := make([]byte, 66)
	copy(nameField, name+"-"+version+"-"+release)
	buf.Write(nameField)
	binary.Write(&buf, binary.BigEndian, uint16(1))
	binary.Write(&buf, binary.BigEndian, uint16(5))
	buf.Write(make([]byte, 16))

	// Empty signature header: no digest tag means ReadHeader skips hash verification entirely.
	writeRPMHeaderIntro(&buf, 0, 0)

	// General header: the four tags GetNEVRA requires, in the order it reads them.
	tags := []struct {
		tag   int32
		value string
	}{
		{1000, name},    // NAME
		{1001, version}, // VERSION
		{1002, release}, // RELEASE
		{1022, arch},    // ARCH
	}

	var data bytes.Buffer
	type tagEntry struct {
		tag, offset int32
	}
	var entries []tagEntry
	for _, tg := range tags {
		entries = append(entries, tagEntry{tag: tg.tag, offset: int32(data.Len())})
		data.WriteString(tg.value)
		data.WriteByte(0)
	}

	writeRPMHeaderIntro(&buf, len(entries), data.Len())
	for _, e := range entries {
		binary.Write(&buf, binary.BigEndian, e.tag)
		binary.Write(&buf, binary.BigEndian, int32(rpmStringType))
		binary.Write(&buf, binary.BigEndian, e.offset)
		binary.Write(&buf, binary.BigEndian, int32(1))
	}
	buf.Write(data.Bytes())

	return buf.Bytes()
}

// writeRPMHeaderIntro writes an RPM header's 16-byte intro: magic, reserved, entry count, and data blob size.
func writeRPMHeaderIntro(buf *bytes.Buffer, entries, dataSize int) {
	binary.Write(buf, binary.BigEndian, uint32(0x8eade801))
	binary.Write(buf, binary.BigEndian, uint32(0))
	binary.Write(buf, binary.BigEndian, uint32(entries))
	binary.Write(buf, binary.BigEndian, uint32(dataSize))
}

func TestGenerateRPMRepo(t *testing.T) {
	dir := t.TempDir()
	rpmBytes := newRPMFixture(t, "simple", "1.0.1", "1", "i386")
	if err := os.WriteFile(filepath.Join(dir, "simple-1.0.1-1.i386.rpm"), rpmBytes, 0o644); err != nil {
		t.Fatalf("failed to write rpm fixture: %v", err)
	}

	if err := GenerateRPMRepo(dir); err != nil {
		t.Fatalf("GenerateRPMRepo failed: %v", err)
	}

	repomdPath := filepath.Join(dir, "repodata", "repomd.xml")
	repomdBytes, err := os.ReadFile(repomdPath)
	if err != nil {
		t.Fatalf("failed to read repomd.xml: %v", err)
	}
	if !containsAll(string(repomdBytes), "repodata/primary.xml.gz", "<checksum>", "<open-checksum>") {
		t.Errorf("repomd.xml missing expected content: %s", repomdBytes)
	}

	primaryGzPath := filepath.Join(dir, "repodata", "primary.xml.gz")
	f, err := os.Open(primaryGzPath)
	if err != nil {
		t.Fatalf("failed to open primary.xml.gz: %v", err)
	}
	defer f.Close()

	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("primary.xml.gz is not valid gzip: %v", err)
	}
	defer zr.Close()

	primaryBytes, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("failed to decompress primary.xml.gz: %v", err)
	}
	primary := string(primaryBytes)

	if !containsAll(primary, "<name>simple</name>", "<arch>i386</arch>", `ver="1.0.1"`, `rel="1"`, "simple-1.0.1-1.i386.rpm") {
		t.Errorf("primary.xml missing expected package metadata: %s", primary)
	}
}

func TestGenerateRPMRepo_NoRPMFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to write notes.txt: %v", err)
	}

	if err := GenerateRPMRepo(dir); err != nil {
		t.Fatalf("GenerateRPMRepo failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "repodata")); !os.IsNotExist(err) {
		t.Error("expected no repodata directory when no .rpm files are present")
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
