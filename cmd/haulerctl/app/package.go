package app

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/archive"
	"github.com/rancherfederal/hauler/pkg/packager_new"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

const (
	packageConfigFileNameFlag    = "package-config"
	packageConfigFileNameDefault = ""
	outputFileNameFlag           = "output-file"
	outputFileNameShorthand      = "f"
	outputFileNameDefault        = "hauler-archive.tar.gz"
	outputFormatFlag             = "output-format"
)

func NewPackageCommand() *cobra.Command {
	opts := &PackageOptions{}

	cmd := &cobra.Command{
		Use:   "package",
		Short: "package all dependencies into an installable archive",
		Long: `package all dependencies into an archive used by deploy.

Container images, git repositories, and more, packaged and ready to be served within an air gap.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Preprocess(args); err != nil {
				return err
			}
			return opts.Run()
		},
	}

	// TODO - set EnvConfig options through CLI

	cmd.Flags().StringVar(&opts.PackageConfigFileName,
		packageConfigFileNameFlag, packageConfigFileNameDefault,
		"package config YAML used for creating archive",
	)
	// TODO - determine if OutputFileName should be positional arg or flag
	cmd.Flags().StringVarP(&opts.OutputFileName,
		outputFileNameFlag, outputFileNameShorthand, outputFileNameDefault,
		"specify the package's output location; - writes to stdout",
	)
	// TODO - improve usage message, dynamically populate all formats for easier future additions
	cmd.Flags().Var(&opts.OutputFormat,
		outputFormatFlag,
		"choose the format of the outputted archive (TarGz, Tar); if unset, will auto-complete based on "+outputFileNameFlag,
	)

	return cmd
}

type PackageOptions struct {
	PackageConfigFileName string
	OutputFileName        string
	OutputFormat          OutputFormat
	// ImageLists    []string
	// ImageArchives []string

	// completed options stored in the options struct
	co *completedPackageOptions
}

// TODO - decide if "frozen" options from PackageOptions should be stored in completedPackageOptions

type completedPackageOptions struct {
	PackageConfig     v1alpha1.PackageConfig
	OutputArchiveKind archive.Kind

	Dst io.Writer
}

// Preprocess infers any remaining options and performs any required validation.
func (o *PackageOptions) Preprocess(_ []string) error {
	// TODO - perform as much validation as possible and return error containing all known issues

	co := &completedPackageOptions{}

	if o.PackageConfigFileName == "" {
		return errors.New("package config is required")
	}
	if o.OutputFileName == "" {
		return errors.New("output file is required")
	}
	if o.OutputFileName == "-" && o.OutputFormat == UnknownFormat {
		return errors.New("must specify a format when outputting to stdout")
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

	if o.OutputFileName == "-" {
		co.Dst = os.Stdout
	} else {
		if dstFile, err := os.Create(o.OutputFileName); err != nil {
			return fmt.Errorf(
				"couldn't create output file %s: %v",
				o.OutputFileName, err,
			)
		} else {
			co.Dst = dstFile
		}
	}

	// TODO - improve scalability of format auto-detection
	switch {
	case o.OutputFileName == "-" || o.OutputFormat != UnknownFormat:
		co.OutputArchiveKind = o.OutputFormat.ToArchiveKind()
	case strings.HasSuffix(o.OutputFileName, ".tar"):
		co.OutputArchiveKind = archive.KindTar
	case strings.HasSuffix(o.OutputFileName, ".tar.gz") || strings.HasSuffix(o.OutputFileName, ".tgz"):
		co.OutputArchiveKind = archive.KindTarGz
	default:
		return errors.New("unable to determine output format, please specify flag or allow auto-detection by using known file type")
	}

	o.co = co
	return nil
}

// Run performs the operation.
func (o *PackageOptions) Run() error {
	if o.co == nil {
		return errors.New("PackageOptions must be preprocessed before Run is called")
	}

	// TODO - set EnvConfig options through CLI
	p := packager.New(nil)
	// TODO - use o.co.OutputArchiveKind
	if err := p.Package(o.co.Dst, o.co.PackageConfig); err != nil {
		return err
	}

	return nil
}
