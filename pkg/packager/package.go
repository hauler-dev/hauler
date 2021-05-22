package packager

import (
	"context"
	"fmt"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/mholt/archiver/v3"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/fetcher"
	"github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"time"
)

type Packager struct {
	Cluster *v1alpha1.Cluster
	logger *logrus.Entry
	za *archiver.TarZstd
}

func NewPackager(cluster *v1alpha1.Cluster) *Packager {
	return &Packager{
		Cluster: cluster,
		logger: logrus.WithFields(logrus.Fields{
			"cluster": cluster.Metadata.Name,
			"driver": cluster.Driver.String(),
		}),
		za: &archiver.TarZstd{
			Tar: &archiver.Tar{
				OverwriteExisting:      true,
				MkdirAll:               true,
			},
		},
	}
}

func (p *Packager) Package(ctx context.Context, outFile string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpdir)

	logrus.Debugf("Using temporary packaging directory: %s", tmpdir)

	if err := p.prepFS(tmpdir); err != nil {
		return err
	}

	var urlsToGet []string
	urlsToGet = append(urlsToGet, buildDriverURLs(p.Cluster.Driver)...)
	// TODO: User defined gets

	filesPath := filepath.Join(tmpdir, v1alpha1.HaulerBin)
	if err := p.pkgFiles(ctx, filesPath, urlsToGet); err != nil {
		return err
	}

	var imgs []string
	imgs = append(imgs, p.Cluster.PreloadImages...)
	// TODO: User defined preloaded images

	preloadImagesPath := filepath.Join(tmpdir, v1alpha1.ImagePreloadPath(p.Cluster.Driver), "hauler.tar")
	if err := p.pkgPreloadImages(ctx, preloadImagesPath, imgs); err != nil {
		return err
	}

	err = p.archive(ctx, tmpdir, outFile)
	if err != nil {
		return err
	}

	return nil
}

func (p *Packager) pkgFiles(ctx context.Context, dir string, rawUrls []string) error {
	logrus.Infof("Packaging %s driver artifacts...", p.Cluster.Driver.String())

	f := fetcher.FileFetcher{}
	for _, rawUrl := range rawUrls {
		dst := filepath.Join(dir, fetcher.GetFileNameFromURL(rawUrl))

		err := f.Get(ctx, rawUrl, dst)
		if err != nil {
			return err
		}
	}
	return nil
}

func buildDriverURLs(driver v1alpha1.Driver) []string {
	urls := make([]string, 0)

	urls = append(urls, driver.ExecutableURL())
	urls = append(urls, driver.ReleaseImagesURL())

	return urls
}

func (p *Packager) pkgPreloadImages(ctx context.Context, dir string, images []string) error {
	logrus.Infof("Packaging %d images to preload...", len(images))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	imageRefs := make(map[name.Reference]v1.Image)

	for _, i := range images {
		ref, err := name.ParseReference(i)
		if err != nil {
			return err
		}

		p.logger.Debugf("adding %s to preload image archive", ref.Name())
		img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
		if err != nil {
			return err
		}

		imageRefs[ref] = img
	}

	//	Get preload images and save to a single compressed tarball
	if err := tarball.MultiRefWriteToFile(dir, imageRefs); err != nil {
		return err
	}

	return nil
}

// archive will archive a folder using Packager's archiver while preserving the root folder structure
func (p *Packager) archive(ctx context.Context, dir string, outFile string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	err = os.Chdir(dir)
	if err != nil {
		return err
	}
	defer os.Chdir(cwd)


	archivePath := filepath.Join(cwd, outFile)
	p.logger.Infof("Archiving contents of %s to %s", dir, archivePath)
	if err := p.za.Archive([]string{"."}, archivePath); err != nil {
		return err
	}
	return nil
}

func (p *Packager) prepFS(dir string) error {
	for _, fs := range v1alpha1.FS(p.Cluster.Driver) {
		path := filepath.Join(dir, fs)

		p.logger.Debugf("creating directory: %s", path)
		err := os.MkdirAll(path, 0755)
		if err != nil {
			return fmt.Errorf("to create directory: %s, %w", path, err)
		}
	}
	return nil
}
