package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hauler-dev/hauler/internal/version"
)

func addVersion(parent *cobra.Command) {
	var json bool

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

			if json {
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
	cmd.Flags().BoolVar(&json, "json", false, "toggle output in JSON")

	parent.AddCommand(cmd)
}
