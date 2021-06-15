package app

import (
	"context"
	"os"

	"github.com/pterm/pterm"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/packager"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

type createOpts struct {
	*rootOpts

	driver     string
	outputFile string
	configFile string
}

// NewCreateCommand creates a new sub command under
// haulerctl for creating dependency artifacts for bootstraps
func NewCreateCommand() *cobra.Command {
	opts := &createOpts{
		rootOpts: &ro,
	}

	cmd := &cobra.Command{
		Use:   "create",
		Short: "package all dependencies into a compressed archive",
		Long: `package all dependencies into a compressed archive used by deploy.

Container images, git repositories, and more, packaged and ready to be served within an air gap.`,
		Aliases: []string{"c"},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.PreRun()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.Run()
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.driver, "driver", "d", "k3s",
		"Driver type to use for package (k3s or rke2)")
	f.StringVarP(&opts.outputFile, "output", "o", "haul",
		"package output location relative to the current directory (haul.tar.zst)")
	f.StringVarP(&opts.configFile, "config", "c", "./package.yaml",
		"config file")

	return cmd
}

func (o *createOpts) PreRun() error {
	return nil
}

// Run performs the operation.
func (o *createOpts) Run() error {
	o.logger.Infof("Creating new deployable bundle using driver: %s", o.driver)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := os.Stat(o.configFile); err != nil {
		logrus.Error(err)
	}

	bundleData, err := os.ReadFile(o.configFile)
	if err != nil {
		logrus.Error(err)
	}

	var p v1alpha1.Package
	err = yaml.Unmarshal(bundleData, &p)
	if err != nil {
		logrus.Error(err)
	}

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		logrus.Error(err)
	}
	defer os.RemoveAll(tmpdir)

	pkgr := packager.NewPackager(tmpdir)

	pb, _ := pterm.DefaultProgressbar.WithTotal(4).WithTitle("Start Packaging").Start()

	o.logger.Infof("Packaging driver (%s %s) artifacts...", p.Spec.Driver.Version, p.Spec.Driver.Kind)
	d := v1alpha1.NewDriver(p.Spec.Driver.Kind)
	if err = pkgr.Driver(ctx, d); err != nil {
		logrus.Error(err)
	}
	pb.Increment()

	o.logger.Infof("Packaging fleet artifacts...")
	if err = pkgr.Fleet(ctx, p.Spec.Fleet); err != nil {
		logrus.Error(err)
	}
	pb.Increment()

	o.logger.Infof("Packaging images and manifests defined in specified paths...")
	if _, err = pkgr.Bundles(ctx, p.Spec.Paths...); err != nil {
		logrus.Error(err)
	}
	pb.Increment()

	a := packager.NewArchiver()
	o.logger.Infof("Archiving and compressing package to: %s.%s", o.outputFile, a.String())
	if err = pkgr.Archive(a, p, o.outputFile); err != nil {
		logrus.Error(err)
	}
	pb.Increment()

	return nil
}
