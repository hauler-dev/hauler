package repodata

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// PackageType returns "rpm" or "deb" when name has that package extension, or "" for any other file, so store info, store copy, and serve fileserver classify packages the same way.
func PackageType(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".rpm"):
		return "rpm"
	case strings.HasSuffix(lower, ".deb"):
		return "deb"
	default:
		return ""
	}
}

// PackagePath returns where serve fileserver --generate-repodata moves the package named title (a slash separated path) and the repo it joins... rpms/<folder> for an rpm, or the apt suite named after its folder (DefaultDebSuite when it has none) for a deb.
func PackagePath(title string) (dest, repo string, err error) {
	if !filepath.IsLocal(filepath.FromSlash(title)) {
		return "", "", errors.New("path is not relative to the fileserver directory")
	}
	title = path.Clean(title)
	folder := path.Dir(title)
	segments := strings.Split(folder, "/")

	switch PackageType(title) {
	case "rpm":
		// each folder gets its own repodata/, so a folder by that name would collide with it
		if slices.Contains(segments, "repodata") {
			return "", "", errors.New("folder [repodata] is reserved for rpm metadata")
		}
		return path.Join("rpms", title), path.Join("rpms", folder), nil
	case "deb":
		// apt reads suites from debs/dists/, so packages can't live there
		if segments[0] == "dists" {
			return "", "", errors.New("folder [dists] is reserved for deb metadata")
		}
		suite := folder
		if folder == "." {
			suite = DefaultDebSuite
		}
		return path.Join("debs", title), suite, nil
	default:
		return "", "", errors.New("not an rpm or deb package")
	}
}

// fileHashes returns the md5, sha1, and sha256 sums and size of the file at path... apt lists all three and rpm repos use the sha256.
func fileHashes(path string) (md5sum, sha1sum, sha256sum string, size int64, err error) {
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
