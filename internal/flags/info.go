package flags

import "github.com/spf13/cobra"

type InfoOpts struct {
	*StoreRootOpts

	OutputFormat string
	TypeFilter   string
	SizeUnit     string
	ListRepos    bool
	ShowDigests  bool
}

func (o *InfoOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.OutputFormat, "output", "o", "table", "(Optional) Specify the output format (table | json)")
	f.StringVarP(&o.TypeFilter, "type", "t", "all", "(Optional) Filter on content type (image | chart | file | sigs | atts | sbom)")
	f.BoolVar(&o.ListRepos, "list-repos", false, "(Optional) List all repository names")
	f.BoolVar(&o.ShowDigests, "digests", false, "(Optional) Show digests of each artifact in the output table")
}
