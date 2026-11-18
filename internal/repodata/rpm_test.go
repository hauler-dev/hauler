package repodata

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	rpmInt16Type       = 3
	rpmInt32Type       = 4
	rpmStringType      = 6
	rpmStringArrayType = 8
)

// rpmTag is an extra header tag for newRPMFixtureWithTags, holding a []string, []int32, or []uint16 value.
type rpmTag struct {
	tag   int32
	value any
}

// newRPMFixture builds a minimal, real RPM file with just the four tags GetNEVRA requires (name/version/release/arch) and returns its bytes.
func newRPMFixture(t *testing.T, name, version, release, arch string) []byte {
	t.Helper()
	return newRPMFixtureWithTags(t, name, version, release, arch, nil)
}

// newRPMFixtureWithTags builds the same minimal RPM file as newRPMFixture plus extra array tags (i.e. requires, files, changelogs).
func newRPMFixtureWithTags(t *testing.T, name, version, release, arch string, extra []rpmTag) []byte {
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
		tag, typ, offset, count int32
	}
	var entries []tagEntry
	for _, tg := range tags {
		entries = append(entries, tagEntry{tag: tg.tag, typ: rpmStringType, offset: int32(data.Len()), count: 1})
		data.WriteString(tg.value)
		data.WriteByte(0)
	}
	for _, tg := range extra {
		switch v := tg.value.(type) {
		case []string:
			entries = append(entries, tagEntry{tag: tg.tag, typ: rpmStringArrayType, offset: int32(data.Len()), count: int32(len(v))})
			for _, s := range v {
				data.WriteString(s)
				data.WriteByte(0)
			}
		case []int32:
			for data.Len()%4 != 0 {
				data.WriteByte(0)
			}
			entries = append(entries, tagEntry{tag: tg.tag, typ: rpmInt32Type, offset: int32(data.Len()), count: int32(len(v))})
			binary.Write(&data, binary.BigEndian, v)
		case []uint16:
			for data.Len()%2 != 0 {
				data.WriteByte(0)
			}
			entries = append(entries, tagEntry{tag: tg.tag, typ: rpmInt16Type, offset: int32(data.Len()), count: int32(len(v))})
			binary.Write(&data, binary.BigEndian, v)
		default:
			t.Fatalf("unsupported rpm tag value type %T", tg.value)
		}
	}

	writeRPMHeaderIntro(&buf, len(entries), data.Len())
	for _, e := range entries {
		binary.Write(&buf, binary.BigEndian, e.tag)
		binary.Write(&buf, binary.BigEndian, e.typ)
		binary.Write(&buf, binary.BigEndian, e.offset)
		binary.Write(&buf, binary.BigEndian, e.count)
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

	if count, err := GenerateRPMRepo(t.Context(), dir); err != nil || count != 1 {
		t.Fatalf("GenerateRPMRepo = %d, %v... want 1 package", count, err)
	}

	repomdPath := filepath.Join(dir, "repodata", "repomd.xml")
	repomdBytes, err := os.ReadFile(repomdPath)
	if err != nil {
		t.Fatalf("failed to read repomd.xml: %v", err)
	}
	if !containsAll(string(repomdBytes), `<data type="primary">`, `<data type="filelists">`, `<data type="other">`, `<checksum type="sha256">`, `<open-checksum type="sha256">`) {
		t.Errorf("repomd.xml missing expected content: %s", repomdBytes)
	}

	primary := readRepodata(t, dir, "primary")

	if !containsAll(primary, "<name>simple</name>", "<arch>i386</arch>", `ver="1.0.1"`, `rel="1"`, "simple-1.0.1-1.i386.rpm") {
		t.Errorf("primary.xml missing expected package metadata: %s", primary)
	}
}

func TestGenerateRPMRepo_NoRPMFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("failed to write notes.txt: %v", err)
	}

	if _, err := GenerateRPMRepo(t.Context(), dir); err != nil {
		t.Fatalf("GenerateRPMRepo failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "repodata")); !os.IsNotExist(err) {
		t.Error("expected no repodata directory when no .rpm files are present")
	}
}

