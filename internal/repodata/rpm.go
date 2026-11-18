package repodata

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	rpmutils "github.com/sassoftware/go-rpmutils"

	"hauler.dev/go/hauler/v2/pkg/log"
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
	Pre   string `xml:"pre,attr,omitempty"`
}

type rpmFile struct {
	Type string `xml:"type,attr,omitempty"`
	Path string `xml:",chardata"`
}

type rpmChangelog struct {
	Author string `xml:"author,attr"`
	Date   int64  `xml:"date,attr"`
	Text   string `xml:",chardata"`
}

// rpm tag and dependency flag values that go-rpmutils doesn't define, from rpm's rpmtag.h and rpmds.h
const (
	tagRecommendName     = 5046
	tagRecommendVersion  = 5047
	tagRecommendFlags    = 5048
	tagSuggestName       = 5049
	tagSuggestVersion    = 5050
	tagSuggestFlags      = 5051
	tagSupplementName    = 5052
	tagSupplementVersion = 5053
	tagSupplementFlags   = 5054
	tagEnhanceName       = 5055
	tagEnhanceVersion    = 5056
	tagEnhanceFlags      = 5057

	// legacy SUSE/Mageia weak dependency tags that createrepo_c still reads
	tagOldSuggestsName    = 1156
	tagOldSuggestsVersion = 1157
	tagOldSuggestsFlags   = 1158
	tagOldEnhancesName    = 1159
	tagOldEnhancesVersion = 1160
	tagOldEnhancesFlags   = 1161

	rpmsensePostTrans  = 1 << 5
	rpmsensePreReq     = 1 << 6
	rpmsensePreTrans   = 1 << 7
	rpmsenseScriptPre  = 1 << 9
	rpmsenseScriptPost = 1 << 10
	rpmsenseMissingOK  = 1 << 19
	rpmsenseStrong     = 1 << 27

	// requires with any of these flags get pre="1" in primary.xml
	rpmsensePre = rpmsensePreReq | rpmsenseScriptPre | rpmsenseScriptPost | rpmsensePreTrans | rpmsensePostTrans

	// createrepo_c keeps the newest 10 changelog entries by default
	changelogLimit = 10
)

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
	Conflicts   *rpmDepList    `xml:"rpm:conflicts,omitempty"`
	Obsoletes   *rpmDepList    `xml:"rpm:obsoletes,omitempty"`
	Suggests    *rpmDepList    `xml:"rpm:suggests,omitempty"`
	Enhances    *rpmDepList    `xml:"rpm:enhances,omitempty"`
	Recommends  *rpmDepList    `xml:"rpm:recommends,omitempty"`
	Supplements *rpmDepList    `xml:"rpm:supplements,omitempty"`
	Files       []rpmFile      `xml:"file"`
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

	// every file and the recent changelogs, written to filelists.xml and other.xml instead of primary.xml
	files      []rpmFile
	changelogs []rpmChangelog
}

type rpmMetadata struct {
	XMLName  xml.Name     `xml:"metadata"`
	Xmlns    string       `xml:"xmlns,attr"`
	XmlnsRpm string       `xml:"xmlns:rpm,attr"`
	Packages int          `xml:"packages,attr"`
	Package  []rpmPackage `xml:"package"`
}

type filelistsPackage struct {
	Pkgid   string     `xml:"pkgid,attr"`
	Name    string     `xml:"name,attr"`
	Arch    string     `xml:"arch,attr"`
	Version rpmVersion `xml:"version"`
	Files   []rpmFile  `xml:"file"`
}

type filelistsMetadata struct {
	XMLName  xml.Name           `xml:"filelists"`
	Xmlns    string             `xml:"xmlns,attr"`
	Packages int                `xml:"packages,attr"`
	Package  []filelistsPackage `xml:"package"`
}

