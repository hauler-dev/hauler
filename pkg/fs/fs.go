package fs

import (
	"fmt"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/mholt/archiver/v3"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	fleetapi "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/spf13/afero"
	"io"
	"k8s.io/apimachinery/pkg/util/json"
	"os"
	"path/filepath"
	"github.com/otiai10/copy"
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

func (p PkgFs) path(elem ...string) string {
	complete := []string{p.root}
	return filepath.Join(append(complete, elem...)...)
}

func (p PkgFs) Bundle() PkgFs {
	return PkgFs{
		FS:   afero.NewBasePathFs(p.FS, v1alpha1.BundlesDir).(*afero.BasePathFs),
		root: p.path(v1alpha1.BundlesDir),
	}
}

func (p PkgFs) Image() PkgFs {
	return PkgFs{
		FS:   afero.NewBasePathFs(p.FS, v1alpha1.LayoutDir).(*afero.BasePathFs),
		root: p.path(v1alpha1.LayoutDir),
	}
}

func (p PkgFs) Bin() PkgFs {
	return PkgFs{
		FS:   afero.NewBasePathFs(p.FS, v1alpha1.BinDir).(*afero.BasePathFs),
		root: p.path(v1alpha1.BinDir),
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
func (p PkgFs) AddImage(image string) error {
	ref, err := name.ParseReference(image)
	if err != nil {
		return err
	}

	annotations := make(map[string]string)
	annotations[ocispec.AnnotationRefName] = ref.Name()

	img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return err
	}

	lp, err := p.layout()
	if err != nil {
		return err
	}

	//TODO: Change to ReplaceImage
	return lp.AppendImage(img, layout.WithAnnotations(annotations))
}

func (p PkgFs) layout() (layout.Path, error) {
	path := p.Image().path(".")
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

//TODO: Move* will actually just copy. This is more expensive, but is much safer/easier at handling deep merges, should this change?
func (p PkgFs) MoveBin() error {
	if err := os.MkdirAll("/opt/hauler", os.ModePerm); err != nil {
		return err
	}

	return copy.Copy(p.Bin().path(), "/opt/hauler/bin")
}

func (p PkgFs) MoveBundle() error {
	//TODO: Generic
	if err := os.MkdirAll("/var/lib/rancher/k3s/server/manifests/hauler", os.ModePerm); err != nil {
		return err
	}

	return copy.Copy(p.Bundle().path(), "/var/lib/rancher/k3s/server/manifests/hauler")
}

func (p PkgFs) MoveImage() error {
	//TODO: Generic
	if err := os.MkdirAll("/var/lib/rancher/k3s/agent/images/hauler", os.ModePerm); err != nil {
		return err
	}

	//TODO: Factor this out to a Store interface
	lp, _ := p.layout()
	ii, _ := lp.ImageIndex()
	im, _ := ii.IndexManifest()

	imgRefs := make(map[name.Reference]v1.Image)

	for _, m := range im.Manifests {
		ref, err := name.ParseReference(m.Annotations[ocispec.AnnotationRefName])
		if err != nil {
			return err
		}

		img, err := lp.Image(m.Digest)
		if err != nil {
			return err
		}

		imgRefs[ref] = img
	}

	//TODO: Would be great if k3s/containerd could load directly from an OCI layout
	return tarball.MultiRefWriteToFile("/var/lib/rancher/k3s/agent/images/hauler/hauler.tar", imgRefs)
}
