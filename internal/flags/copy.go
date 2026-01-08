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

	f.StringVarP(&o.Username, "username", "u", "", "(Deprecated) Please use 'hauler login'")
	f.StringVarP(&o.Password, "password", "p", "", "(Deprecated) Please use 'hauler login'")
	f.BoolVar(&o.Insecure, "insecure", false, "(Optional) Allow insecure connections")
	f.BoolVar(&o.PlainHTTP, "plain-http", false, "(Optional) Allow plain HTTP connections")
	f.StringVarP(&o.Only, "only", "o", "", "(Optional) Custom string array to only copy specific 'image' items")

	cmd.MarkFlagsRequiredTogether("username", "password")

	if err := f.MarkDeprecated("username", "please use 'hauler login'"); err != nil {
		panic(err)
	}
	if err := f.MarkDeprecated("password", "please use 'hauler login'"); err != nil {
		panic(err)
	}
	if err := f.MarkHidden("username"); err != nil {
		panic(err)
	}
	if err := f.MarkHidden("password"); err != nil {
		panic(err)
	}
}
