package create

import (
	"context"
	"fmt"
	"github.com/rancherfederal/hauler/pkg/apis/driver"
	"github.com/rancherfederal/hauler/pkg/apis/haul"
	"github.com/rancherfederal/hauler/pkg/archive"
	"github.com/rancherfederal/hauler/pkg/fetcher"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rs/zerolog"
	"k8s.io/apimachinery/pkg/util/json"
	"os"
	"path/filepath"
)

type Creator struct {
	logger zerolog.Logger
}

func NewCreator(logger zerolog.Logger) (*Creator, error) {
	return &Creator{
		logger: logger,
	}, nil
}

func (c Creator) Create(ctx context.Context, h haul.Haul) error {
	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return err
	}
	defer os.Remove(tmpdir)

	if err := c.buildHaulLayout(h.Spec.Driver, tmpdir); err != nil {
		return err
	}

	if err := c.stamp(h.Spec.Driver, tmpdir); err != nil {
		return err
	}

	err = saveDriverExecutable(ctx, h.Spec.Driver, tmpdir, &c.logger)
	if err != nil {
		return err
	}

	archivePath := filepath.Join(tmpdir, h.Spec.Driver.PreloadImagesPath(), fmt.Sprintf("%s.tar", h.Metadata.Name))
	err = SavePreloadImages(ctx, h, archivePath, &c.logger)
	if err != nil {
		return err
	}

	a := archive.NewArchiver()
	err = archive.CompressAndArchive(a, tmpdir, h.Metadata.Name)
	if err != nil {
		return err
	}

	return nil
}

func (c Creator) stamp(d driver.Driver, dir string) error {
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}

	haulerCfgFile := filepath.Join(dir, "hauler.json")
	err = os.WriteFile(haulerCfgFile, data, 0644)
	if err != nil {
		return err
	}

	return nil
}

func (c Creator) buildHaulLayout(d driver.Driver, dir string) error {
	c.logger.Info().Msgf("building package layout in %s", dir)

	preloadImagesPath := filepath.Join(dir, d.PreloadImagesPath())
	c.logger.Debug().Msgf("Creating directory for preloaded images: %s", preloadImagesPath)
	if err := os.MkdirAll(preloadImagesPath, 0755); err != nil {
		return err
	}

	autodeployManifestsPath := filepath.Join(dir, d.AutodeployManifestsPath())
	c.logger.Debug().Msgf("Creating directory for autodeployed resources: %s", autodeployManifestsPath)
	if err := os.MkdirAll(autodeployManifestsPath, 0700); err != nil {
		return err
	}

	anonymousStaticPath := filepath.Join(dir, d.AnonymousStaticPath())
	c.logger.Debug().Msgf("Creating directory for content to host anonymously: %s", anonymousStaticPath)
	if err := os.MkdirAll(anonymousStaticPath, 0700); err != nil {
		return err
	}

	driverExecutablePath := filepath.Join(dir, driver.ExecutableBin)
	c.logger.Debug().Msgf("Creating directory for driver executable: %s", driverExecutablePath)
	if err := os.MkdirAll(driverExecutablePath, 0755); err != nil {
		return err
	}

	return nil
}

func saveDriverExecutable(ctx context.Context, d driver.Driver, dir string, logger log.Logger) error {
	logger.Info().Msgf("Fetching %s executable", d.Name())
	rawUrl := fmt.Sprintf("%s/%s", d.ReleaseArtifactsURL(), driver.K3sExecutable)

	f := fetcher.FileFetcher{}
	dst := filepath.Join(dir, driver.ExecutableBin, driver.K3sExecutable)

	err := f.Get(ctx, rawUrl, dst)
	if err != nil {
		return err
	}

	return nil
}
