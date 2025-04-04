package flags

import "github.com/spf13/cobra"

type CopyOpts struct {
	*StoreRootOpts

	Username  string
	Password  string
	Insecure  bool
	PlainHTTP bool
	Only      string
}

func (o *CopyOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.Username, "username", "u", "", "(Optional) Username to use for authentication")
	f.StringVarP(&o.Password, "password", "p", "", "(Optional) Password to use for authentication")
	f.BoolVar(&o.Insecure, "insecure", false, "(Optional) Allow insecure connections")
	f.BoolVar(&o.PlainHTTP, "plain-http", false, "(Optional) Allow plain HTTP connections")
	f.StringVarP(&o.Only, "only", "o", "", "(Optional) Custom string array to only copy specific 'image' items, this flag is comma delimited. ex: --only=sig,att,sbom")
}
