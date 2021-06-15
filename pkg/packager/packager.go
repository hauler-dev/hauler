package packager

import (
	"context"
	fleetapi "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"github.com/rancher/fleet/pkg/bundle"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/fs"
	"github.com/rancherfederal/hauler/pkg/packager/images"
	"io"
	"k8s.io/apimachinery/pkg/util/json"
	"net/http"
	"path/filepath"
)

type Packager interface {
	Bundles(context.Context, ...string) ([]*fleetapi.Bundle, error)
	Driver(context.Context, v1alpha1.Drive) error
	Fleet(context.Context, v1alpha1.Fleet) error
	Archive(Archiver, v1alpha1.Package, string) error
}

type pkg struct {
	fs fs.PkgFs
}

//NewPackager loads a new packager given a path on disk
func NewPackager(path string) Packager {
	return pkg{
		fs: fs.NewPkgFS(path),
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

func (p pkg) Bundles(ctx context.Context, path ...string) ([]*fleetapi.Bundle, error) {
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

func (p pkg) Driver(ctx context.Context, d v1alpha1.Drive) error {
	if err := writeURL(p.fs, d.BinURL(), "k3s"); err != nil {
		return err
	}

	//TODO: Stop hardcoding
	if err := writeURL(p.fs, "https://get.k3s.io", "k3s-init.sh"); err != nil {
		return err
	}

	imgMap, err := images.MapImager(d)
	if err != nil {
		return err
	}

	for ref, im := range imgMap {
		err := p.fs.AddImage(ref, im)
		if err != nil {
			return err
		}
	}

	return nil
}

//TODO: Add this to Driver?
func (p pkg) Fleet(ctx context.Context, fl v1alpha1.Fleet) error {
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

func writeURL(fsys fs.PkgFs, rawURL string, name string) error {
	rc, err := fetchURL(rawURL)
	if err != nil {
		return err
	}
	defer rc.Close()

	return fsys.AddBin(rc, name)
}

func fetchURL(rawURL string) (io.ReadCloser, error) {
	resp, err := http.Get(rawURL)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, err
	}

	return resp.Body, nil
}
