package repodata

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	debpkg "pault.ag/go/debian/deb"

	"hauler.dev/go/hauler/v2/pkg/log"
)

// DefaultDebSuite is the apt suite for debs whose name has no folder.
const DefaultDebSuite = "stable"

const debComponent = "main"

// GenerateDebRepo indexes the top-level .deb files in each of pkgDirs (slash separated and relative to repoDir) as suite, writing the dists/<suite>/<component>/binary-<arch>/ tree apt reads, and returns how many packages it indexed... a file it can't read is skipped with a warning.
func GenerateDebRepo(ctx context.Context, repoDir, suite string, pkgDirs ...string) (int, error) {
	byArch := map[string][]debStanza{}
	count := 0
	for _, pkgDir := range pkgDirs {
		entries, err := os.ReadDir(filepath.Join(repoDir, filepath.FromSlash(pkgDir)))
		if err != nil {
			return 0, err
		}
		for _, e := range entries {
			if e.IsDir() || PackageType(e.Name()) != "deb" {
				continue
			}
			// Filename is relative to repoDir, which is the url apt is pointed at
			filename := path.Join(pkgDir, e.Name())
			stanza, err := buildDebStanza(filepath.Join(repoDir, filepath.FromSlash(filename)), filename)
			if err != nil {
				log.FromContext(ctx).Warnf("failed to read deb package [%s]: %v... skipping", filename, err)
				continue
			}
			byArch[stanza.arch] = append(byArch[stanza.arch], stanza)
			count++
		}
	}
	if count == 0 {
		return 0, nil
	}

	distDir := filepath.Join(repoDir, "dists", filepath.FromSlash(suite))
	archs := make([]string, 0, len(byArch))
	type releaseEntry struct {
		path                 string
		md5, sha1, sha256sum string
		size                 int64
	}
	var releaseEntries []releaseEntry

	for arch, stanzas := range byArch {
		archs = append(archs, arch)
		// apt only reads binary-all when Release lists "all", so also list arch independent packages under every concrete arch like reprepro and aptly do
		if arch != "all" {
			stanzas = append(append([]debStanza{}, stanzas...), byArch["all"]...)
		}
		sort.Slice(stanzas, func(i, j int) bool { return stanzas[i].filename < stanzas[j].filename })

		binDir := filepath.Join(distDir, debComponent, "binary-"+arch)
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			return 0, err
		}

		var sb strings.Builder
		for _, s := range stanzas {
			sb.WriteString(s.text)
			sb.WriteString("\n")
		}
		packages := sb.String()

		packagesPath := filepath.Join(binDir, "Packages")
		if err := os.WriteFile(packagesPath, []byte(packages), 0o644); err != nil {
			return 0, err
		}
		// Release lives at distDir, so its checksum entries are relative to distDir, not repoDir.
		relPath, err := filepath.Rel(distDir, packagesPath)
		if err != nil {
			return 0, err
		}
		if _, err := writeGzip(filepath.Join(binDir, "Packages.gz"), []byte(packages)); err != nil {
			return 0, err
		}
		gzRelPath, err := filepath.Rel(distDir, filepath.Join(binDir, "Packages.gz"))
		if err != nil {
			return 0, err
		}

		for _, p := range []string{relPath, gzRelPath} {
			md5sum, sha1sum, sha256sum, size, err := fileHashes(filepath.Join(distDir, p))
			if err != nil {
				return 0, err
			}
			releaseEntries = append(releaseEntries, releaseEntry{path: p, md5: md5sum, sha1: sha1sum, sha256sum: sha256sum, size: size})
		}
	}
	sort.Strings(archs)

	var rel strings.Builder
	fmt.Fprintf(&rel, "Origin: hauler\n")
	fmt.Fprintf(&rel, "Label: hauler\n")
	fmt.Fprintf(&rel, "Suite: %s\n", suite)
	fmt.Fprintf(&rel, "Codename: %s\n", suite)
	fmt.Fprintf(&rel, "Architectures: %s\n", strings.Join(archs, " "))
	fmt.Fprintf(&rel, "Components: %s\n", debComponent)
	fmt.Fprintf(&rel, "Date: %s\n", time.Now().UTC().Format(time.RFC1123))
	sort.Slice(releaseEntries, func(i, j int) bool { return releaseEntries[i].path < releaseEntries[j].path })
	// reprepro and aptly write all three hash sections, and older apt and mirroring tools only read MD5Sum or SHA1
	for _, section := range []string{"MD5Sum", "SHA1", "SHA256"} {
		fmt.Fprintf(&rel, "%s:\n", section)
		for _, e := range releaseEntries {
			sum := map[string]string{"MD5Sum": e.md5, "SHA1": e.sha1, "SHA256": e.sha256sum}[section]
			fmt.Fprintf(&rel, " %s %d %s\n", sum, e.size, filepath.ToSlash(e.path))
		}
	}

	return count, os.WriteFile(filepath.Join(distDir, "Release"), []byte(rel.String()), 0o644)
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

	md5sum, sha1sum, sha256sum, size, err := fileHashes(path)
	if err != nil {
		return debStanza{}, err
	}

	var sb strings.Builder
	for _, key := range d.Control.Paragraph.Order {
		writeDebField(&sb, key, d.Control.Paragraph.Values[key])
	}
	fmt.Fprintf(&sb, "Filename: %s\n", filename)
	fmt.Fprintf(&sb, "Size: %d\n", size)
	fmt.Fprintf(&sb, "MD5sum: %s\n", md5sum)
	fmt.Fprintf(&sb, "SHA1: %s\n", sha1sum)
	fmt.Fprintf(&sb, "SHA256: %s\n", sha256sum)

	return debStanza{arch: d.Control.Architecture.String(), filename: filename, text: sb.String()}, nil
}

// writeDebField writes key and value in deb822 form, indenting continuation lines and writing blank ones as " ." since the parser strips both.
func writeDebField(sb *strings.Builder, key, value string) {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	fmt.Fprintf(sb, "%s: %s\n", key, lines[0])
	for _, line := range lines[1:] {
		if line == "" {
			line = "."
		}
		fmt.Fprintf(sb, " %s\n", line)
	}
}
