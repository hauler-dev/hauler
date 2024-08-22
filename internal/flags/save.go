package flags

import "github.com/spf13/cobra"

type SaveOpts struct {
	*RootOpts
	FileName string
}

func (o *SaveOpts) AddArgs(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.FileName, "filename", "f", "haul.tar.zst", "Name of archive")
}
