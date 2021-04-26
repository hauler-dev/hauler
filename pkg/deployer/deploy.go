package deployer

import (
	"context"
	"github.com/mholt/archiver/v3"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/embed"
	"github.com/sirupsen/logrus"
	"io/fs"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"os/exec"
	"path/filepath"
)

type Deployer struct {
	Cluster *v1alpha1.Cluster
	logger *logrus.Entry
	za *archiver.TarZstd
}

func NewDeployer() *Deployer {
	return &Deployer{
		// TODO: Load this from config file
		Cluster: &v1alpha1.Cluster{
			TypeMeta:            metav1.TypeMeta{},
			Metadata:            metav1.ObjectMeta{},
			Driver:              v1alpha1.K3SDriver{
				Version:    "",
				ReleaseURL: "",
			},
			Arch:                "amd64",
			PreloadImages:       nil,
			AutodeployManifests: nil,
		},
		logger:  logrus.WithFields(logrus.Fields{
			"cluster": "",
			"driver": "",
			"stage:": "deployer",
		}),
		za: &archiver.TarZstd{
			Tar: &archiver.Tar{
				OverwriteExisting: true,
				MkdirAll: true,
			},
		},
	}
}

func (d *Deployer) Deploy(ctx context.Context, pkg string) error {
	d.logger.Infof("deploying cluster from %s", pkg)
	if err := d.explode(pkg); err != nil {
		return err
	}

	d.logger.Infof("configuring %s", d.Cluster.Driver.String())
	if err := d.config(); err != nil {
		return err
	}

	d.logger.Infof("starting %s", d.Cluster.Driver.String())
	if err := d.start(); err != nil {
		return err
	}
	return nil
}

func (d *Deployer) explode(pkg string) error {
	d.logger.Infof("extracting %s to %s", d.Cluster.Driver.String(), v1alpha1.DriverVarPath)
	if err := d.za.Extract(pkg, d.Cluster.Driver.String(), v1alpha1.DriverVarPath); err != nil {
		return err
	}

	// TODO: This walks through the compressed archive twice, loss in speed is minimal but it's still not optimal
	d.logger.Infof("extracting %s to %s", v1alpha1.HaulerBin, "/opt/hauler")
	if err := d.za.Extract(pkg, v1alpha1.HaulerBin, "/opt/hauler"); err != nil {
		return err
	}

	d.logger.Infof("rendering embedded files")
	if err := embed.RenderEmbedded(d.Cluster.Driver); err != nil {
		return err
	}

	d.logger.Debugf("ensuring everything in %s is executable", "/opt/hauler/bin")
	err := filepath.WalkDir("/opt/hauler/bin", func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			return err
		}
		err = os.Chmod(path, 0755)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

// config will create a driver config if not exists, and merge if one exists
func (d *Deployer) config() error {
	cfgFilePath := filepath.Join(v1alpha1.DriverEtcPath, d.Cluster.Driver.String(), "config.yaml")
	err := os.MkdirAll(filepath.Dir(cfgFilePath), 0666)
	if err != nil {
		return err
	}

	if _, err := os.ReadFile(cfgFilePath); os.IsNotExist(err) {
		data, _ := d.Cluster.Driver.MarshalConfig()
		err := os.WriteFile(cfgFilePath, data, 0666)
		if err != nil {
			return err
		}

	} else {
	//	TODO: Merge configs
	}

	return nil
}

func (d *Deployer) start() error {
	cmd := exec.Command("/bin/sh", "/opt/hauler/bin/k3s-init.sh")
	cmd.Env = append(os.Environ(), []string{
		"INSTALL_K3S_SKIP_DOWNLOAD=true",
		"INSTALL_K3S_SELINUX_WARN=true",
		"INSTALL_K3S_SKIP_SELINUX_RPM=true",
		"INSTALL_K3S_BIN_DIR=/opt/hauler/bin",
	}...)
	_, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}

	// TODO: Do this? or leave it up to user and print message
	//path := os.Getenv("PATH")
	//d.logger.Infof("adding /opt/hauler/bin to $PATH")
	//err = os.Setenv("PATH", fmt.Sprintf("%s:/opt/hauler/bin", path))
	//if err != nil {
	//	return err
	//}

	return nil
}
