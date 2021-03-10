package app

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/rancherfederal/hauler/pkg/packager"

	"github.com/spf13/cobra"
)

const (
	outputFileNameFlag    = "out-file"
	outputFileNameDefault = "hauler-package.tar.gz"
)

func NewPackageCommand() *cobra.Command {
	opts := &PackageOptions{}

	cmd := &cobra.Command{
		Use:   "package",
		Short: "package all dependencies into an installable archive",
		Long: `package all dependencies into an archive used by deploy.

Container images, git repositories, and more, packaged and ready to be served within an air-gap.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Preprocess(); err != nil {
				return err
			}
			return opts.Run()
		},
	}

	cmd.Flags().StringVar(
		&opts.OutputFileName, outputFileNameFlag, outputFileNameDefault,
		"specify the package's output location; '-' writes to standard out",
	)

	return cmd
}

type PackageOptions struct {
	OutputFileName string
	// ImageLists    []string
	// ImageArchives []string
}

// Preprocess infers any remaining options and performs any required validation.
func (o *PackageOptions) Preprocess() error {
	if o.OutputFileName == "" {
		return errors.New("output file is required")
	}
	return nil
}

const (
	k3sVersion = "v1.19.5+k3s1"
)

// Run performs the operation.
func (o *PackageOptions) Run() error {
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

	pconfig := packager.Config{
		Destination:       dst,
		KubernetesVersion: "k3s:" + k3sVersion,
	}

	p, err := packager.New(pconfig)
	if err != nil {
		return fmt.Errorf("initialize packager: %v", err)
	}

	if err := p.Run(); err != nil {
		log.Fatalln(err)
	}

	return nil
}
