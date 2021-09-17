package packager

import (
	"context"
	"path"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/mholt/archiver/v3"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/content"
	"github.com/rancherfederal/hauler/pkg/driver"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/store"
)

type packager struct {
	store  *store.Store
	logger log.Logger
}

func NewPackager(s *store.Store, logger log.Logger) (*packager, error) {
	logger = logger.With(log.Fields{"store_path": s.DataDir})
	return &packager{
		store:  s,
		logger: logger,
	}, nil
}

// Create will create a package on disk by fetching all content referenced by the Package
// TODO: Refactor driverType into variadic options
func (r packager) Create(ctx context.Context, driverType string, haulerPackage v1alpha1.Package) error {
	logger := r.logger.With(log.Fields{"package": haulerPackage.Name})
	ctx = logger.WithContext(ctx)

	logger.Debugf("Initializing content store")
	r.store.Start()
	defer r.store.Stop()

	logger.Infof("Building package: %s", haulerPackage.Name)
	p, err := content.NewPackage(ctx, haulerPackage)
	if err != nil {
		return err
	}

	logger.Infof("Adding package %s to store", haulerPackage.Name)
	if err := r.store.Add(ctx, p); err != nil {
		return err
	}

	if driverType == "" {
		r.logger.Warnf("No driver specified, not packaging any driver components")

	} else {
		d := driver.NewDriver(driverType, "")
		r.logger.Infof("Adding driver (%s) and dependencies to store", d.Name())
		if err := r.addDriver(ctx, d); err != nil {
			return err
		}
	}

	r.logger.Infof("Successfully finished creating package")
	logger.Debugf("Shutting down content store")
	return nil
}

// Compress will archive and compress (zstd) the bundlers contents and output it to outputPath
// TODO: Just use mholt/archiver for now, even though we don't need most of it
func (r packager) Compress(ctx context.Context, outputPath string) error {
	a := archiver.NewTarZstd()
	a.OverwriteExisting = true

	return a.Archive([]string{
		r.store.DataDir,
	}, outputPath)
}

// addDriver will add the driver and all it's dependencies to the content store
func (r packager) addDriver(ctx context.Context, d driver.Driver) error {
	ref := path.Join(store.HaulerRepo, d.Name())
	binary, err := content.NewGeneric(ref, d.BinaryFetchURL())
	if err != nil {
		return err
	}

	r.logger.Debugf("Adding driver (%s) binary to store", d.Name())
	if err := r.store.Add(ctx, binary); err != nil {
		return err
	}

	r.logger.Debugf("Adding driver (%s) dependent images to store", d.Name())
	imgs, err := d.Images(ctx)
	for _, img := range imgs {
		i := content.NewImage(img, remote.WithAuthFromKeychain(authn.DefaultKeychain))
		if err := r.store.Add(ctx, i); err != nil {
			return err
		}
	}

	return nil
}
