package login

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/rancherfederal/hauler/pkg/log"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"
)

type HelmOpts struct {
	Username string
	Password string
	PasswordStdin bool
	CertFile string
	KeyFile string
	CAFile string
	InsecureSkipTLSverify bool
	PassCredentialsAll bool
}

func (o *HelmOpts) AddArgs(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVarP(&o.Username, "username", "u", "", "chart repository username where to locate the requested chart")
	f.StringVarP(&o.Password, "password", "p", "", "chart repository password where to locate the requested chart")
	f.BoolVar(&o.PasswordStdin, "password-stdin", false, "Take the password from stdin")
	f.StringVar(&o.CertFile, "cert-file", "", "identify HTTPS client using this SSL certificate file")
	f.StringVar(&o.KeyFile, "key-file", "", "identify HTTPS client using this SSL key file")
	f.StringVar(&o.CAFile, "ca-file", "", "verify certificates of HTTPS-enabled servers using this CA bundle")
	f.BoolVar(&o.InsecureSkipTLSverify, "insecure-skip-tls-verify", false, "skip tls certificate checks for the chart download")
	f.BoolVar(&o.PassCredentialsAll, "pass-credentials", false, "pass credentials to all domains")
}

func HelmLoginCmd(ctx context.Context, o *HelmOpts, repoName string, repoUrl string) error {
	l := log.FromContext(ctx)

	// Create a new Entry for the repository
	entry := repo.Entry{
		Name:     repoName,
		URL:      repoUrl,
		Username: o.Username,
		Password: o.Password,
		CertFile: o.CertFile,
		KeyFile: o.KeyFile,
		CAFile: o.CAFile,
		InsecureSkipTLSverify: o.InsecureSkipTLSverify,
		PassCredentialsAll: o.PassCredentialsAll,
	}

	// Get the settings from the environment
	settings := cli.New()

	// Create a new RepoFile if it does not exist
	repoFile := settings.RepositoryConfig
	l.Debugf(repoFile)
	if _, err := os.Stat(repoFile); os.IsNotExist(err) {
		_, err := os.Create(repoFile)
		if err != nil {
			return err
		}
	}

	// Load the RepoFile
	repositories, err := repo.LoadFile(repoFile)
	if err != nil {
		return err
	}

	// validate that the repo is a valid and can be reached with the information provided
	r, err := repo.NewChartRepository(&entry, getter.All(settings))
	if err != nil {
		return err
	}
	if _, err := r.DownloadIndexFile(); err != nil {
		return fmt.Errorf("looks like %s is not a valid chart repository or cannot be reached: %s", repoUrl, err)
	}

	// Add the new Entry to the RepoFile
	repositories.Update(&entry)

	// Write the changes to the file
	err = repositories.WriteFile(repoFile, 0644)
	if err != nil {
		return err
	}

	l.Infof("%s has been added to your repositories", repoName)
	return nil
}