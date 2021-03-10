package packager

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

const (
	distroSep = ":"
	distroK3s = "k3s"
	// distroRKE2 = "rke2"
)

var (
	packageDistros = map[string]bool{
		distroK3s: true,
		// distroRKE2: true,
	}
)

// Config passes all configuration options into a Packager.
type Config struct {
	// Destination specifies the writer the package will be outputted to.
	Destination io.Writer

	// KubernetesVersion specifies the distribution and version of Kubernetes to
	// use for the deployed cluster. Must be of format `k3s:1.18.8-rc1+k3s1`;
	// currently only supports k3s.
	KubernetesVersion string

	// HTTPClient is the client to use for all HTTP calls. If nil, will use the
	// default client from http module. Possible use is to trust additional CAs
	// without assuming access to the local filesystem.
	HTTPClient *http.Client
}

func (c Config) Complete() (completedPackageConfig, error) {
	splitK8s := strings.Split(c.KubernetesVersion, distroSep)
	if len(splitK8s) != 2 {
		return completedPackageConfig{}, fmt.Errorf("bad kubernetes version provided: %q", c.KubernetesVersion)
	}

	k8sDistro, k8sVersion := splitK8s[0], splitK8s[1]
	if !packageDistros[k8sDistro] {
		return completedPackageConfig{}, fmt.Errorf("kubernetes distribution %q not supported", k8sDistro)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	res := completedPackageConfig{
		dst:        c.Destination,
		k8sDistro:  k8sDistro,
		k8sVersion: k8sVersion,
		httpClient: httpClient,
	}
	return res, nil
}

type completedPackageConfig struct {
	dst        io.Writer
	k8sDistro  string
	k8sVersion string
	httpClient *http.Client
}

// Packager provides the functionality for collecting and packaging all
// dependencies required to install a Kubernetes cluster and deploy utility
// applications to that cluster.
type Packager struct {
	completedPackageConfig
}

// New returns a new Packager from the provided config
func New(config Config) (*Packager, error) {
	completeConfig, err := config.Complete()
	if err != nil {
		return nil, err
	}

	res := &Packager{
		completedPackageConfig: completeConfig,
	}
	return res, nil
}

func (p *Packager) Run() error {
	var err error

	gzipWriter := gzip.NewWriter(p.dst)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	switch p.k8sDistro {
	case distroK3s:
		err = packageK3sArtifacts(p, tarWriter, p.k8sVersion)
		// case distroRKE2:
		// 	err = packageRKE2Artifacts(tarWriter, p.k8sVersion)
	}
	if err != nil {
		return fmt.Errorf("package %s artifacts: %v", p.k8sDistro, err)
	}

	return nil
}

// Artifact represents a source artifact with an optional name to prevent
// conflicts. Source must point to a local file or an http/https endpoint.
type Artifact struct {
	Source string
	Name   string
}

// GetName returns the explicitly set name of the artifact or the base path
// segment as a default.
func (a Artifact) GetName() string {
	if a.Name == "" {
		return path.Base(a.Source)
	}
	return a.Name
}

// ArtifactGroup defines a set of collected artifacts in a packaged directory.
type ArtifactGroup struct {
	PackagePath   string
	Binaries      []Artifact
	ImageArchives []Artifact
	ImageLists    []Artifact
	// TODO - validate SHA256 hashes
	// SHA256Sums []string
}

const (
	binBaseDirName           = "bin"
	imageArchivesBaseDirName = "image-archives"
	imageListsBaseDirName    = "image-lists"
)

// Package downloads or reads all files in the ArtifactGroup and writes
// them to dst according to the expected file structure. Package does not
// handle creating any parent directory Headers in dst; this must be handled
// outside of this function.
func (p *Packager) Package(dst *tar.Writer, g ArtifactGroup) error {
	packageTime := time.Now()
	// TODO - download/read Binaries, ImageArchives, ImageLists in parallel

	// collect and package all binaries
	if len(g.Binaries) != 0 {

		binFolderHeader := &tar.Header{
			Typeflag: tar.TypeDir,
			Name:     path.Join(g.PackagePath, binBaseDirName),
			ModTime:  packageTime,
			Mode:     0755,
		}
		if err := dst.WriteHeader(binFolderHeader); err != nil {
			return fmt.Errorf("add tar header for binary directory %q: %v", binFolderHeader.Name, err)
		}

		for _, binArtifact := range g.Binaries {
			binName := binArtifact.GetName()

			buf := &bytes.Buffer{}
			if err := p.PackageBinaryArtifact(buf, binArtifact); err != nil {
				return fmt.Errorf("package binary %q: %v", binName, err)
			}

			binHeader := &tar.Header{
				Typeflag: tar.TypeReg,
				Name:     path.Join(g.PackagePath, binBaseDirName, binName),
				Size:     int64(buf.Len()),
				ModTime:  packageTime,
				Mode:     0755,
			}
			if err := dst.WriteHeader(binHeader); err != nil {
				return fmt.Errorf("add tar header for binary %q: %v", binHeader.Name, err)
			}
			if _, err := io.Copy(dst, buf); err != nil {
				return fmt.Errorf("add binary %q: %v", binHeader.Name, err)
			}
		}
	}

	// collect and package all image archives
	if len(g.ImageArchives) != 0 {
		imageArchivesFolderHeader := &tar.Header{
			Typeflag: tar.TypeDir,
			Name:     path.Join(g.PackagePath, imageArchivesBaseDirName),
			ModTime:  packageTime,
			Mode:     0755,
		}
		if err := dst.WriteHeader(imageArchivesFolderHeader); err != nil {
			return fmt.Errorf("add tar header for image archive directory %q: %v", imageArchivesFolderHeader.Name, err)
		}
		for _, imageArchiveArtifact := range g.ImageArchives {
			imageArchiveName := imageArchiveArtifact.GetName()

			buf := &bytes.Buffer{}
			if err := p.PackageImageArchiveArtifact(buf, imageArchiveArtifact); err != nil {
				return fmt.Errorf("package image archive %q: %v", imageArchiveName, err)
			}

			imageArchiveHeader := &tar.Header{
				Typeflag: tar.TypeReg,
				Name:     path.Join(g.PackagePath, imageArchivesBaseDirName, imageArchiveName+".gz"),
				Size:     int64(buf.Len()),
				ModTime:  packageTime,
				Mode:     0644,
			}
			if err := dst.WriteHeader(imageArchiveHeader); err != nil {
				return fmt.Errorf("add tar header for image archive %q: %v", imageArchiveName, err)
			}
			if _, err := io.Copy(dst, buf); err != nil {
				return fmt.Errorf("add iamge archive %q: %v", imageArchiveHeader.Name, err)
			}
		}
	}

	// collect and package all image lists
	if len(g.ImageLists) != 0 {
		imageListsFolderHeader := &tar.Header{
			Typeflag: tar.TypeDir,
			Name:     path.Join(g.PackagePath, imageListsBaseDirName),
			ModTime:  packageTime,
			Mode:     0755,
		}
		if err := dst.WriteHeader(imageListsFolderHeader); err != nil {
			return fmt.Errorf("add tar header for image list directory %q: %v", imageListsFolderHeader.Name, err)
		}
		for _, imageListArtifact := range g.ImageLists {
			imageListName := imageListArtifact.GetName()

			buf := &bytes.Buffer{}
			if err := p.PackageImageListArtifact(buf, imageListArtifact); err != nil {
				return fmt.Errorf("package image list %q: %v", imageListName, err)
			}

			imageListHeader := &tar.Header{
				Typeflag: tar.TypeReg,
				Name:     path.Join(g.PackagePath, imageListsBaseDirName, imageListName+".tar.gz"),
				Size:     int64(buf.Len()),
				ModTime:  packageTime,
				Mode:     0644,
			}
			if err := dst.WriteHeader(imageListHeader); err != nil {
				return fmt.Errorf("add tar header for image list %q: %v", imageListName, err)
			}
			if _, err := io.Copy(dst, buf); err != nil {
				return fmt.Errorf("add iamge list %q: %v", imageListHeader.Name, err)
			}
		}
	}

	return nil
}

// PackageBinaryArtifact writes the contents of the specified binary to dst,
// fetching the binary from artifact.Source.
func (p *Packager) PackageBinaryArtifact(dst io.Writer, artifact Artifact) error {
	srcURL, err := url.Parse(artifact.Source)
	if err != nil {
		return fmt.Errorf("parse binary source %q: %v", artifact.Source, err)
	}

	var binSrc io.Reader
	if srcURL.Host != "" {
		switch srcURL.Scheme {
		case "http", "https":
			resp, err := p.httpClient.Get(srcURL.String())
			if err != nil {
				return fmt.Errorf("get binary from URL %q: %v", srcURL, err)
			}
			defer resp.Body.Close()

			// TODO - confirm good HTTP status codes
			if resp.StatusCode != 200 {
				b := &strings.Builder{}
				if _, cpErr := io.Copy(b, resp.Body); cpErr != nil {
					// could not get response body, return simple error
					return fmt.Errorf(
						"get binary from URL %q: bad response: status %s",
						srcURL, resp.Status,
					)
				}
				return fmt.Errorf(
					"get binary from URL %q: bad response: status %s, body %s",
					srcURL, resp.Status, b.String(),
				)
			}

			binSrc = resp.Body

		default:
			return fmt.Errorf("unsupported URL scheme %q", srcURL.Scheme)

		}
	} else {
		// TODO - use more efficient but lower-level os.Open to minimize RAM use
		binBuf, err := ioutil.ReadFile(srcURL.String())
		if err != nil {
			return fmt.Errorf("read binary file %q: %v", srcURL.String(), err)
		}
		binSrc = bytes.NewBuffer(binBuf)
	}

	// package binary by performing a simple copy
	if _, err := io.Copy(dst, binSrc); err != nil {
		return fmt.Errorf("write binary %q: %v", artifact.GetName(), err)
	}

	return nil
}

// PackageImageArchiveArtifact writes the gzip-compressed contents of the
// specified image archive to dst, fetching the archive from artifact.Source.
func (p *Packager) PackageImageArchiveArtifact(dst io.Writer, artifact Artifact) error {
	srcURL, err := url.Parse(artifact.Source)
	if err != nil {
		return fmt.Errorf("parse image archive source %q: %v", artifact.Source, err)
	}

	var archiveSrc io.Reader
	if srcURL.Host != "" {
		switch srcURL.Scheme {
		case "http", "https":
			resp, err := p.httpClient.Get(srcURL.String())
			if err != nil {
				return fmt.Errorf("get image archive from URL %q: %v", srcURL, err)
			}
			defer resp.Body.Close()

			// TODO - confirm good HTTP status codes
			if resp.StatusCode != 200 {
				b := &strings.Builder{}
				if _, cpErr := io.Copy(b, resp.Body); cpErr != nil {
					// could not get response body, return simple error
					return fmt.Errorf(
						"get image archive from URL %q: bad response: status %s",
						srcURL, resp.Status,
					)
				}
				return fmt.Errorf(
					"get image archive from URL %q: bad response: status %s, body %s",
					srcURL, resp.Status, b.String(),
				)
			}

			archiveSrc = resp.Body

		default:
			return fmt.Errorf("unsupported URL scheme %q", srcURL.Scheme)

		}
	} else {
		// TODO - use more efficient but lower-level os.Open to minimize RAM use
		archiveBuf, err := ioutil.ReadFile(srcURL.String())
		if err != nil {
			return fmt.Errorf("read image archive file %q: %v", srcURL.String(), err)
		}
		archiveSrc = bytes.NewBuffer(archiveBuf)
	}

	// package image archive by performing a copy to a gzip writer
	gzipDst := gzip.NewWriter(dst)
	defer gzipDst.Close()

	if _, err := io.Copy(gzipDst, archiveSrc); err != nil {
		return fmt.Errorf("write image archive %q: %v", artifact.GetName(), err)
	}

	return nil
}

// PackageImageListArtifact fetches the contents of an image list from
// artifact.Source and calls ArchiveImageList to write the resulting images
// to dst.
func (p *Packager) PackageImageListArtifact(dst io.Writer, artifact Artifact) error {
	srcURL, err := url.Parse(artifact.Source)
	if err != nil {
		return fmt.Errorf("parse image list source %q: %v", artifact.Source, err)
	}

	var listSrc io.Reader
	if srcURL.Host != "" {
		switch srcURL.Scheme {
		case "http", "https":
			resp, err := p.httpClient.Get(srcURL.String())
			if err != nil {
				return fmt.Errorf("get image list from URL %q: %v", srcURL, err)
			}
			defer resp.Body.Close()

			// TODO - confirm good HTTP status codes
			if resp.StatusCode != 200 {
				b := &strings.Builder{}
				if _, cpErr := io.Copy(b, resp.Body); cpErr != nil {
					// could not get response body, return simple error
					return fmt.Errorf(
						"get image list from URL %q: bad response: status %s",
						srcURL, resp.Status,
					)
				}
				return fmt.Errorf(
					"get image list from URL %q: bad response: status %s, body %s",
					srcURL, resp.Status, b.String(),
				)
			}

			listSrc = resp.Body

		default:
			return fmt.Errorf("unsupported URL scheme %q", srcURL.Scheme)

		}
	} else {
		// TODO - use more efficient but lower-level os.Open to minimize RAM use
		listBuf, err := ioutil.ReadFile(srcURL.String())
		if err != nil {
			return fmt.Errorf("read image list file %q: %v", srcURL.String(), err)
		}
		listSrc = bytes.NewBuffer(listBuf)
	}

	if err := p.PackageImageList(dst, listSrc); err != nil {
		return fmt.Errorf("archive images: %v", err)
	}

	return nil
}

// PackageImageList reads a list of newline-delimited images from src, pulls all
// images, and writes the gzip-compressed tarball to dst.
func (p *Packager) PackageImageList(dst io.Writer, src io.Reader) error {
	scanner := bufio.NewScanner(src)

	refToImage := map[name.Reference]v1.Image{}

	for scanner.Scan() {
		src := scanner.Text()

		ref, err := name.ParseReference(src)
		if err != nil {
			return fmt.Errorf("bad image reference %s: %v", src, err)
		}

		transport := p.httpClient.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}

		img, err := crane.Pull(src, crane.WithTransport(transport))
		if err != nil {
			return fmt.Errorf("pull image %s: %v", src, err)
		}

		refToImage[ref] = img
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read image list: %v", err)
	}

	gzipDst := gzip.NewWriter(dst)
	defer gzipDst.Close()

	if err := tarball.MultiRefWrite(refToImage, gzipDst); err != nil {
		return fmt.Errorf("write images: %v", err)
	}

	return nil
}

