package packager

import (
	"context"

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
	logger = logger.With(log.Fields{"dir": s.DataDir})
	return &packager{
		store:  s,
		logger: logger,
	}, nil
}

// AddPackage will create a package on disk by fetching all content referenced by the Package
// TODO: Refactor driverType into variadic options
func (r packager) AddPackage(ctx context.Context, haulerPackage v1alpha1.Package) error {
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

	r.logger.Infof("Successfully finished creating package")
	logger.Debugf("Shutting down content store")
	return nil
}

// Compress will archive and compress (zstd) the bundlers contents and output it to outputPath
// TODO: Just use mholt/archiver for now, even though we don't need most of it
func (r packager) Compress(ctx context.Context, outputPath string) error {
	logger := r.logger
	ctx = logger.WithContext(ctx)

	a := archiver.NewTarZstd()
	a.OverwriteExisting = true

	logger.Infof("Compressing and archiving directory %s to %s", r.store.DataDir, outputPath)
	return a.Archive([]string{
		r.store.DataDir,
	}, outputPath)
}

// AddDriver will add a driver to a package
func (r packager) AddDriver(ctx context.Context, kind string, version string) error {
	logger := r.logger
	ctx = logger.WithContext(ctx)

	logger.Debugf("Initializing content store")
	r.store.Start()
	defer r.store.Stop()

	d, err := driver.NewDriver(kind, version)
	if err != nil {
		return err
	}
	r.logger.Infof("Adding driver (%s) and dependencies to store", d.Name())

	var k3s v1alpha1.Getter
	k3s = v1alpha1.Http{
		Name: "k3s",
		Url:  d.BinaryFetchURL(),
	}

	binary, err := content.NewGeneric(k3s)
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
		// TODO: Configurable remote opts
		i := content.NewImage(img, remote.WithAuthFromKeychain(authn.DefaultKeychain))
		if err := r.store.Add(ctx, i); err != nil {
			return err
		}
	}

	return nil
}

// AddFleet will fetch the required fleet charts and images required and add it to the store, this is a helper wrapper around content.NewChart
//      This will fetch the chart CRD and chart based on the provided version and store it in the oci registry
func (r packager) AddFleet(ctx context.Context, version string) error {
	logger := r.logger
	ctx = logger.WithContext(ctx)

	logger.Debugf("Initializing content store")
	r.store.Start()
	defer r.store.Stop()

	flt := driver.NewFleet(version)

	logger.Debugf("Adding fleet CRD chart to store")
	fleetCRDRef := content.NewSystemRef(content.FleetCRDChartRef, flt.Version())
	fleetCrdChart := content.NewChart(fleetCRDRef, flt.CRDUrl())
	if err := r.store.Add(ctx, fleetCrdChart); err != nil {
		return err
	}

	logger.Debugf("Adding fleet chart to store")
	fleetRef := content.NewSystemRef(content.FleetChartRef, flt.Version())
	fleetChart := content.NewChart(fleetRef, flt.Url())
	if err := r.store.Add(ctx, fleetChart); err != nil {
		return err
	}

	return nil
}
