package packager

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/archive"
	"github.com/rancherfederal/hauler/pkg/util"
)

type Packager struct {
	httpClient *http.Client
}

func New(config *v1alpha1.EnvConfig) *Packager {
	if config == nil {
		config = new(v1alpha1.EnvConfig)
	}

	httpTransport := http.DefaultTransport.(*http.Transport).Clone()

	httpTransport.Proxy = util.ProxyFromEnvConfig(*config)
	httpTransport.TLSClientConfig = util.TLSFromEnvConfig(*config)

	httpClient := &http.Client{
		Transport: httpTransport,
	}

	return &Packager{
		httpClient: httpClient,
	}
}

func (p *Packager) Package(
	dst io.Writer,
	pkgConfig v1alpha1.PackageConfig,
) error {
	// wrap around dst
	aw, err := archive.NewWriter(dst, archive.WriterKindTar)
	if err != nil {
		return fmt.Errorf("create archive writer: %v", err)
	}

	var packageErrors []error
	for _, config := range pkgConfig.Packages {
		switch cfg := config.GetDef().(type) {
		case v1alpha1.PackageK3s:
			if err := p.PackageK3s(aw, cfg); err != nil {
				packageErrors = append(
					packageErrors,
					fmt.Errorf("package id %s: collect k3s: %w", config.Name, err),
				)
			}
		default:
			packageErrors = append(
				packageErrors,
				fmt.Errorf("package id %s: unknown package type", config.Name),
			)
		}
	}

	if len(packageErrors) != 0 {
		b := &strings.Builder{}
		b.WriteString("collect errors:")
		for _, err := range packageErrors {
			b.WriteString("\n")
			b.WriteString(err.Error())
		}
		return errors.New(b.String())
	}

	return nil
}

const (
	k3sReleaseDownloadFmtStr   = `https://github.com/k3s-io/k3s/releases/download/%s/%s`
	k3sRawFmtStr               = `https://raw.githubusercontent.com/k3s-io/k3s/%s/%s`
	k3sDefaultInstallScriptRef = "master"
)

func (p *Packager) PackageK3s(
	writer *archive.Writer,
	config v1alpha1.PackageK3s,
) error {
	fmt.Printf("release %s\n", config.Release)
	fmt.Printf("install script ref %s\n", config.InstallScriptRef)

	return nil
}

func (p *Packager) PackageContainerImages(
	writer *archive.Writer,
	config v1alpha1.PackageContainerImages,
) error {
	return nil
}

func (p *Packager) PackageGitRepository(
	writer *archive.Writer,
	config v1alpha1.PackageGitRepository,
) error {
	return nil
}

func (p *Packager) PackageFileTree(
	writer *archive.Writer,
	config v1alpha1.PackageFileTree,
) error {
	return nil
}