// readRepodata finds kind's file through repomd.xml the way dnf does, checks its checksum, and returns it decompressed.
func readRepodata(t *testing.T, dir, kind string) string {
	t.Helper()

	repomdBytes, err := os.ReadFile(filepath.Join(dir, "repodata", "repomd.xml"))
	if err != nil {
		t.Fatalf("failed to read repomd.xml: %v", err)
	}
	var rmd repomd
	if err := xml.Unmarshal(repomdBytes, &rmd); err != nil {
		t.Fatalf("failed to parse repomd.xml: %v", err)
	}
	for _, d := range rmd.Data {
		if d.Type != kind {
			continue
		}
		gz, err := os.ReadFile(filepath.Join(dir, d.Location.Href))
		if err != nil {
			t.Fatalf("failed to read %s: %v", d.Location.Href, err)
		}
		if sum := sha256.Sum256(gz); hex.EncodeToString(sum[:]) != d.Checksum.Value {
			t.Fatalf("%s checksum does not match repomd.xml", d.Location.Href)
		}
		zr, err := gzip.NewReader(bytes.NewReader(gz))
		if err != nil {
			t.Fatalf("%s is not valid gzip: %v", d.Location.Href, err)
		}
		raw, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("failed to decompress %s: %v", d.Location.Href, err)
		}
		return string(raw)
	}
	t.Fatalf("repomd.xml has no %s entry", kind)
	return ""
}

