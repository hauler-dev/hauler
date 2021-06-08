package bootstrap

import (
	"context"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/fs"
	"github.com/sirupsen/logrus"
	"os"
	"os/exec"
	"sigs.k8s.io/yaml"
)

func Boot(ctx context.Context, p v1alpha1.Package, fsys fs.PkgFs) error {
	d := v1alpha1.NewDriver(p.Spec.Driver.Kind)
	_ = d

	if err := fsys.MoveBin(); err != nil {
		return err
	}

	if err := fsys.MoveBundle(); err != nil {
		return err
	}

	if err := fsys.MoveImage(); err != nil {
		return err
	}

	if err := config(ctx, d); err != nil {
		return err
	}

	out, err := start(ctx, d)
	if err != nil {
		return err
	}

	//TODO: Log better
	logrus.Infof(string(out))

	return nil
}

//config will write out the driver config to the appropriate location and merge with anything already there
func config(ctx context.Context, d v1alpha1.Drive) error {
	c := make(map[string]interface{})
	c["write-kubeconfig-mode"] = 0644

	//TODO: Randomize this name so multi-node works
	c["node-name"] = "hauler"

	//TODO: Lazy
	if err := os.MkdirAll("/etc/rancher/k3s", os.ModePerm); err != nil {
		return err
	}

	//Merge anything existing
	if data, err := os.ReadFile(d.ConfigFile()); err == nil {
		// file exists
		err := yaml.Unmarshal(data, &c)
		if err != nil {
			return err
		}
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(d.ConfigFile(), data, 0644)
}

//start will start the cluster using the appropriate driver
func start(ctx context.Context, d v1alpha1.Drive) ([]byte, error) {
	cmd := exec.Command("/bin/sh", "/opt/hauler/bin/k3s-init.sh")

	//General rule of thumb is keep as much configuration in config.yaml as possible, only set script args here
	cmd.Env	= append(os.Environ(), []string{
		"INSTALL_K3S_SKIP_DOWNLOAD=true",
		"INSTALL_K3S_SELINUX_WARN=true",
		"INSTALL_K3S_SKIP_SELINUX_RPM=true",
		"INSTALL_K3S_BIN_DIR=/opt/hauler/bin",
		"INSTALL_K3S_SKIP_START=true",
	}...)

	return cmd.CombinedOutput()
}