package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"oras.land/oras-go/pkg/content"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/cosign"
)

func addLogin(parent *cobra.Command) {
	o := &flags.LoginOpts{}

	cmd := &cobra.Command{
		Use:     "login",
		Short:   "Login to a registry",
		Long:    "Login to an OCI Compliant Registry (stored at ~/.docker/config.json)",
		Example: "# login to registry.example.com\nhauler login registry.example.com -u bob -p haulin",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, arg []string) error {
			ctx := cmd.Context()

			if o.PasswordStdin {
				contents, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				o.Password = strings.TrimSuffix(string(contents), "\n")
				o.Password = strings.TrimSuffix(o.Password, "\r")
			}

			if o.Username == "" && o.Password == "" {
				return fmt.Errorf("username and password required")
			}

			return login(ctx, o, arg[0], ro)
		},
	}
	o.AddFlags(cmd)

	parent.AddCommand(cmd)
}

func login(ctx context.Context, o *flags.LoginOpts, registry string, ro *flags.CliRootOpts) error {
	ropts := content.RegistryOptions{
		Username: o.Username,
		Password: o.Password,
	}

	err := cosign.RegistryLogin(ctx, nil, registry, ropts, ro)
	if err != nil {
		return err
	}

	return nil
}
