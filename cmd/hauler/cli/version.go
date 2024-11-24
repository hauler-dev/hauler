package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/internal/version"
)

func addVersion(parent *cobra.Command, ro *flags.CliRootOpts) {
	o := &flags.VersionOpts{}

	cmd := &cobra.Command{
		Use:     "version",
		Short:   "Print the current version",
		Aliases: []string{"v"},
		RunE: func(cmd *cobra.Command, args []string) error {
			v := version.GetVersionInfo()
			v.Name = cmd.Root().Name()
			v.Description = cmd.Root().Short
			v.FontName = "starwars"
			cmd.SetOut(cmd.OutOrStdout())

			if o.JSON {
				out, err := v.JSONString()
				if err != nil {
					return fmt.Errorf("unable to generate JSON from version info: %w", err)
				}
				cmd.Println(out)
			} else {
				cmd.Println(v.String())
			}
			return nil
		},
	}
	o.AddFlags(cmd)

	parent.AddCommand(cmd)
}
