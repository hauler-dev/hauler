package flags

import "github.com/spf13/cobra"

type InfoOpts struct {
	*RootOpts

	OutputFormat string
	TypeFilter   string
	SizeUnit     string
	ListRepos    bool
}

func (o *InfoOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.OutputFormat, "output", "o", "table", "Output format (table, json)")
	f.StringVarP(&o.TypeFilter, "type", "t", "all", "Filter on type (image, chart, file, sigs, atts, sbom)")
	f.BoolVar(&o.ListRepos, "list-repos", false, "List all repository names")

	// TODO: Regex/globbing
}
