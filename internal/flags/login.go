package flags

import "github.com/spf13/cobra"

type LoginOpts struct {
	Username      string
	Password      string
	PasswordStdin bool
}

func (o *LoginOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.Username, "username", "u", "", "(Optional) Username to use for authentication")
	f.StringVarP(&o.Password, "password", "p", "", "(Optional) Password to use for authentication")
	f.BoolVar(&o.PasswordStdin, "password-stdin", false, "(Optional) Password to use for authentication (from stdin)")
}
