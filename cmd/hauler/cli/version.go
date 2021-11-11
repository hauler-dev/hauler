package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/pkg/version"
)

func addVersion(parent *cobra.Command) {
	var json bool

	cmd := &cobra.Command{
		Use:     "version",
		Short:   "Print current hauler version",
		Long:    "Print current hauler version",
		Aliases: []string{"v"},
		RunE: func(cmd *cobra.Command, args []string) error {
			v := version.GetVersionInfo()
			response := v.String()
			if json {
				data, err := v.JSONString()
				if err != nil {
					return err
				}
				response = data
			}
			fmt.Print(response)
			return nil
		},
	}

	cmd.Flags().BoolVar(&json, "json", false, "toggle output in JSON")

	parent.AddCommand(cmd)
}
