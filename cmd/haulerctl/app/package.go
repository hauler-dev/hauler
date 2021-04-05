package app

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/archive"
	"github.com/rancherfederal/hauler/pkg/packager_new"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

const (
	packageConfigFileNameFlag    = "package-config"
	packageConfigFileNameDefault = ""
	outputFileNameFlag           = "out-file"
	outputFileNameDefault        = "hauler-archive.tar.gz"
)

func NewPackageCommand() *cobra.Command {
	opts := &PackageOptions{}

	cmd := &cobra.Command{
		Use:   "package",
		Short: "package all dependencies into an installable archive",
		Long: `package all dependencies into an archive used by deploy.

Container images, git repositories, and more, packaged and ready to be served within an air gap.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Preprocess(); err != nil {
				return err
			}
			return opts.Run()
		},
	}

	// TODO - set EnvConfig options through CLI

	cmd.Flags().StringVar(
		&opts.PackageConfigFileName, packageConfigFileNameFlag, packageConfigFileNameDefault,
		"package config YAML used for creating archive",
	)
	cmd.Flags().StringVar(
		&opts.OutputFileName, outputFileNameFlag, outputFileNameDefault,
		"specify the package's output location; '-' writes to standard out",
	)

	return cmd
}

type PackageOptions struct {
	PackageConfigFileName string
	OutputFileName        string
	// ImageLists    []string
	// ImageArchives []string

	// completed options stored in the options struct
	co *completedPackageOptions
}

type completedPackageOptions struct {
	PackageConfig     v1alpha1.PackageConfig
	OutputFileName    string
	OutputArchiveKind archive.WriterKind
}

// Preprocess infers any remaining options and performs any required validation.
func (o *PackageOptions) Preprocess() error {
	// TODO - perform as much validation as possible and return error containing all known issues

	co := &completedPackageOptions{}

	if o.PackageConfigFileName == "" {
		return errors.New("package config is required")
	}
	if o.OutputFileName == "" {
		return errors.New("output file is required")
	}

	pconfigBytes, err := ioutil.ReadFile(o.PackageConfigFileName)
	if err != nil {
		return fmt.Errorf(
			"couldn't read package config file %s: %v",
			o.PackageConfigFileName, err,
		)
	}

	pconfig := v1alpha1.PackageConfig{}
	if err := yaml.Unmarshal(pconfigBytes, &pconfig); err != nil {
		return fmt.Errorf(
			"couldn't parse %s as a PackageConfig: %v",
			o.PackageConfigFileName, err,
		)
	}

	co.PackageConfig = pconfig

	o.co = co
	return nil
}

// Run performs the operation.
func (o *PackageOptions) Run() error {
	if o.co == nil {
		return errors.New("package options must be preprocessed before Run is called")
	}

	var dst io.Writer
	if o.OutputFileName == "-" {
		dst = os.Stdout
	} else {
		dstFile, err := os.Create(o.OutputFileName)
		if err != nil {
			return fmt.Errorf("create output file: %v", err)
		}
		dst = dstFile
	}

	// TODO - set EnvConfig options through CLI
	p := packager.New(nil)

	if err := p.Package(dst, o.co.PackageConfig); err != nil {
		return err
	}

	return nil
}