type otherPackage struct {
	Pkgid      string         `xml:"pkgid,attr"`
	Name       string         `xml:"name,attr"`
	Arch       string         `xml:"arch,attr"`
	Version    rpmVersion     `xml:"version"`
	Changelogs []rpmChangelog `xml:"changelog"`
}

type otherMetadata struct {
	XMLName  xml.Name       `xml:"otherdata"`
	Xmlns    string         `xml:"xmlns,attr"`
	Packages int            `xml:"packages,attr"`
	Package  []otherPackage `xml:"package"`
}

type repomdChecksum struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

type repomdData struct {
	Type         string         `xml:"type,attr"`
	Checksum     repomdChecksum `xml:"checksum"`
	OpenChecksum repomdChecksum `xml:"open-checksum"`
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

// GenerateRPMRepo scans dir for top-level .rpm files and writes a repodata/ directory next to them, so dnf/yum/zypper can consume dir as a repo, and returns how many packages it indexed... a file it can't read is skipped with a warning like createrepo_c does.
func GenerateRPMRepo(ctx context.Context, dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}

	var pkgs []rpmPackage
	for _, e := range entries {
		if e.IsDir() || PackageType(e.Name()) != "rpm" {
			continue
		}
		pkg, err := buildRPMPackage(filepath.Join(dir, e.Name()), e.Name())
		if err != nil {
			log.FromContext(ctx).Warnf("failed to read rpm package [%s]: %v... skipping", e.Name(), err)
			continue
		}
		pkgs = append(pkgs, *pkg)
	}
	if len(pkgs) == 0 {
		return 0, nil
	}

	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Location.Href < pkgs[j].Location.Href })

	meta := rpmMetadata{
		Xmlns:    "http://linux.duke.edu/metadata/common",
		XmlnsRpm: "http://linux.duke.edu/metadata/rpm",
		Packages: len(pkgs),
		Package:  pkgs,
	}

	filelists := filelistsMetadata{Xmlns: "http://linux.duke.edu/metadata/filelists", Packages: len(pkgs)}
	other := otherMetadata{Xmlns: "http://linux.duke.edu/metadata/other", Packages: len(pkgs)}
	for _, p := range pkgs {
		filelists.Package = append(filelists.Package, filelistsPackage{Pkgid: p.Checksum.Value, Name: p.Name, Arch: p.Arch, Version: p.Version, Files: p.files})
		other.Package = append(other.Package, otherPackage{Pkgid: p.Checksum.Value, Name: p.Name, Arch: p.Arch, Version: p.Version, Changelogs: p.changelogs})
	}

	// rebuild repodata from scratch so checksum named files from earlier runs don't pile up
	repodataDir := filepath.Join(dir, "repodata")
	if err := os.RemoveAll(repodataDir); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(repodataDir, 0o755); err != nil {
		return 0, err
	}

	rmd := repomd{
		Xmlns:    "http://linux.duke.edu/metadata/repo",
		XmlnsRpm: "http://linux.duke.edu/metadata/rpm",
		Revision: time.Now().Unix(),
	}
	for _, md := range []struct {
		kind string
		v    any
	}{{"primary", meta}, {"filelists", filelists}, {"other", other}} {
		data, err := writeRepomdData(repodataDir, md.kind, md.v)
		if err != nil {
			return 0, err
		}
		rmd.Data = append(rmd.Data, data)
	}

	repomdXML, err := xml.MarshalIndent(rmd, "", "  ")
	if err != nil {
		return 0, err
	}
	repomdXML = append([]byte(xml.Header), repomdXML...)

	return len(pkgs), os.WriteFile(filepath.Join(repodataDir, "repomd.xml"), repomdXML, 0o644)
}

