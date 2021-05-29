package bootstrap

import (
	"context"
	"embed"
	"fmt"
	"github.com/rancherfederal/hauler/pkg/apis/driver"
	"github.com/rancherfederal/hauler/pkg/apis/haul"
	"github.com/rancherfederal/hauler/pkg/util"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed bin/*
var e embed.FS

type bootstrapper struct {
	haul haul.Haul
	dir string
}

func NewBootstrapper(h haul.Haul, haulerDir string) *bootstrapper {
	return &bootstrapper{
		haul: h,
		dir: haulerDir,
	}
}

func (b bootstrapper) Bootstrap(ctx context.Context) error {
	if err := b.renderEmbeddedDriverInit(); err != nil {
		return err
	}

	if err := b.createLayout(); err != nil {
		return err
	}

	for _, bndl := range b.haul.Spec.Bundles {
		bundleBasePath := filepath.Join(b.dir, "bundles", bndl.Name)
		err := bndl.Setup(b.haul.Spec.Driver, bundleBasePath)
		if err != nil {
			return err
		}
	}

	err := StartDriver(b.haul.Spec.Driver)
	if err != nil {
		return err
	}

	return nil
}

func (b bootstrapper) renderEmbeddedDriverInit() error {
	err := fs.WalkDir(e, "bin", func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			return nil
		}

		if strings.Contains(path, b.haul.Spec.Driver.Name()) {
			data, err := fs.ReadFile(e, path)
			if err != nil {
				return err
			}

			renderedFile := filepath.Join(b.dir, path)
			err = os.WriteFile(renderedFile, data, 0755)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

func (b bootstrapper) createLayout() error {
	l := util.NewLayout(b.haul.Spec.Driver.VarPath())
	l.AddDir("server/static", 0700)
	l.AddDir("server/manifests", 0755)
	l.AddDir("agent/images", 0755)
	return l.Create()
}

func StartDriver(d driver.Driver) error {
	cmd := exec.Command("/bin/sh", "/opt/hauler/bin/k3s-init.sh")
	cmd.Env = append(os.Environ(), []string{
		"INSTALL_K3S_SKIP_DOWNLOAD=true",
		"INSTALL_K3S_SELINUX_WARN=true",
		"INSTALL_K3S_SKIP_SELINUX_RPM=true",
		"INSTALL_K3S_BIN_DIR=/opt/hauler/bin",
	}...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("with driver: %s\n%v", out, err)
	}

	fmt.Println(string(out))

	return nil
}