package repodata

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	rpmutils "github.com/sassoftware/go-rpmutils"
)

type rpmVersion struct {
	Epoch string `xml:"epoch,attr"`
	Ver   string `xml:"ver,attr"`
	Rel   string `xml:"rel,attr"`
}

type rpmChecksum struct {
	Type  string `xml:"type,attr"`
	Pkgid string `xml:"pkgid,attr"`
	Value string `xml:",chardata"`
}

type rpmTime struct {
	File  int64 `xml:"file,attr"`
	Build int64 `xml:"build,attr"`
}

type rpmSize struct {
	Package   int64 `xml:"package,attr"`
	Installed int64 `xml:"installed,attr"`
	Archive   int64 `xml:"archive,attr"`
}

type rpmLocation struct {
	Href string `xml:"href,attr"`
}

type rpmDepEntry struct {
	Name  string `xml:"name,attr"`
	Flags string `xml:"flags,attr,omitempty"`
	Epoch string `xml:"epoch,attr,omitempty"`
	Ver   string `xml:"ver,attr,omitempty"`
	Rel   string `xml:"rel,attr,omitempty"`
}

type rpmDepList struct {
	Entries []rpmDepEntry `xml:"rpm:entry"`
}

type rpmHeaderRange struct {
	Start int `xml:"start,attr"`
	End   int `xml:"end,attr"`
}

type rpmFormat struct {
	License     string         `xml:"rpm:license"`
	Vendor      string         `xml:"rpm:vendor"`
	Group       string         `xml:"rpm:group"`
	Buildhost   string         `xml:"rpm:buildhost"`
	SourceRPM   string         `xml:"rpm:sourcerpm,omitempty"`
	HeaderRange rpmHeaderRange `xml:"rpm:header-range"`
	Provides    *rpmDepList    `xml:"rpm:provides,omitempty"`
	Requires    *rpmDepList    `xml:"rpm:requires,omitempty"`
}

type rpmPackage struct {
	Type        string      `xml:"type,attr"`
	Name        string      `xml:"name"`
	Arch        string      `xml:"arch"`
	Version     rpmVersion  `xml:"version"`
	Checksum    rpmChecksum `xml:"checksum"`
	Summary     string      `xml:"summary"`
	Description string      `xml:"description"`
	Packager    string      `xml:"packager"`
	URL         string      `xml:"url"`
	Time        rpmTime     `xml:"time"`
	Size        rpmSize     `xml:"size"`
	Location    rpmLocation `xml:"location"`
	Format      rpmFormat   `xml:"format"`
}

type rpmMetadata struct {
	XMLName  xml.Name     `xml:"metadata"`
	Xmlns    string       `xml:"xmlns,attr"`
	XmlnsRpm string       `xml:"xmlns:rpm,attr"`
	Packages int          `xml:"packages,attr"`
	Package  []rpmPackage `xml:"package"`
}

type repomdData struct {
	Type         string `xml:"type,attr"`
	Checksum     string `xml:"checksum"`
	OpenChecksum string `xml:"open-checksum"`
	Location     struct {
		Href string `xml:"href,attr"`
	} `xml:"location"`
	Timestamp int64 `xml:"timestamp"`
	Size      int64 `xml:"size"`
	OpenSize  int64 `xml:"open-size"`
}

type repomd struct {
	XMLName  xml.Name     `xml:"repomd"`
	Xmlns    string       `xml:"xmlns,attr"`
	XmlnsRpm string       `xml:"xmlns:rpm,attr"`
	Revision int64        `xml:"revision"`
	Data     []repomdData `xml:"data"`
}

