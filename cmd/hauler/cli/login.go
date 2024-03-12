package cli

import (
	"strings"
	"os"
	"io"
	"fmt"
	"github.com/spf13/cobra"

	"github.com/rancherfederal/hauler/cmd/hauler/cli/login"
)

func addLogin(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to a registry or helm repo",
		Aliases: []string{"l"},
		Example: `
# authenticate to registry reg.example.com
hauler login registry reg.example.com -u bob -p haulin

# authenticate to a helm repo
hauler login helm <chart-name> https://chart.repo.com -u bob -p haulin`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, arg []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		addLoginRegistry(),
		addLoginHelm(),
	)

	parent.AddCommand(cmd)
}

func addLoginRegistry() *cobra.Command {
	o := &login.RegistryOpts{}

	cmd := &cobra.Command{
		Use:     "registry",
		Short:   "Log into an authenticated registry",
		Aliases: []string{"r"},
		Example: `hauler login registry reg.example.com -u bob -p haulin`,
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

			return login.RegistryLoginCmd(ctx, o, arg[0])
		},
	}
	o.AddArgs(cmd)

	return cmd
}

func addLoginHelm() *cobra.Command {
	o := &login.HelmOpts{}

	cmd := &cobra.Command{
		Use:     "helm-repo [name] [url]",
		Short:   "Authentication info for a helm repo",
		Aliases: []string{"h"},
		Example: `hauler login helm-repo my-helm-repo https://chart.repo.com -u bob -p haulin`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, arg []string) error {
			ctx := cmd.Context()

			name := arg[0]
			url := arg[1]

			if o.PasswordStdin {
				contents, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				o.Password = strings.TrimSuffix(string(contents), "\n")
				o.Password = strings.TrimSuffix(o.Password, "\r")
			}
			
			if o.Username == "" || o.Password == "" {
				return fmt.Errorf("both username and password are required")
			}

			// Check if the repo name is legal
			if strings.Contains(name, "/") {
				return fmt.Errorf("repository name (%s) contains '/', please specify a different name without '/'", name)
			}

			return login.HelmLoginCmd(ctx, o, name, url)
		},
	}
	o.AddArgs(cmd)

	return cmd
}


