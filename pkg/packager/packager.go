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

	PackageImages(context.Context, map[name.Reference]v1.Image) error
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
	opts := &bundle.Options{Compress: true}

	var bundles []*fleetapi.Bundle
	for _, pth := range path {
		bundleName := filepath.Base(pth)
		fb, err := bundle.Open(ctx, bundleName, pth, "", opts)
		if err != nil {
			return nil, err
		}
		//TODO: Figure out why bundle.Open doesn't return with GVK
		bn := fleetapi.NewBundle("fleet-local", bundleName, *fb.Definition)

		if err := p.fs.AddBundle(bn); err != nil {
			return nil, err
		}

		bundles = append(bundles, bn)
	}

	return bundles, nil
}

func (p pkg) PackageDriver(ctx context.Context, d driver.Driver) error {
	rc, err := d.Binary()
	if err != nil {
		return err
	}

	if err := p.fs.AddBin(rc, d.Name()); err != nil {
		return err
	}
	rc.Close()

	imgMap, err := d.Images(ctx)
	if err != nil {
		return err
	}

	err = p.PackageImages(ctx, imgMap)
	if err != nil {
		return err
	}

	return nil
}

func (p pkg) PackageImages(ctx context.Context, imgMap map[name.Reference]v1.Image) error {
	for ref, im := range imgMap {
		if err := p.fs.AddImage(ref, im); err != nil {
			return err
		}
	}
	return nil
}

//TODO: Add this to PackageDriver?
func (p pkg) PackageFleet(ctx context.Context, fl v1alpha1.Fleet) error {
	imgMap, err := images.MapImager(fl)
	if err != nil {
		return err
	}

	for ref, im := range imgMap {
		err := p.fs.AddImage(ref, im)
		if err != nil {
			return err
		}
	}

	if err := p.fs.AddChart(fl.CRDChart(), fl.Version); err != nil {
		return err
	}

	if err := p.fs.AddChart(fl.Chart(), fl.Version); err != nil {
		return err
	}

	return nil
}
