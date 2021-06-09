package packager

import (
	"context"
	"github.com/mholt/archiver/v3"
	fleetapi "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"github.com/rancher/fleet/pkg/bundle"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/fs"
	"k8s.io/apimachinery/pkg/util/json"
	"net/http"
	"path/filepath"
)

//Create will create a deployable package
func Create(ctx context.Context, p v1alpha1.Package, fsys fs.PkgFs, a archiver.Archiver) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}

	//TODO: Lol @ npm
	if err := fsys.WriteFile("package.json", data, 0644); err != nil {
		return err
	}

	opts := &bundle.Options{
		Compress: true,
	}

	//Get and write bundles to disk
	for _, path := range p.Spec.Paths {
		bundleName := filepath.Base(path)
		fb, err := bundle.Open(ctx, bundleName, path, "", opts)
		if err != nil {
			return err
		}

		//TODO: Figure out why bundle.Open doesn't return with GVK
		bn := fleetapi.NewBundle("fleet-local", bundleName, *fb.Definition)

		_, err = IdentifyImages(bn)
		if err != nil {
			return err
		}

		if err := fsys.AddBundle(bn); err != nil {
			return err
		}
	}

	d := v1alpha1.NewDriver(p.Spec.Driver.Kind)

	if err := writeURL(fsys, d.BinURL(), "k3s"); err != nil {
		return err
	}

	if err := writeURL(fsys, "https://get.k3s.io", "k3s-init.sh"); err != nil {
		return err
	}

	//TODO: Bad bad
	if err := fsys.AddChart("https://github.com/rancher/fleet/releases/download/v0.3.5/fleet-crd-0.3.5.tgz", "0.3.5"); err != nil {
		return err
	}

	if err := fsys.AddChart("https://github.com/rancher/fleet/releases/download/v0.3.5/fleet-0.3.5.tgz", "0.3.5"); err != nil {
		return err
	}

	imgMap, err := ConcatImages(d, p.Spec.Fleet)
	if err != nil {
		return err
	}

	for ref, im := range imgMap {
		err := fsys.AddImage(ref, im)
		if err != nil {
			return err
		}
	}

	return fsys.Archive(a, "hauler.tar.zst")
}

func writeURL(fsys fs.PkgFs, rawURL string, name string) error {
	resp, err := http.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return fsys.AddBin(resp.Body, name)
}