func TestGenerateRPMRepo_Dependencies(t *testing.T) {
	dir := t.TempDir()
	extra := []rpmTag{
		{1047, []string{"deps", "deps(x86-64)"}}, // PROVIDENAME
		{1112, []int32{8, 8}},                    // PROVIDEFLAGS
		{1113, []string{"1.0-1", "1.0-1"}},       // PROVIDEVERSION
		{1049, []string{"rpmlib(CompressedFileNames)", "/bin/sh", "/usr/bin/deps", "libc", "libc", "deps(x86-64)", "/usr/share/doc/deps", "libc.so.6()(64bit)", "libc.so.6(GLIBC_2.34)(64bit)", "libc.so.6(GLIBC_2.17)(64bit)", "coreutils", "deps-extras"}}, // REQUIRENAME
		{1048, []int32{1<<24 | 8 | 2, 1 << 9, 0, 8 | 4, 8 | 4, 8, 0, 0, 0, 0, 1 << 7, 1 << 19}}, // REQUIREFLAGS
		{1050, []string{"3.0.4-1", "", "", "2.0", "2.0", "1.0-1", "", "", "", "", "", ""}},      // REQUIREVERSION
		{tagOldSuggestsName, []string{"old-strong", "old-weak"}},                                // OLDSUGGESTSNAME
		{tagOldSuggestsFlags, []int32{1 << 27, 0}},                                              // OLDSUGGESTSFLAGS
		{tagOldSuggestsVersion, []string{"", ""}},                                               // OLDSUGGESTSVERSION
		{tagOldEnhancesName, []string{"oldenh-strong", "oldenh-weak"}},                          // OLDENHANCESNAME
		{tagOldEnhancesFlags, []int32{1 << 27, 0}},                                              // OLDENHANCESFLAGS
		{tagOldEnhancesVersion, []string{"", ""}},                                               // OLDENHANCESVERSION
		{1054, []string{"olddeps"}},                                                             // CONFLICTNAME
		{1053, []int32{0}},                                                                      // CONFLICTFLAGS
		{1055, []string{""}},                                                                    // CONFLICTVERSION
		{1090, []string{"deps-legacy"}},                                                         // OBSOLETENAME
		{1114, []int32{2}},                                                                      // OBSOLETEFLAGS
		{1115, []string{"1.0"}},                                                                 // OBSOLETEVERSION
		{tagRecommendName, []string{"deps-docs"}},                                               // RECOMMENDNAME
		{tagRecommendFlags, []int32{0}},                                                         // RECOMMENDFLAGS
		{tagRecommendVersion, []string{""}},                                                     // RECOMMENDVERSION
		{1027, []string{"/etc/deps.conf", "/usr/bin/deps", "/usr/share/doc/deps", "/var/log/deps.log", "/run/deps"}}, // OLDFILENAMES
		{1030, []uint16{0o100644, 0o100755, 0o040755, 0o100644, 0o040755}},                                           // FILEMODES
		{1037, []int32{1, 0, 0, 1 << 6, 1 << 6}},                                                                     // FILEFLAGS
		{1080, []int32{1700000200, 1700000100, 1700000100}},                                                          // CHANGELOGTIME
		{1081, []string{"c <c@example.com> ", "b <b@example.com>", "a <a@example.com>"}},                             // CHANGELOGNAME
		{1082, []string{"- third", "- second", "- first"}},                                                           // CHANGELOGTEXT
	}
	if err := os.WriteFile(filepath.Join(dir, "deps-1.0-1.x86_64.rpm"), newRPMFixtureWithTags(t, "deps", "1.0", "1", "x86_64", extra), 0o644); err != nil {
		t.Fatalf("failed to write rpm fixture: %v", err)
	}

	if _, err := GenerateRPMRepo(t.Context(), dir); err != nil {
		t.Fatalf("GenerateRPMRepo failed: %v", err)
	}

	primary := readRepodata(t, dir, "primary")
	if !containsAll(primary,
		`<rpm:entry name="deps(x86-64)" flags="EQ" epoch="0" ver="1.0" rel="1"></rpm:entry>`,
		`<rpm:entry name="/bin/sh" pre="1"></rpm:entry>`,
		`<rpm:conflicts>`, `<rpm:entry name="olddeps"></rpm:entry>`,
		`<rpm:obsoletes>`, `<rpm:entry name="deps-legacy" flags="LT" epoch="0" ver="1.0"></rpm:entry>`,
		`<rpm:recommends>`, `<rpm:entry name="deps-docs"></rpm:entry>`,
		`<file>/etc/deps.conf</file>`, `<file>/usr/bin/deps</file>`,
	) {
		t.Errorf("primary.xml missing expected dependencies or files:\n%s", primary)
	}
	// createrepo_c drops rpmlib() requires (rpm satisfies them itself), requires on the package's own files, and duplicates, and keeps primary.xml to the /etc and bin/ files
	if strings.Contains(primary, "rpmlib(") || strings.Contains(primary, `name="/usr/bin/deps"`) || strings.Count(primary, `name="libc"`) != 1 {
		t.Errorf("primary.xml kept requires createrepo_c drops:\n%s", primary)
	}
	if strings.Contains(primary, "<file>/usr/share/doc/deps") || strings.Contains(primary, "/var/log/deps.log") {
		t.Errorf("primary.xml lists files outside /etc and bin/:\n%s", primary)
	}

	// createrepo_c also drops requires the package provides itself, keeps only the highest libc.so.6 requirement (listed last), keeps requires on its own non primary files, and marks pretrans requires as pre
	requires := xmlSection(primary, "rpm:requires")
	if strings.Contains(requires, "deps(x86-64)") || strings.Contains(requires, "GLIBC_2.17") || strings.Contains(requires, "libc.so.6()") ||
		!strings.HasSuffix(strings.TrimSpace(requires), `<rpm:entry name="libc.so.6(GLIBC_2.34)(64bit)"></rpm:entry>`) ||
		!containsAll(requires, `<rpm:entry name="/usr/share/doc/deps"></rpm:entry>`, `<rpm:entry name="coreutils" pre="1"></rpm:entry>`) {
		t.Errorf("rpm:requires not filtered like createrepo_c:\n%s", requires)
	}
	// legacy weak dependencies fold into the modern lists like createrepo_c... missingok requires and strong old suggests become recommends, strong old enhances become supplements
	if strings.Contains(requires, "deps-extras") || !containsAll(xmlSection(primary, "rpm:recommends"), `<rpm:entry name="deps-extras"></rpm:entry>`, `<rpm:entry name="old-strong"></rpm:entry>`) ||
		!strings.Contains(xmlSection(primary, "rpm:suggests"), `<rpm:entry name="old-weak"></rpm:entry>`) ||
		!strings.Contains(xmlSection(primary, "rpm:supplements"), `<rpm:entry name="oldenh-strong"></rpm:entry>`) ||
		!strings.Contains(xmlSection(primary, "rpm:enhances"), `<rpm:entry name="oldenh-weak"></rpm:entry>`) {
		t.Errorf("legacy weak dependencies not folded like createrepo_c:\n%s", primary)
	}

	filelists := readRepodata(t, dir, "filelists")
	if !containsAll(filelists, `name="deps" arch="x86_64"`, `<file>/etc/deps.conf</file>`, `<file type="dir">/usr/share/doc/deps</file>`, `<file type="ghost">/var/log/deps.log</file>`, `<file type="dir">/run/deps</file>`) {
		t.Errorf("filelists.xml missing expected files:\n%s", filelists)
	}

	other := readRepodata(t, dir, "other")
	first, third := strings.Index(other, "- first"), strings.Index(other, "- third")
	if first < 0 || third < 0 || first > third || !strings.Contains(other, `author="a &lt;a@example.com&gt;" date="1700000100"`) {
		t.Errorf("other.xml changelogs missing or not oldest first:\n%s", other)
	}
	// like createrepo_c, the newer entry sharing a timestamp moves up one second and trailing spaces leave the author
	if !containsAll(other, `author="b &lt;b@example.com&gt;" date="1700000101"`, `author="c &lt;c@example.com&gt;" date="1700000200"`) {
		t.Errorf("other.xml changelog dates or authors not adjusted like createrepo_c:\n%s", other)
	}
}

