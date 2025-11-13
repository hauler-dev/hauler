package flags

import (
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
)

type AddImageOpts struct {
	*StoreRootOpts
	Name                         string
	Key                          string
	CertOidcIssuer               string
	CertOidcIssuerRegexp         string
	CertIdentity                 string
	CertIdentityRegexp           string
	CertGithubWorkflowRepository string
	Tlog                         bool
	Platform                     string
	Rewrite                      string
}

func (o *AddImageOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.Key, "key", "k", "", "(Optional) Location of public key to use for signature verification")
	f.StringVar(&o.CertIdentity, "certificate-identity", "", "(Optional) Cosign certificate-identity (either --certificate-identity or --certificate-identity-regexp required for keyless verification)")
	f.StringVar(&o.CertIdentityRegexp, "certificate-identity-regexp", "", "(Optional) Cosign certificate-identity-regexp (either --certificate-identity or --certificate-identity-regexp required for keyless verification)")
	f.StringVar(&o.CertOidcIssuer, "certificate-oidc-issuer", "", "(Optional) Cosign option to validate oidc issuer")
	f.StringVar(&o.CertOidcIssuerRegexp, "certificate-oidc-issuer-regexp", "", "(Optional) Cosign option to validate oidc issuer with regex")
	f.StringVar(&o.CertGithubWorkflowRepository, "certificate-github-workflow-repository", "", "(Optional) Cosign certificate-github-workflow-repository option")
	f.BoolVarP(&o.Tlog, "use-tlog-verify", "v", false, "(Optional) Allow transparency log verification. (defaults to false)")
	f.StringVarP(&o.Platform, "platform", "p", "", "(Optional) Specifiy the platform of the image... i.e. linux/amd64 (defaults to all)")
	f.StringVar(&o.Rewrite, "rewrite", "", "(Optional) Rewrite artifact path to specified sting")
}

//func (o *AddImageOpts) RewriteValue() string { return o.Rewrite }

type AddFileOpts struct {
	*StoreRootOpts
	Name string
}

func (o *AddFileOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.Name, "name", "n", "", "(Optional) Rewrite the name of the file")
}

type AddChartOpts struct {
	*StoreRootOpts

	ChartOpts *action.ChartPathOptions
	Rewrite   string
}

func (o *AddChartOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVar(&o.ChartOpts.RepoURL, "repo", "", "Location of the chart (https:// | http:// | oci://)")
	f.StringVar(&o.ChartOpts.Version, "version", "", "(Optional) Specifiy the version of the chart (v1.0.0 | 2.0.0 | ^2.0.0)")
	f.BoolVar(&o.ChartOpts.Verify, "verify", false, "(Optional) Verify the chart before fetching it")
	f.StringVar(&o.ChartOpts.Username, "username", "", "(Optional) Username to use for authentication")
	f.StringVar(&o.ChartOpts.Password, "password", "", "(Optional) Password to use for authentication")
	f.StringVar(&o.ChartOpts.CertFile, "cert-file", "", "(Optional) Location of the TLS Certificate to use for client authenication")
	f.StringVar(&o.ChartOpts.KeyFile, "key-file", "", "(Optional) Location of the TLS Key to use for client authenication")
	f.BoolVar(&o.ChartOpts.InsecureSkipTLSverify, "insecure-skip-tls-verify", false, "(Optional) Skip TLS certificate verification")
	f.StringVar(&o.ChartOpts.CaFile, "ca-file", "", "(Optional) Location of CA Bundle to enable certification verification")
	f.StringVar(&o.Rewrite, "rewrite", "", "(Optional) Rewrite artifact path to specified sting")
}