// writeRepomdData gzips v as kind's xml into repodataDir under a checksum prefixed name like createrepo_c does, and returns its repomd.xml entry.
func writeRepomdData(repodataDir, kind string, v any) (repomdData, error) {
	raw, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		return repomdData{}, err
	}
	raw = append([]byte(xml.Header), raw...)

	tmpPath := filepath.Join(repodataDir, kind+".xml.gz.tmp")
	sums, err := writeGzip(tmpPath, raw)
	if err != nil {
		return repomdData{}, err
	}
	name := sums.gzChecksum + "-" + kind + ".xml.gz"
	if err := os.Rename(tmpPath, filepath.Join(repodataDir, name)); err != nil {
		return repomdData{}, err
	}

	data := repomdData{
		Type:         kind,
		Checksum:     repomdChecksum{Type: "sha256", Value: sums.gzChecksum},
		OpenChecksum: repomdChecksum{Type: "sha256", Value: sums.rawChecksum},
		Timestamp:    time.Now().Unix(),
		Size:         sums.gzSize,
		OpenSize:     sums.rawSize,
	}
	data.Location.Href = "repodata/" + name
	return data, nil
}

// buildRPMPackage reads the rpm at path into its primary.xml entry, with filename as its location in the repo.
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

	_, _, fileChecksum, fileSize, err := fileHashes(path)
	if err != nil {
		return nil, err
	}
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}

	files, err := rpmFiles(hdr)
	if err != nil {
		return nil, err
	}
	ownFiles := map[string]bool{}
	var primaryFiles []rpmFile
	for _, file := range files {
		ownFiles[file.Path] = true
		if isPrimaryFile(file.Path) {
			primaryFiles = append(primaryFiles, file)
		}
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
	archiveSize, err := hdr.PayloadSize()
	if err != nil {
		archiveSize = installedSize
	}

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
			File:  stat.ModTime().Unix(),
			Build: int64(buildTime),
		},
		Size: rpmSize{
			Package:   fileSize,
			Installed: installedSize,
			Archive:   archiveSize,
		},
		Location: rpmLocation{Href: filename},
		Format: rpmFormat{
			License:     license,
			Vendor:      vendor,
			Group:       group,
			Buildhost:   buildhost,
			SourceRPM:   sourceRPM,
			HeaderRange: rpmHeaderRange{Start: rng.Start, End: rng.End},
			Files:       primaryFiles,
		},
		files:      files,
		changelogs: rpmChangelogs(hdr),
	}
	addDependencies(&pkg.Format, hdr, ownFiles)

	return pkg, nil
}

// rpmFiles lists every file in hdr, marking directories and ghost files the way filelists.xml expects.
func rpmFiles(hdr *rpmutils.RpmHeader) ([]rpmFile, error) {
	paths, err := hdr.GetStrings(rpmutils.OLDFILENAMES)
	if err != nil {
		if errors.As(err, &rpmutils.NoSuchTagError{}) {
			return nil, nil
		}
		return nil, err
	}
	modes, _ := hdr.GetInts(rpmutils.FILEMODES)
	flags, _ := hdr.GetInts(rpmutils.FILEFLAGS)

	files := make([]rpmFile, 0, len(paths))
	for i, p := range paths {
		file := rpmFile{Path: p}
		// like createrepo_c, a ghost directory is still listed as a dir
		switch {
		case i < len(modes) && modes[i]&0o170000 == 0o040000:
			file.Type = "dir"
		case i < len(flags) && flags[i]&rpmutils.RPMFILE_GHOST != 0:
			file.Type = "ghost"
		}
		files = append(files, file)
	}
	return files, nil
}

// isPrimaryFile reports whether createrepo_c would also list path in primary.xml, so file requires resolve without filelists.xml.
func isPrimaryFile(path string) bool {
	return strings.HasPrefix(path, "/etc/") || strings.Contains(path, "bin/") || path == "/usr/lib/sendmail"
}

