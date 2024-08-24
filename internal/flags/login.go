package flags

import "github.com/spf13/cobra"

type LoginOpts struct {
	Username      string
	Password      string
	PasswordStdin bool
}

func (o *LoginOpts) AddArgs(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.Username, "username", "u", "", "Username to use for authentication")
	f.StringVarP(&o.Password, "password", "p", "", "Password to use for authentication")
	f.BoolVar(&o.PasswordStdin, "password-stdin", false, "Password to use for authentication (from stdin)")
}
