package fs

import (
	"fmt"
	"github.com/rancherfederal/hauler/pkg/packager/images"
	"io"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	fleetapi "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/spf13/afero"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/getter"
	"k8s.io/apimachinery/pkg/util/json"
)

type PkgFs struct {
	FS   *afero.BasePathFs
	root string
}

func NewPkgFS(dir string) PkgFs {
	var p PkgFs
	p.FS = afero.NewBasePathFs(afero.NewOsFs(), dir).(*afero.BasePathFs)

	// TODO: absolutely no way this'll bite us in the butt later...
	abs, _ := filepath.Abs(dir)
	p.root = abs
	return p
}

func (p PkgFs) Path(elem ...string) string {
	complete := []string{p.root}
	return filepath.Join(append(complete, elem...)...)
}

func (p PkgFs) Bundle() PkgFs {
	return PkgFs{
		FS:   afero.NewBasePathFs(p.FS, v1alpha1.BundlesDir).(*afero.BasePathFs),
		root: p.Path(v1alpha1.BundlesDir),
	}
}

func (p PkgFs) Image() PkgFs {
	return PkgFs{
		FS:   afero.NewBasePathFs(p.FS, v1alpha1.LayoutDir).(*afero.BasePathFs),
		root: p.Path(v1alpha1.LayoutDir),
	}
}

func (p PkgFs) Bin() PkgFs {
	return PkgFs{
		FS:   afero.NewBasePathFs(p.FS, v1alpha1.BinDir).(*afero.BasePathFs),
		root: p.Path(v1alpha1.BinDir),
	}
}

func (p PkgFs) Chart() PkgFs {
	return PkgFs{
		FS:   afero.NewBasePathFs(p.FS, v1alpha1.ChartDir).(*afero.BasePathFs),
		root: p.Path(v1alpha1.ChartDir),
	}
}

//AddBundle will add a bundle to a package and all images that are autodetected from it
func (p PkgFs) AddBundle(b *fleetapi.Bundle) error {
	if err := p.mkdirIfNotExists(v1alpha1.BundlesDir, os.ModePerm); err != nil {
		return err
	}

	data, err := json.Marshal(b)
	if err != nil {
		return err
	}

	if err := p.Bundle().WriteFile(fmt.Sprintf("%s.json", b.Name), data, 0644); err != nil {
		return err
	}

	imgs, err := images.ImageMapFromBundle(b)
	if err != nil {
		return err
	}

	for k, v := range imgs {
		err := p.AddImage(k, v)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p PkgFs) AddBin(r io.Reader, name string) error {
	if err := p.mkdirIfNotExists(v1alpha1.BinDir, os.ModePerm); err != nil {
		return err
	}

	f, err := p.Bin().FS.OpenFile(name, os.O_WRONLY|os.O_CREATE, 0755)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, r)
	return err
}

//AddImage will add an image to the pkgfs in OCI layout fmt
//TODO: Extra work is done to ensure this is unique within the index.json
func (p PkgFs) AddImage(ref name.Reference, img v1.Image) error {
	if err := p.mkdirIfNotExists(v1alpha1.LayoutDir, os.ModePerm); err != nil {
		return err
	}

	annotations := make(map[string]string)
	annotations[ocispec.AnnotationRefName] = ref.Name()

	lp, err := p.layout()
	if err != nil {
		return err
	}

	//TODO: Change to ReplaceImage
	return lp.AppendImage(img, layout.WithAnnotations(annotations))
}

//TODO: Not very robust
//For ref: https://github.com/helm/helm/blob/bf486a25cdc12017c7dac74d1582a8a16acd37ea/pkg/action/pull.go#L75
func (p PkgFs) AddChart(ref string, version string) error {
	if err := p.mkdirIfNotExists(v1alpha1.ChartDir, os.ModePerm); err != nil {
		return err
	}

	d := downloader.ChartDownloader{
		Out:     nil,
		Verify:  downloader.VerifyNever,
		Getters: getter.All(cli.New()), // TODO: Probably shouldn't do this...
		Options: []getter.Option{
			getter.WithInsecureSkipVerifyTLS(true),
		},
	}

	_, _, err := d.DownloadTo(ref, version, p.Chart().Path())
	return err
}

func (p PkgFs) layout() (layout.Path, error) {
	path := p.Image().Path(".")
	lp, err := layout.FromPath(path)
	if os.IsNotExist(err) {
		lp, err = layout.Write(path, empty.Index)
	}

	return lp, err
}

//WriteFile is a helper method to write a file within the PkgFs
func (p PkgFs) WriteFile(name string, data []byte, perm os.FileMode) error {
	f, err := p.FS.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err1 := f.Close(); err1 != nil && err == nil {
		err = err1
	}
	return err
}

func (p PkgFs) MapLayout() (map[name.Reference]v1.Image, error) {
	imgRefs := make(map[name.Reference]v1.Image)

	//TODO: Factor this out to a Store interface
	lp, err := p.layout()
	if err != nil {
		return nil, err
	}

	ii, _ := lp.ImageIndex()
	im, _ := ii.IndexManifest()

	for _, m := range im.Manifests {
		ref, err := name.ParseReference(m.Annotations[ocispec.AnnotationRefName])
		if err != nil {
			return nil, err
		}

		img, err := lp.Image(m.Digest)
		if err != nil {
			return nil, err
		}

		imgRefs[ref] = img
	}

	return imgRefs, err
}

//TODO: Is this actually faster than just os.MkdirAll?
func (p PkgFs) mkdirIfNotExists(dir string, perm os.FileMode) error {
	_, err := os.Stat(p.Path(dir))
	if os.IsNotExist(err) {
		mkdirErr := p.FS.MkdirAll(dir, perm)
		if mkdirErr != nil {
			return mkdirErr
		}
	}

	return nil
}
