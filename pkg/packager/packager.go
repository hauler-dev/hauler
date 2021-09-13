package packager

import (
	"context"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	fleetapi "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"github.com/rancher/fleet/pkg/bundle"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/driver"
	"github.com/rancherfederal/hauler/pkg/fs"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/pkg/packager/images"
	"k8s.io/apimachinery/pkg/util/json"
	"path/filepath"
)

type Packager interface {
	Archive(Archiver, v1alpha1.Package, string) error

	PackageBundles(context.Context, ...string) ([]*fleetapi.Bundle, error)

	PackageDriver(context.Context, driver.Driver) error

	PackageFleet(context.Context, v1alpha1.Fleet) error

	PackageImages(context.Context, ...string) error
}

type pkg struct {
	fs fs.PkgFs

	logger log.Logger
}

//NewPackager loads a new packager given a path on disk
func NewPackager(path string, logger log.Logger) Packager {
	return pkg{
		fs:     fs.NewPkgFS(path),
		logger: logger,
	}
}

func (p pkg) Archive(a Archiver, pkg v1alpha1.Package, output string) error {
	data, err := json.Marshal(pkg)
	if err != nil {
		return err
	}

	if err = p.fs.WriteFile("package.json", data, 0644); err != nil {
		return err
	}

	return Package(a, p.fs.Path(), output)
}

func (p pkg) PackageBundles(ctx context.Context, path ...string) ([]*fleetapi.Bundle, error) {
	p.logger.Infof("Packaging %d bundle(s)", len(path))

	opts := &bundle.Options{
		Compress: true,
	}

	var cImgs int

	var bundles []*fleetapi.Bundle
	for _, pth := range path {
		p.logger.Infof("Creating bundle from path: %s", pth)

		bundleName := filepath.Base(pth)
		fb, err := bundle.Open(ctx, bundleName, pth, "", opts)
		if err != nil {
			return nil, err
		}
		//TODO: Figure out why bundle.Open doesn't return with GVK
		bn := fleetapi.NewBundle("fleet-local", bundleName, *fb.Definition)

		imgs, err := p.fs.AddBundle(bn)
		if err != nil {
			return nil, err
		}

		if err := p.pkgImages(ctx, imgs); err != nil {
			return nil, err
		}

		bundles = append(bundles, bn)
		cImgs += len(imgs)
	}

	p.logger.Infof("Finished packaging %d bundle(s) along with %d autodetected image(s)", len(path), cImgs)
	return bundles, nil
}

func (p pkg) PackageDriver(ctx context.Context, d driver.Driver) error {
	p.logger.Infof("Packaging %s components", d.Name())

	p.logger.Infof("Adding %s executable to package", d.Name())
	rc, err := d.Binary()
	if err != nil {
		return err
	}

	if err := p.fs.AddBin(rc, d.Name()); err != nil {
		return err
	}
	rc.Close()

	p.logger.Infof("Adding required images for %s to package", d.Name())
	imgMap, err := d.Images(ctx)
	if err != nil {
		return err
	}

	err = p.pkgImages(ctx, imgMap)
	if err != nil {
		return err
	}

	p.logger.Infof("Finished packaging %s components", d.Name())
	return nil
}

func (p pkg) PackageImages(ctx context.Context, imgs ...string) error {
	p.logger.Infof("Packaging %d user defined images", len(imgs))
	imgMap, err := images.ResolveRemoteRefs(imgs...)
	if err != nil {
		return err
	}

	if err := p.pkgImages(ctx, imgMap); err != nil {
		return err
	}

	p.logger.Infof("Finished packaging %d user defined images", len(imgs))
	return nil
}

//TODO: Add this to PackageDriver?
func (p pkg) PackageFleet(ctx context.Context, fl v1alpha1.Fleet) error {
	p.logger.Infof("Packaging fleet components")

	imgMap, err := images.MapImager(fl)
	if err != nil {
		return err
	}

	if err := p.pkgImages(ctx, imgMap); err != nil {
		return err
	}

	p.logger.Infof("Adding fleet crds to package")
	if err := p.fs.AddChart(fl.CRDChart(), fl.Version); err != nil {
		return err
	}

	p.logger.Infof("Adding fleet to package")
	if err := p.fs.AddChart(fl.Chart(), fl.Version); err != nil {
		return err
	}

	p.logger.Infof("Finished packaging fleet components")
	return nil
}

//pkgImages is a helper function to loop through an image map and add it to a layout
func (p pkg) pkgImages(ctx context.Context, imgMap map[name.Reference]v1.Image) error {
	var i int
	for ref, im := range imgMap {
		p.logger.Infof("Packaging image (%d/%d): %s", i+1, len(imgMap), ref.Name())
		if err := p.fs.AddImage(ref, im); err != nil {
			return err
		}
		i++
	}
	return nil
}
