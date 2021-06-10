package bootstrap

import (
	"context"
	"fmt"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/imdario/mergo"
	"github.com/otiai10/copy"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/fs"
	"helm.sh/helm/v3/pkg/chart/loader"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"os"
	"os/exec"
	"path/filepath"
	"sigs.k8s.io/yaml"
)

type Booter interface {
	Init() error
	PreBoot(context.Context) error
	Boot(context.Context, v1alpha1.Drive) error
	PostBoot(context.Context, v1alpha1.Drive) error
}

type booter struct {
	Package v1alpha1.Package
	fs fs.PkgFs
}

//NewBooter will build a new booter given a path to a directory containing a hauler package.json
func NewBooter(pkgPath string) (*booter, error) {
	pkg, err := v1alpha1.LoadPackageFromDir(pkgPath)
	if err != nil {
		return nil, err
	}

	fsys := fs.NewPkgFS(pkgPath)

	return &booter{
		Package: pkg,
		fs:     fsys,
	}, nil
}

func (b booter) Init() error {
	d := v1alpha1.NewDriver(b.Package.Spec.Driver.Kind)

	//TODO: Feel like there's a better way to do this
	if err := b.moveBin(); err != nil { return err }
	if err := b.moveImages(d); err != nil { return err }
	if err := b.moveBundles(d); err != nil { return err }
	if err := b.moveCharts(d); err != nil { return err }

	return nil
}

func (b booter) PreBoot(ctx context.Context, d v1alpha1.Drive) error {
	if err := b.writeConfig(d); err != nil {
		return err
	}

	return nil
}

func (b booter) Boot(ctx context.Context, d v1alpha1.Drive) error {
	//TODO: Generic
	cmd := exec.Command("/bin/sh", "/opt/hauler/bin/k3s-init.sh")

	cmd.Env	= append(os.Environ(), []string{
		"INSTALL_K3S_SKIP_DOWNLOAD=true",
		"INSTALL_K3S_SELINUX_WARN=true",
		"INSTALL_K3S_SKIP_SELINUX_RPM=true",
		"INSTALL_K3S_BIN_DIR=/opt/hauler/bin",
		//"INSTALL_K3S_SKIP_START=true",
	}...)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s\n%v", out, err)
	}

	//TODO: Figure out what to do with output

	return waitForDriver(ctx, d)
}

func (b booter) PostBoot(ctx context.Context, d v1alpha1.Drive) error {
	cf := genericclioptions.NewConfigFlags(true)
	cf.KubeConfig = stringptr(fmt.Sprintf("%s/k3s.yaml", d.EtcPath()))

	fleetCrdChartPath := b.fs.Chart().Path(fmt.Sprintf("fleet-crd-%s.tgz", b.Package.Spec.Fleet.Version))
	fleetCrdChart, err := loader.Load(fleetCrdChartPath)
	if err != nil {
		return err
	}

	fleetCrdRelease, fleetCrdErr := installChart(cf, fleetCrdChart, "fleet-crd", "fleet-system", nil)
	if fleetCrdErr != nil {
		return fleetCrdErr
	}

	fleetChartPath := b.fs.Chart().Path(fmt.Sprintf("fleet-%s.tgz", b.Package.Spec.Fleet.Version))
	fleetChart, err := loader.Load(fleetChartPath)
	if err != nil {
		return err
	}

	fleetRelease, fleetErr := installChart(cf, fleetChart, "fleet", "fleet-system", nil)
	if fleetErr != nil {
		return fleetErr
	}

	//TODO
	_ = fleetCrdRelease
	_ = fleetRelease

	return nil
}

//TODO: Move* will actually just copy. This is more expensive, but is much safer/easier at handling deep merges, should this change?
func (b booter) moveBin() error {
	path := filepath.Join("/opt/hauler/bin")
	if err := os.MkdirAll(path, os.ModePerm); err != nil {
		return err
	}

	return copy.Copy(b.fs.Bin().Path(), path)
}

func (b booter) moveImages(d v1alpha1.Drive) error {
	//NOTE: archives are not recursively searched, this _must_ be at the images dir
	path := filepath.Join(d.LibPath(), "agent/images")
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}

	refs, err := b.fs.MapLayout()
	if err != nil {
		return err
	}

	return tarball.MultiRefWriteToFile(filepath.Join(path, "hauler.tar"), refs)
}

func (b booter) moveBundles(d v1alpha1.Drive) error {
	path := filepath.Join(d.LibPath(), "server/manifests/hauler")
	if err := os.MkdirAll(d.LibPath(), 0700); err != nil {
		return err
	}

	return copy.Copy(b.fs.Bundle().Path(), path)
}

func (b booter) moveCharts(d v1alpha1.Drive) error {
	path := filepath.Join(d.LibPath(), "server/static/charts/hauler")
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}

	return copy.Copy(b.fs.Chart().Path(), path)
}

func (b booter) writeConfig(d v1alpha1.Drive) error {
	if err := os.MkdirAll(d.EtcPath(), os.ModePerm); err != nil {
		return err
	}

	c, err := d.Config()
	if err != nil {
		return err
	}

	var uc map[string]interface{}

	path := filepath.Join(d.EtcPath(), "config.yaml")
	if data, err := os.ReadFile(path); err != nil {
		err := yaml.Unmarshal(data, &uc)
		if err != nil {
			return err
		}
	}

	//Merge with user defined configs taking precedence
	if err := mergo.Merge(c, uc); err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	return os.WriteFile(path, data, 0644)
}
