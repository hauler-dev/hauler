package create

import (
	"context"
	"fmt"
	"github.com/mholt/archiver/v3"
	"github.com/rancherfederal/hauler/pkg/apis/bundle"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"

	"github.com/rancherfederal/hauler/pkg/apis/driver"
	"github.com/rancherfederal/hauler/pkg/apis/haul"
	"github.com/rancherfederal/hauler/pkg/fetcher"
)

type Creator struct{}

func NewCreator() (*Creator, error) {
	return &Creator{}, nil
}

func (c Creator) Create(ctx context.Context, h haul.Haul) error {
	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return err
	}
	defer os.Remove(tmpdir)

	layout := h.CreateLayout(tmpdir)
	if err := layout.Create(); err != nil {
		return err
	}

	err = saveDriverExecutable(ctx, h.Spec.Driver, tmpdir)
	if err != nil {
		return err
	}

	for _, b := range h.Spec.Bundles {
		logrus.Infof("Packaging bundle %s", b.Name)

		err = b.ResolveBundleFromPath()
		if err != nil {
			return err
		}

		bundlePath := filepath.Join(tmpdir, "bundles", b.Name)
		bl := b.CreateLayout(bundlePath)
		if err := bl.Create(); err != nil {
			return err
		}

		imagesPath := filepath.Join(bundlePath, bundle.ImagePreloadDirectory, fmt.Sprintf("%s.tar", b.Name))
		err = SavePreloadImages(ctx, b.Images, imagesPath)
		if err != nil {
			return err
		}
	}

	if data, err := yaml.Marshal(h); err != nil {
		return err
	} else {
		haulerConfigPath := filepath.Join(tmpdir, "hauler.yaml")
		err = os.WriteFile(haulerConfigPath, data, os.ModePerm)
		if err != nil {
			return err
		}
	}

	zstd := archiver.NewTarZstd()
	zstd.OverwriteExisting = true
	err = layout.Archive(zstd, h.Metadata.Name)
	if err != nil {
		return err
	}

	return nil
}

func saveDriverExecutable(ctx context.Context, d driver.Driver, dir string) error {
	rawUrl := fmt.Sprintf("%s/%s", d.ReleaseArtifactsURL(), d.Name())

	f := fetcher.FileFetcher{}
	dst := filepath.Join(dir, driver.ExecutableBin, driver.K3sExecutable)

	err := f.Get(ctx, rawUrl, dst)
	if err != nil {
		return err
	}

	err = os.Chmod(dst, 0755)
	if err != nil {
		return err
	}

	return nil
}