// rpmChangelogs returns hdr's newest changelog entries up to changelogLimit, oldest first like createrepo_c.
func rpmChangelogs(hdr *rpmutils.RpmHeader) []rpmChangelog {
	times, _ := hdr.GetInts(rpmutils.CHANGELOGTIME)
	names, _ := hdr.GetStrings(rpmutils.CHANGELOGNAME)
	texts, _ := hdr.GetStrings(rpmutils.CHANGELOGTEXT)

	n := min(len(times), len(names), len(texts), changelogLimit)
	dates := make([]int64, n)
	var last int64
	for i := range n {
		t := int64(times[i])
		dates[i] = t
		if i > 0 && t == last {
			// like createrepo_c, bump the newer entries that share this timestamp so every date is unique
			for j, d := i-1, t; j >= 0 && dates[j] == d; j, d = j-1, d+1 {
				dates[j]++
			}
		} else {
			last = t
		}
	}
	changelogs := make([]rpmChangelog, 0, n)
	for i := n - 1; i >= 0; i-- {
		changelogs = append(changelogs, rpmChangelog{Author: strings.TrimRight(names[i], " "), Date: dates[i], Text: texts[i]})
	}
	return changelogs
}

// rpmDep is one dependency read from an rpm header's parallel name/version/flags tag arrays.
type rpmDep struct {
	name, ver string
	flags     int
}

// readDeps reads the dependencies stored under nameTag, verTag, and flagsTag in hdr.
func readDeps(hdr *rpmutils.RpmHeader, nameTag, verTag, flagsTag int) []rpmDep {
	names, _ := hdr.GetStrings(nameTag)
	vers, _ := hdr.GetStrings(verTag)
	flags, _ := hdr.GetInts(flagsTag)

	deps := make([]rpmDep, len(names))
	for i, name := range names {
		deps[i].name = name
		if i < len(vers) {
			deps[i].ver = vers[i]
		}
		if i < len(flags) {
			deps[i].flags = flags[i]
		}
	}
	return deps
}

// key matches createrepo_c's name, flags, and version string used to find requires the package provides itself.
func (d rpmDep) key() string {
	return d.name + "\x00" + senseFlagsToString(d.flags) + "\x00" + d.ver
}

// entry converts d to its primary.xml form.
func (d rpmDep) entry() rpmDepEntry {
	e := rpmDepEntry{Name: d.name}
	if d.ver != "" {
		e.Ver = d.ver
		e.Flags = senseFlagsToString(d.flags)
		if epoch, v, rel, ok := splitEVR(d.ver); ok {
			e.Epoch = epoch
			e.Ver = v
			e.Rel = rel
		}
	}
	return e
}

// appendDep adds e to list, creating the list on first use so empty lists stay out of primary.xml.
func appendDep(list *rpmDepList, e rpmDepEntry) *rpmDepList {
	if list == nil {
		list = &rpmDepList{}
	}
	list.Entries = append(list.Entries, e)
	return list
}

