package fs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/mholt/archiver/v3"
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

func (p *PkgFs) Archive(a archiver.Archiver, out string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(cwd)

	err = os.Chdir(p.root)
	if err != nil {
		return err
	}

	return a.Archive([]string{"."}, filepath.Join(cwd, out))
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

func (p PkgFs) AddBundle(b *fleetapi.Bundle) error {
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return p.Bundle().WriteFile(fmt.Sprintf("%s.json", b.Name), data, 0644)
}

func (p PkgFs) AddBin(r io.Reader, name string) error {
	f, err := p.Bin().FS.OpenFile(name, os.O_WRONLY|os.O_CREATE, 0755)
	_, err = io.Copy(f, r)
	return err
}

//AddImage will add an image to the pkgfs in OCI layout fmt
//TODO: Extra work is done to ensure this is unique within the index.json
func (p PkgFs) AddImage(ref name.Reference, img v1.Image) error {
	annotations := make(map[string]string)
	annotations[ocispec.AnnotationRefName] = ref.Name()

	lp, err := p.layout()
	if err != nil {
		return err
	}

	//TODO: Change to ReplaceImage
	return lp.AppendImage(img, layout.WithAnnotations(annotations))
}

//TODO: Fully aware this _only_ works for fleet right now, refactor this to use downloader.ChartDownloader
//For ref: https://github.com/helm/helm/blob/bf486a25cdc12017c7dac74d1582a8a16acd37ea/pkg/action/pull.go#L75
func (p PkgFs) AddChart(ref string, version string) error {
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

//TODO: Init feels too primitive, should this just be added to object factory?
func (p *PkgFs) Init() error {
	if err := p.FS.Mkdir(v1alpha1.BundlesDir, os.ModePerm); err != nil {
		return err
	}
	if err := p.FS.Mkdir(v1alpha1.LayoutDir, os.ModePerm); err != nil {
		return err
	}
	if err := p.FS.Mkdir(v1alpha1.BinDir, os.ModePerm); err != nil {
		return err
	}
	if err := p.FS.Mkdir(v1alpha1.ChartDir, os.ModePerm); err != nil {
		return err
	}
	return nil
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