// GenerateRPMRepo scans dir for top-level .rpm files and writes a repodata/ directory next to them, so dnf/yum/zypper can consume dir as a repo.
func GenerateRPMRepo(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var pkgs []rpmPackage
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".rpm") {
			continue
		}
		pkg, err := buildRPMPackage(filepath.Join(dir, e.Name()), e.Name())
		if err != nil {
			return fmt.Errorf("reading rpm package [%s]: %w", e.Name(), err)
		}
		pkgs = append(pkgs, *pkg)
	}
	if len(pkgs) == 0 {
		return nil
	}

	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Location.Href < pkgs[j].Location.Href })

	meta := rpmMetadata{
		Xmlns:    "http://linux.duke.edu/metadata/common",
		XmlnsRpm: "http://linux.duke.edu/metadata/rpm",
		Packages: len(pkgs),
		Package:  pkgs,
	}

	primaryXML, err := xml.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	primaryXML = append([]byte(xml.Header), primaryXML...)

	repodataDir := filepath.Join(dir, "repodata")
	if err := os.MkdirAll(repodataDir, 0o755); err != nil {
		return err
	}

	primaryGzPath := filepath.Join(repodataDir, "primary.xml.gz")
	sums, err := writeGzip(primaryGzPath, primaryXML)
	if err != nil {
		return err
	}

	rmd := repomd{
		Xmlns:    "http://linux.duke.edu/metadata/repo",
		XmlnsRpm: "http://linux.duke.edu/metadata/rpm",
		Revision: time.Now().Unix(),
		Data: []repomdData{
			{
				Type:         "primary",
				Checksum:     sums.gzChecksum,
				OpenChecksum: sums.rawChecksum,
				Timestamp:    time.Now().Unix(),
				Size:         sums.gzSize,
				OpenSize:     sums.rawSize,
			},
		},
	}
	rmd.Data[0].Location.Href = "repodata/primary.xml.gz"

	repomdXML, err := xml.MarshalIndent(rmd, "", "  ")
	if err != nil {
		return err
	}
	repomdXML = append([]byte(xml.Header), repomdXML...)

	return os.WriteFile(filepath.Join(repodataDir, "repomd.xml"), repomdXML, 0o644)
}

func buildRPMPackage(path, filename string) (*rpmPackage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	hdr, err := rpmutils.ReadHeader(f)
	if err != nil {
		return nil, err
	}

	nevra, err := hdr.GetNEVRA()
	if err != nil {
		return nil, err
	}

	fileChecksum, fileSize, err := sha256File(path)
	if err != nil {
		return nil, err
	}

	summary, _ := hdr.GetString(rpmutils.SUMMARY)
	description, _ := hdr.GetString(rpmutils.DESCRIPTION)
	packager, _ := hdr.GetString(rpmutils.PACKAGER)
	url, _ := hdr.GetString(rpmutils.URL)
	license, _ := hdr.GetString(rpmutils.LICENSE)
	vendor, _ := hdr.GetString(rpmutils.VENDOR)
	group, _ := hdr.GetString(rpmutils.GROUP)
	buildhost, _ := hdr.GetString(rpmutils.BUILDHOST)
	sourceRPM, _ := hdr.GetString(rpmutils.SOURCERPM)
	buildTime, _ := hdr.GetInt(rpmutils.BUILDTIME)

	installedSize, _ := hdr.InstalledSize()

	rng := hdr.GetRange()

	epoch := nevra.Epoch
	if epoch == "" {
		epoch = "0"
	}

	pkg := &rpmPackage{
		Type: "rpm",
		Name: nevra.Name,
		Arch: nevra.Arch,
		Version: rpmVersion{
			Epoch: epoch,
			Ver:   nevra.Version,
			Rel:   nevra.Release,
		},
		Checksum: rpmChecksum{
			Type:  "sha256",
			Pkgid: "YES",
			Value: fileChecksum,
		},
		Summary:     summary,
		Description: description,
		Packager:    packager,
		URL:         url,
		Time: rpmTime{
			File:  int64(buildTime),
			Build: int64(buildTime),
		},
		Size: rpmSize{
			Package:   fileSize,
			Installed: installedSize,
			Archive:   installedSize,
		},
		Location: rpmLocation{Href: filename},
		Format: rpmFormat{
			License:     license,
			Vendor:      vendor,
			Group:       group,
			Buildhost:   buildhost,
			SourceRPM:   sourceRPM,
			HeaderRange: rpmHeaderRange{Start: rng.Start, End: rng.End},
			Provides:    depList(hdr, rpmutils.PROVIDENAME, rpmutils.PROVIDEVERSION, rpmutils.PROVIDEFLAGS),
			Requires:    depList(hdr, rpmutils.REQUIRENAME, rpmutils.REQUIREVERSION, rpmutils.REQUIREFLAGS),
		},
	}

	return pkg, nil
}

