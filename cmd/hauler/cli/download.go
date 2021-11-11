package cli

import (
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/download"
)

func addDownload(parent *cobra.Command) {
	o := &download.Opts{}

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download OCI content from a registry and populate it on disk",
		Long: `Locate OCI content based on it's reference in a compatible registry and download the contents to disk.

Note that the content type determines it's format on disk.  Hauler's built in content types act as follows:

	- File: as a file named after the pushed contents source name (ex: my-file.yaml:latest --> my-file.yaml)
	- Image: as a .tar named after the image (ex: alpine:latest --> alpine:latest.tar)
	- Chart: as a .tar.gz named after the chart (ex: loki:2.0.2 --> loki-2.0.2.tar.gz)`,
		Example: `
# Download a file
hauler dl my-file.yaml:latest

# Download an image
hauler dl rancher/k3s:v1.22.2-k3s2

# Download a chart
hauler dl longhorn:1.2.0`,
		Aliases: []string{"dl"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, arg []string) error {
			ctx := cmd.Context()

			return download.Cmd(ctx, o, arg[0])
		},
	}
	o.AddArgs(cmd)

	parent.AddCommand(cmd)
}
