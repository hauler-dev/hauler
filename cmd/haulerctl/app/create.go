package app

import (
	"context"
	"github.com/mholt/archiver/v3"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/fs"
	"github.com/rancherfederal/hauler/pkg/packager"
	"github.com/spf13/cobra"
	"os"
	"sigs.k8s.io/yaml"
)

type createOpts struct {
	driver            string
	outputFile        string
	configFile string
}

// NewCreateCommand creates a new sub command under
// haulerctl  for creating dependency artifacts for bootstraps
func NewCreateCommand() *cobra.Command {
	opts := &createOpts{}

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
	f.StringVarP(&opts.outputFile, "output", "o", "haul.tar.zst",
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := os.Stat(o.configFile); err != nil {
		return err
	}

	bundleData, err := os.ReadFile(o.configFile)
	if err != nil {
		return err
	}

	var p v1alpha1.Package
	err = yaml.Unmarshal(bundleData, &p)
	if err != nil {
		return err
	}

	fsys, tmpdir, err := tmpFS()
	if err != nil {
		return err
	}

	defer os.RemoveAll(tmpdir)

	if err := fsys.Init(); err != nil {
		return err
	}

	z := newTarZstd()

	err = packager.Create(ctx, p, fsys, z)
	if err != nil {
		return err
	}

	return nil
}

func tmpFS() (fs.PkgFs, string, error) {
	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return fs.PkgFs{}, "", err
	}

	return fs.NewPkgFS(tmpdir), tmpdir, nil
}

func newTarZstd() *archiver.TarZstd {
	return &archiver.TarZstd{
		Tar: &archiver.Tar{
			OverwriteExisting:      true,
			MkdirAll:               true,
			ImplicitTopLevelFolder: false,
			StripComponents:        0,
			ContinueOnError:        false,
		},
	}
}