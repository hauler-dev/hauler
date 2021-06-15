package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/otiai10/copy"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/driver"
	"github.com/rancherfederal/hauler/pkg/fs"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/sirupsen/logrus"
	"helm.sh/helm/v3/pkg/chart/loader"
	"io"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"os"
	"path/filepath"
)

type Booter interface {
	Init() error
	PreBoot(context.Context) error
	Boot(context.Context, driver.Driver) error
	PostBoot(context.Context, driver.Driver) error
}

type booter struct {
	Package v1alpha1.Package
	fs      fs.PkgFs
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
		fs:      fsys,
	}, nil
}

func (b booter) PreBoot(ctx context.Context, d driver.Driver, logger log.Logger) error {
	l := logger.WithFields(logrus.Fields{
		"phase": "preboot",
	})

	//TODO: Feel like there's a better way to do this
	if err := b.moveBin(); err != nil {
		return err
	}

	if err := b.moveImages(d); err != nil {
		return err
	}

	if err := b.moveBundles(d); err != nil {
		return err
	}

	if err := b.moveCharts(d); err != nil {
		return err
	}

	l.Infof("Creating driver configuration")
	if err := d.WriteConfig(); err != nil {
		return err
	}

	return nil
}

func (b booter) Boot(ctx context.Context, d driver.Driver, logger log.Logger) error {
	l := logger.WithFields(logrus.Fields{
		"phase": "boot",
	})

	var stdoutBuf, stderrBuf bytes.Buffer
	out := io.MultiWriter(os.Stdout, &stdoutBuf, &stderrBuf)

	err := d.Start(out)
	if err != nil {
		return err
	}

	l.Infof("Waiting for driver core components to provision...")
	waitErr := waitForDriver(ctx, d)
	if waitErr != nil {
		return err
	}

	return nil
}

func (b booter) PostBoot(ctx context.Context, d driver.Driver, logger log.Logger) error {
	l := logger.WithFields(logrus.Fields{
		"phase": "postboot",
	})

	cf := genericclioptions.NewConfigFlags(true)
	cf.KubeConfig = stringptr(d.KubeConfigPath())

	fleetCrdChartPath := b.fs.Chart().Path(fmt.Sprintf("fleet-crd-%s.tgz", b.Package.Spec.Fleet.Version))
	fleetCrdChart, err := loader.Load(fleetCrdChartPath)
	if err != nil {
		return err
	}

	l.Infof("Installing fleet crds")
	fleetCrdRelease, fleetCrdErr := installChart(cf, fleetCrdChart, "fleet-crd", "fleet-system", nil)
	if fleetCrdErr != nil {
		return fleetCrdErr
	}

	l.Infof("Successfully installed '%s' to namespace '%s'", fleetCrdRelease.Name, fleetCrdRelease.Namespace)

	fleetChartPath := b.fs.Chart().Path(fmt.Sprintf("fleet-%s.tgz", b.Package.Spec.Fleet.Version))
	fleetChart, err := loader.Load(fleetChartPath)
	if err != nil {
		return err
	}

	l.Infof("Installing fleet")
	fleetRelease, fleetErr := installChart(cf, fleetChart, "fleet", "fleet-system", nil)
	if fleetErr != nil {
		return fleetErr
	}

	l.Infof("Successfully installed '%s' to namespace '%s'", fleetRelease.Name, fleetRelease.Namespace)

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

func (b booter) moveImages(d driver.Driver) error {
	//NOTE: archives are not recursively searched, this _must_ be at the images dir
	path := d.DataPath("agent/images")
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}

	refs, err := b.fs.MapLayout()
	if err != nil {
		return err
	}

	return tarball.MultiRefWriteToFile(filepath.Join(path, "hauler.tar"), refs)
}

func (b booter) moveBundles(d driver.Driver) error {
	path := d.DataPath("server/manifests/hauler")
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return copy.Copy(b.fs.Bundle().Path(), path)
}

func (b booter) moveCharts(d driver.Driver) error {
	path := d.DataPath("server/static/charts/hauler")
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return copy.Copy(b.fs.Chart().Path(), path)
}
