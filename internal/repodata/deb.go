package repodata

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	debpkg "pault.ag/go/debian/deb"
)

const (
	debSuite     = "stable"
	debComponent = "main"
)

// GenerateDebRepo scans dir for top-level .deb files and writes the dists/<suite>/<component>/binary-<arch>/ tree apt needs to consume dir as a repo.
func GenerateDebRepo(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	byArch := map[string][]debStanza{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".deb") {
			continue
		}
		stanza, err := buildDebStanza(filepath.Join(dir, e.Name()), e.Name())
		if err != nil {
			return fmt.Errorf("reading deb package [%s]: %w", e.Name(), err)
		}
		byArch[stanza.arch] = append(byArch[stanza.arch], stanza)
	}
	if len(byArch) == 0 {
		return nil
	}

	distDir := filepath.Join(dir, "dists", debSuite)
	archs := make([]string, 0, len(byArch))
	type releaseEntry struct {
		path string
		sum  checksums
	}
	var releaseEntries []releaseEntry

	for arch, stanzas := range byArch {
		archs = append(archs, arch)
		sort.Slice(stanzas, func(i, j int) bool { return stanzas[i].filename < stanzas[j].filename })

		binDir := filepath.Join(distDir, debComponent, "binary-"+arch)
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			return err
		}

		var sb strings.Builder
		for _, s := range stanzas {
			sb.WriteString(s.text)
			sb.WriteString("\n")
		}
		packages := sb.String()

		packagesPath := filepath.Join(binDir, "Packages")
		if err := os.WriteFile(packagesPath, []byte(packages), 0o644); err != nil {
			return err
		}
		// Release lives at distDir, so its checksum entries are relative to distDir, not dir.
		relPath, err := filepath.Rel(distDir, packagesPath)
		if err != nil {
			return err
		}
		sum, err := writeGzip(filepath.Join(binDir, "Packages.gz"), []byte(packages))
		if err != nil {
			return err
		}
		gzRelPath, err := filepath.Rel(distDir, filepath.Join(binDir, "Packages.gz"))
		if err != nil {
			return err
		}

		rawChecksum, rawSize, err := sha256File(packagesPath)
		if err != nil {
			return err
		}
		releaseEntries = append(releaseEntries,
			releaseEntry{path: relPath, sum: checksums{rawChecksum: rawChecksum, rawSize: rawSize}},
			releaseEntry{path: gzRelPath, sum: checksums{rawChecksum: sum.gzChecksum, rawSize: sum.gzSize}},
		)
	}
	sort.Strings(archs)

	var rel strings.Builder
	fmt.Fprintf(&rel, "Origin: hauler\n")
	fmt.Fprintf(&rel, "Label: hauler\n")
	fmt.Fprintf(&rel, "Suite: %s\n", debSuite)
	fmt.Fprintf(&rel, "Codename: %s\n", debSuite)
	fmt.Fprintf(&rel, "Architectures: %s\n", strings.Join(archs, " "))
	fmt.Fprintf(&rel, "Components: %s\n", debComponent)
	fmt.Fprintf(&rel, "Date: %s\n", time.Now().UTC().Format(time.RFC1123))
	fmt.Fprintf(&rel, "SHA256:\n")
	sort.Slice(releaseEntries, func(i, j int) bool { return releaseEntries[i].path < releaseEntries[j].path })
	for _, e := range releaseEntries {
		fmt.Fprintf(&rel, " %s %d %s\n", e.sum.rawChecksum, e.sum.rawSize, filepath.ToSlash(e.path))
	}

	return os.WriteFile(filepath.Join(distDir, "Release"), []byte(rel.String()), 0o644)
}

// debStanza holds one Packages-file entry along with the architecture it was built for, so a package's control file is only opened and parsed once.
type debStanza struct {
	arch     string
	filename string
	text     string
}

// buildDebStanza reproduces filename's embedded control stanza (in its original field order) with the Filename/Size/MD5sum/SHA1/SHA256 fields real apt repos add appended.
func buildDebStanza(path, filename string) (debStanza, error) {
	d, closer, err := debpkg.LoadFile(path)
	if err != nil {
		return debStanza{}, err
	}
	defer closer()

	md5sum, sha1sum, sha256sum, size, err := debHashes(path)
	if err != nil {
		return debStanza{}, err
	}

	var sb strings.Builder
	for _, key := range d.Control.Paragraph.Order {
		fmt.Fprintf(&sb, "%s: %s\n", key, d.Control.Paragraph.Values[key])
	}
	fmt.Fprintf(&sb, "Filename: %s\n", filename)
	fmt.Fprintf(&sb, "Size: %d\n", size)
	fmt.Fprintf(&sb, "MD5sum: %s\n", md5sum)
	fmt.Fprintf(&sb, "SHA1: %s\n", sha1sum)
	fmt.Fprintf(&sb, "SHA256: %s\n", sha256sum)

	return debStanza{arch: d.Control.Architecture.String(), filename: filename, text: sb.String()}, nil
}

func debHashes(path string) (md5sum, sha1sum, sha256sum string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", "", 0, err
	}
	defer f.Close()

	mh := md5.New()
	sh1 := sha1.New()
	sh256 := sha256.New()

	n, err := io.Copy(io.MultiWriter(mh, sh1, sh256), f)
	if err != nil {
		return "", "", "", 0, err
	}

	return hex.EncodeToString(mh.Sum(nil)), hex.EncodeToString(sh1.Sum(nil)), hex.EncodeToString(sh256.Sum(nil)), n, nil
}
