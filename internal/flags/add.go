package flags

import (
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
)

type AddImageOpts struct {
	*RootOpts
	Name     string
	Key      string
	Platform string
}

func (o *AddImageOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.Key, "key", "k", "", "(Optional) Path to the key for digital signature verification")
	f.StringVarP(&o.Platform, "platform", "p", "", "(Optional) Specific platform to save. i.e. linux/amd64. Defaults to all if flag is omitted.")
}

type AddFileOpts struct {
	*RootOpts
	Name string
}

func (o *AddFileOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.Name, "name", "n", "", "(Optional) Name to assign to file in store")
}

type AddChartOpts struct {
	*RootOpts

	ChartOpts *action.ChartPathOptions
}

func (o *AddChartOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVar(&o.ChartOpts.RepoURL, "repo", "", "chart repository url where to locate the requested chart")
	f.StringVar(&o.ChartOpts.Version, "version", "", "specify a version constraint for the chart version to use. This constraint can be a specific tag (e.g. 1.1.1) or it may reference a valid range (e.g. ^2.0.0). If this is not specified, the latest version is used")
	f.BoolVar(&o.ChartOpts.Verify, "verify", false, "verify the package before using it")
	f.StringVar(&o.ChartOpts.Username, "username", "", "chart repository username where to locate the requested chart")
	f.StringVar(&o.ChartOpts.Password, "password", "", "chart repository password where to locate the requested chart")
	f.StringVar(&o.ChartOpts.CertFile, "cert-file", "", "identify HTTPS client using this SSL certificate file")
	f.StringVar(&o.ChartOpts.KeyFile, "key-file", "", "identify HTTPS client using this SSL key file")
	f.BoolVar(&o.ChartOpts.InsecureSkipTLSverify, "insecure-skip-tls-verify", false, "skip tls certificate checks for the chart download")
	f.StringVar(&o.ChartOpts.CaFile, "ca-file", "", "verify certificates of HTTPS-enabled servers using this CA bundle")
}