// depList builds a <rpm:provides>/<rpm:requires> entry list from an RPM header's parallel name/version/flags tag arrays.
func depList(hdr *rpmutils.RpmHeader, nameTag, verTag, flagsTag int) *rpmDepList {
	names, _ := hdr.GetStrings(nameTag)
	if len(names) == 0 {
		return nil
	}
	vers, _ := hdr.GetStrings(verTag)
	flags, _ := hdr.GetInts(flagsTag)

	entries := make([]rpmDepEntry, 0, len(names))
	for i, name := range names {
		e := rpmDepEntry{Name: name}
		var ver string
		if i < len(vers) {
			ver = vers[i]
		}
		var flag int
		if i < len(flags) {
			flag = flags[i]
		}
		if ver != "" {
			e.Ver = ver
			e.Flags = senseFlagsToString(flag)
			if epoch, v, rel, ok := splitEVR(ver); ok {
				e.Epoch = epoch
				e.Ver = v
				e.Rel = rel
			}
		}
		entries = append(entries, e)
	}
	return &rpmDepList{Entries: entries}
}

// senseFlagsToString maps RPM's RPMSENSE_* bitmask to primary.xml's flags attribute values.
func senseFlagsToString(flags int) string {
	less := flags&rpmutils.RPMSENSE_LESS != 0
	greater := flags&rpmutils.RPMSENSE_GREATER != 0
	equal := flags&rpmutils.RPMSENSE_EQUAL != 0

	switch {
	case less && equal:
		return "LE"
	case greater && equal:
		return "GE"
	case equal:
		return "EQ"
	case less:
		return "LT"
	case greater:
		return "GT"
	default:
		return ""
	}
}

// splitEVR splits a dependency version string of the form "[epoch:]version[-release]" into its parts.
func splitEVR(evr string) (epoch, ver, rel string, ok bool) {
	epoch = "0"
	rest := evr
	if i := strings.Index(rest, ":"); i >= 0 {
		epoch = rest[:i]
		rest = rest[i+1:]
	}
	if i := strings.LastIndex(rest, "-"); i >= 0 {
		ver, rel = rest[:i], rest[i+1:]
	} else {
		ver = rest
	}
	return epoch, ver, rel, ver != ""
}

type checksums struct {
	rawChecksum string
	rawSize     int64
	gzChecksum  string
	gzSize      int64
}

// writeGzip gzip-compresses data to path and returns sha256 checksums and sizes of both the raw and compressed forms.
func writeGzip(path string, data []byte) (checksums, error) {
	rawSum := sha256.Sum256(data)

	f, err := os.Create(path)
	if err != nil {
		return checksums{}, err
	}
	defer f.Close()

	h := sha256.New()
	zw := gzip.NewWriter(io.MultiWriter(f, h))
	if _, err := zw.Write(data); err != nil {
		return checksums{}, err
	}
	if err := zw.Close(); err != nil {
		return checksums{}, err
	}

	info, err := f.Stat()
	if err != nil {
		return checksums{}, err
	}

	return checksums{
		rawChecksum: hex.EncodeToString(rawSum[:]),
		rawSize:     int64(len(data)),
		gzChecksum:  hex.EncodeToString(h.Sum(nil)),
		gzSize:      info.Size(),
	}, nil
}

func sha256File(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