const (
	k3sDownloadFmtStr = `https://github.com/k3s-io/k3s/releases/download/%s/%s`
	k3sRawFmtStr      = `https://raw.githubusercontent.com/k3s-io/k3s/%s/%s`

	k3sVersionDefault          = "v1.19.5+k3s1"
	k3sInstallScriptRefDefault = "master"
)

type K3sPackageConfig struct {
	Version          string
	InstallScriptRef string
}

type completedK3sPackageConfig struct {
	K3sPackageConfig
}

func (c *K3sPackageConfig) Complete() *completedK3sPackageConfig {
	config := &completedK3sPackageConfig{
		K3sPackageConfig: *c,
	}

	if config.Version == "" {
		config.Version = k3sVersionDefault
	}
	if config.InstallScriptRef == "" {
		config.InstallScriptRef = k3sInstallScriptRefDefault
	}

	return config
}

func packageK3sArtifacts(p *Packager, dst *tar.Writer, config *completedK3sPackageConfig) error {
	binaries := []Artifact{
		{Source: fmt.Sprintf(k3sDownloadFmtStr, url.QueryEscape(config.Version), "k3s")},
		{Source: fmt.Sprintf(k3sRawFmtStr, url.QueryEscape(config.InstallScriptRef), "install.sh")},
	}
	imageArchives := []Artifact{
		{Source: fmt.Sprintf(k3sDownloadFmtStr, url.QueryEscape(config.Version), "k3s-airgap-images-amd64.tar")},
	}

	k3sArtifactGroup := ArtifactGroup{
		PackagePath:   "kubernetes/k3s",
		Binaries:      binaries,
		ImageArchives: imageArchives,
	}

	if err := TarMkdirP(dst, k3sArtifactGroup.PackagePath); err != nil {
		return fmt.Errorf("make k3s base directory: %v", err)
	}

	if err := p.Package(dst, k3sArtifactGroup); err != nil {
		return fmt.Errorf("collect k3s artifacts: %v", err)
	}

	log.Println("packageK3sArtifacts done")

	return nil
}

// TarMkdirP creates the full chain of Header entries in dst based on the
// provided nested directory string path.
func TarMkdirP(dst *tar.Writer, path string) error {
	mkdirpTime := time.Now()

	path = strings.TrimSuffix(path, "/")
	paths := strings.Split(path, "/")

	for i := range paths {
		dirName := strings.Join(paths[:i+1], "/") + "/"
		dirHeader := &tar.Header{
			Typeflag: tar.TypeDir,
			Name:     dirName,
			ModTime:  mkdirpTime,
			Mode:     0755,
		}
		if err := dst.WriteHeader(dirHeader); err != nil {
			return fmt.Errorf("write directory %q: %v", dirName, err)
		}
	}

	return nil
}

// CopyGzip is a tiny wrapper to write the gzip-compressed contents of src to dst
func CopyGzip(dst io.Writer, src io.Reader) error {
	gzipDst := gzip.NewWriter(dst)
	defer gzipDst.Close()

	_, err := io.Copy(gzipDst, src)
	return err
}
