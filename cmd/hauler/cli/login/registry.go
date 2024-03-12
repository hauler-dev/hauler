package login

import (
	"context"
	"github.com/spf13/cobra"
	"oras.land/oras-go/pkg/content"

	"github.com/rancherfederal/hauler/pkg/cosign"
)

type RegistryOpts struct {
	Username  string
	Password  string
	PasswordStdin bool
}

func (o *RegistryOpts) AddArgs(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.Username, "username", "u", "", "Username")
	f.StringVarP(&o.Password, "password", "p", "", "Password")
	f.BoolVar(&o.PasswordStdin, "password-stdin", false, "Take the password from stdin")
}

func RegistryLoginCmd(ctx context.Context, o *RegistryOpts, registry string) error {
	ropts := content.RegistryOptions{
		Username:  o.Username,
		Password:  o.Password,
	}

	err := cosign.RegistryLogin(ctx, nil, registry, ropts)
	if err != nil {
		return err
	}
	
	return nil
}