// xmlSection returns the text between the first <tag> and its closing tag in s, or "" when tag is missing.
func xmlSection(s, tag string) string {
	start := strings.Index(s, "<"+tag+">")
	if start < 0 {
		return ""
	}
	start += len(tag) + 2
	end := strings.Index(s[start:], "</"+tag+">")
	if end < 0 {
		return ""
	}
	return s[start : start+end]
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func TestCompareLibcDep(t *testing.T) {
	tests := []struct {
		dep1, dep2 string
		want       int
	}{
		{"libc.so.6(GLIBC_2.4)", "libc.so.6(GLIBC_2.4)", 0},
		{"libc.so.6()(64bit)", "libc.so.6(GLIBC_2.3.4)(64bit)", 2},
		{"libc.so.6(GLIBC_2.3.4)(64bit)", "libc.so.6(GLIBC_2.4)(64bit)", 2},
		{"libc.so.6(GLIBC_2.34)(64bit)", "libc.so.6(GLIBC_2.17)(64bit)", 1},
		{"libc.so.6", "libc.so.6()(64bit)", 2},
		{"libc.so.6()(64bit)", "libc.so.6", 1},
		{"libc.so.6", "libc.so.6", 0},
		{"libc.so.6(GLIBC", "libc.so.6(GLIBC", -1},
		{"libc.so.6(GLIBC_PRIVATE)", "libc.so.6(GLIBC_2.17)", 2},
	}
	for _, tt := range tests {
		if got := compareLibcDep(tt.dep1, tt.dep2); got != tt.want {
			t.Errorf("compareLibcDep(%q, %q) = %d, want %d", tt.dep1, tt.dep2, got, tt.want)
		}
	}
}

// TestGenerateRPMRepo_SkipsUnreadable verifies a file that isn't a real rpm is left out with a warning instead of failing the whole repo, like createrepo_c.
func TestGenerateRPMRepo_SkipsUnreadable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "simple-1.0.1-1.i386.rpm"), newRPMFixture(t, "simple", "1.0.1", "1", "i386"), 0o644); err != nil {
		t.Fatalf("failed to write rpm fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.rpm"), []byte("not a package"), 0o644); err != nil {
		t.Fatalf("failed to write broken.rpm: %v", err)
	}

	if count, err := GenerateRPMRepo(t.Context(), dir); err != nil || count != 1 {
		t.Fatalf("GenerateRPMRepo = %d, %v... want 1 package and no error", count, err)
	}
	primary := readRepodata(t, dir, "primary")
	if !strings.Contains(primary, "<name>simple</name>") || strings.Contains(primary, "broken.rpm") {
		t.Errorf("primary.xml should list simple and skip broken.rpm:\n%s", primary)
	}
}
