package packager

import (
	"context"
	"fmt"
	"github.com/mholt/archiver/v3"
	"github.com/rancher/fleet/pkg/bundle"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/fs"
	"k8s.io/apimachinery/pkg/util/json"
	"net/http"
	"os"
	"path/filepath"
)

//Unpackage will do things
func Unpackage(ctx context.Context, a archiver.Archiver, source string, dest string) (v1alpha1.Package, error) {
	//err := a.Unarchive(source, dest)

	var p v1alpha1.Package

	data, err := os.ReadFile(filepath.Join(dest, "package.json"))
	err = json.Unmarshal(data, &p)

	return p, err
}

//Create will create a deployable package
func Create(ctx context.Context, p v1alpha1.Package, fsys fs.PkgFs, a archiver.Archiver) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}

	if err := fsys.WriteFile("package.json", data, 0644); err != nil {
		return err
	}

	opts := &bundle.Options{
		Compress: true,
	}

	//Get and write bundles to disk
	for _, path := range p.Spec.Paths {
		base := filepath.Base(path)
		b, err := bundle.Open(ctx, fmt.Sprintf("hauler-%s", base), path, "", opts)
		if err != nil {
			return err
		}

		if err := fsys.AddBundle(b.Definition); err != nil {
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

	images := append(p.Spec.Images, d.Images()...)

	//TODO: Smartly add fleet images
	images = append(images, []string{"rancher/gitjob:v0.1.15", "rancher/fleet:v0.3.5", "rancher/fleet-agent:v0.3.5"}...)

	for _, i := range images {
		err := fsys.AddImage(i)
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
