package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rancherfederal/hauler/pkg/archive"

	"github.com/spf13/cobra"
)

const (
	archiveFileNameFlag      = "archive-file"
	archiveFileNameShorthand = "f"
	archiveFileNameDefault   = ""
	archiveFormatFlag        = "archive-format"
)

func NewDeployCommand() *cobra.Command {
	opts := &DeployOptions{}

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "deploy all dependencies from a generated package",
		Long: `deploy all dependencies from a generated package.

Given an archive generated from the package command, deploy all needed
components to serve packaged dependencies.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Preprocess(args); err != nil {
				return err
			}
			return opts.Run()
		},
	}

	cmd.Flags().StringVarP(&opts.ArchiveFileName,
		archiveFileNameFlag, archiveFileNameShorthand, archiveFileNameDefault,
		"specify the archive to deploy; - reads from stdin",
	)
	cmd.Flags().Var(&opts.ArchiveFormat,
		archiveFormatFlag,
		"specify the format of the target archive (TarGZ, Tar); if unset, will auto-complete based on "+archiveFileNameFlag,
	)

	return cmd
}

type DeployOptions struct {
	ArchiveFileName string
	ArchiveFormat   OutputFormat
	// UseRPMs bool
	// SELinux bool

	co *completedDeployOptions
}

// TODO - decide if "frozen" options from DeployOptions should be stored in completedDeployOptions

type completedDeployOptions struct {
	ArchiveKind archive.Kind

	Src io.Reader
}

// Preprocess infers any remaining options and performs any required validation.
func (o *DeployOptions) Preprocess(_ []string) error {
	// TODO - perform as much validation as possible and return error containing all known issues

	co := &completedDeployOptions{}

	if o.ArchiveFileName == "" {
		return errors.New("archive file is required")
	}
	if o.ArchiveFileName == "-" && o.ArchiveFormat == UnknownFormat {
		return errors.New("must specify a format when reading from stdin")
	}

	if o.ArchiveFileName == "-" {
		co.Src = os.Stdin
	} else {
		if srcFile, err := os.Open(o.ArchiveFileName); err != nil {
			return fmt.Errorf(
				"couldn't read archive file %s: %v",
				o.ArchiveFileName, err,
			)
		} else {
			co.Src = srcFile
		}
	}

	// TODO - improve scalability of format auto-detections
	switch {
	case o.ArchiveFileName == "-" || o.ArchiveFormat != UnknownFormat:
		co.ArchiveKind = o.ArchiveFormat.ToArchiveKind()
	case strings.HasSuffix(o.ArchiveFileName, ".tar"):
		co.ArchiveKind = archive.KindTar
	case strings.HasSuffix(o.ArchiveFileName, ".tar.gz") || strings.HasSuffix(o.ArchiveFileName, ".tgz"):
		co.ArchiveKind = archive.KindTarGz
	default:
		return errors.New("unable to determine archive format, please specify flag or allow auto-detection by using known file type")
	}

	o.co = co
	return nil
}

// Run performs the operation.
func (o *DeployOptions) Run() error {
	if o.co == nil {
		return errors.New("DeployOptions must be preprocessed before Run is called")
	}

	// TODO - use deployer and actually deploy!

	return nil
}