// addDependencies fills format's dependency lists from hdr, filtering requires and folding legacy weak dependencies the way createrepo_c does.
func addDependencies(format *rpmFormat, hdr *rpmutils.RpmHeader, ownFiles map[string]bool) {
	provided := map[string]bool{}
	for _, d := range readDeps(hdr, rpmutils.PROVIDENAME, rpmutils.PROVIDEVERSION, rpmutils.PROVIDEFLAGS) {
		provided[d.key()] = true
		format.Provides = appendDep(format.Provides, d.entry())
	}
	for _, d := range readDeps(hdr, rpmutils.CONFLICTNAME, rpmutils.CONFLICTVERSION, rpmutils.CONFLICTFLAGS) {
		format.Conflicts = appendDep(format.Conflicts, d.entry())
	}
	for _, d := range readDeps(hdr, rpmutils.OBSOLETENAME, rpmutils.OBSOLETEVERSION, rpmutils.OBSOLETEFLAGS) {
		format.Obsoletes = appendDep(format.Obsoletes, d.entry())
	}

	// createrepo_c keeps only the highest libc.so.6 requirement and lists it last
	var libc *rpmDepEntry
	added := map[string]rpmDepEntry{}
	for _, d := range readDeps(hdr, rpmutils.REQUIRENAME, rpmutils.REQUIREVERSION, rpmutils.REQUIREFLAGS) {
		if strings.HasPrefix(d.name, "rpmlib(") || (ownFiles[d.name] && isPrimaryFile(d.name)) || provided[d.key()] {
			continue
		}
		e := d.entry()
		if d.flags&rpmsensePre != 0 {
			e.Pre = "1"
		}
		if prev, ok := added[e.Name]; ok && prev == e {
			continue
		}
		switch {
		case d.flags&rpmsenseMissingOK != 0:
			e.Pre = ""
			format.Recommends = appendDep(format.Recommends, e)
		case strings.HasPrefix(e.Name, "libc.so.6"):
			if libc == nil || compareLibcDep(libc.Name, e.Name) == 2 {
				libc = &e
			}
		default:
			format.Requires = appendDep(format.Requires, e)
			added[e.Name] = e
		}
	}
	if libc != nil {
		format.Requires = appendDep(format.Requires, *libc)
	}

	for _, d := range readDeps(hdr, tagSuggestName, tagSuggestVersion, tagSuggestFlags) {
		format.Suggests = appendDep(format.Suggests, d.entry())
	}
	for _, d := range readDeps(hdr, tagEnhanceName, tagEnhanceVersion, tagEnhanceFlags) {
		format.Enhances = appendDep(format.Enhances, d.entry())
	}
	for _, d := range readDeps(hdr, tagRecommendName, tagRecommendVersion, tagRecommendFlags) {
		format.Recommends = appendDep(format.Recommends, d.entry())
	}
	for _, d := range readDeps(hdr, tagSupplementName, tagSupplementVersion, tagSupplementFlags) {
		format.Supplements = appendDep(format.Supplements, d.entry())
	}
	for _, d := range readDeps(hdr, tagOldSuggestsName, tagOldSuggestsVersion, tagOldSuggestsFlags) {
		if d.flags&rpmsenseStrong != 0 {
			format.Recommends = appendDep(format.Recommends, d.entry())
		} else {
			format.Suggests = appendDep(format.Suggests, d.entry())
		}
	}
	for _, d := range readDeps(hdr, tagOldEnhancesName, tagOldEnhancesVersion, tagOldEnhancesFlags) {
		if d.flags&rpmsenseStrong != 0 {
			format.Supplements = appendDep(format.Supplements, d.entry())
		} else {
			format.Enhances = appendDep(format.Enhances, d.entry())
		}
	}
}

// compareLibcDep ports createrepo_c's cr_compare_dependency for libc.so.6 requires... 0 when equal, 1 when dep1 is higher, 2 when dep2 is higher, -1 when neither has a closing paren.
func compareLibcDep(dep1, dep2 string) int {
	o1, o2 := strings.Index(dep1, "("), strings.Index(dep2, "(")
	switch {
	case o1 < 0 && o2 < 0:
		return 0
	case o1 < 0:
		return 2
	case o2 < 0:
		return 1
	}
	c1, c2 := strings.Index(dep1[o1:], ")"), strings.Index(dep2[o2:], ")")
	switch {
	case c1 < 0 && c2 < 0:
		return -1
	case c1 < 0:
		return 2
	case c2 < 0:
		return 1
	}
	v1, v2 := dep1[o1+1:o1+c1], dep2[o2+1:o2+c2]
	switch {
	case v1 == "" && v2 == "":
		return 0
	case v1 == "":
		return 2
	case v2 == "":
		return 1
	}
	d1, d2 := strings.IndexAny(v1, "0123456789"), strings.IndexAny(v2, "0123456789")
	switch {
	case d1 < 0 && d2 < 0:
		return 0
	case d1 < 0:
		return 2
	case d2 < 0:
		return 1
	}
	switch rpmutils.Vercmp(v1[d1:], v2[d2:]) {
	case 1:
		return 1
	case -1:
		return 2
	}
	return 0
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

// checksums holds the sha256 and size of a metadata file before and after gzip, which repomd.xml lists.
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
