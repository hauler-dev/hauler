package packager

import (
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/util"

	"archive/tar"
	"io"
	"net/http"
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

func (p *Packager) Collect(
	dst io.Writer,
	pkgConfig v1alpha1.PackageConfig,
) error {

	return nil
}

func (p *Packager) CollectK3s(
	tarDst *tar.Writer,
	config v1alpha1.PackageK3s,
) error {
	return nil
}

func (p *Packager) CollectContainerImages(
	tarDst *tar.Writer,
	config v1alpha1.PackageContainerImages,
) error {
	return nil
}

func (p *Packager) CollectGitRepository(
	tarDst *tar.Writer,
	config v1alpha1.PackageGitRepository,
) error {
	return nil
}

func (p *Packager) CollectFileTree(
	tarDst *tar.Writer,
	config v1alpha1.PackageFileTree,
) error {
	return nil
}
