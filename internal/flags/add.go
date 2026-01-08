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
	f.BoolVar(&o.Tlog, "use-tlog-verify", false, "(Optional) Allow transparency log verification (defaults to false)")
	f.StringVarP(&o.Platform, "platform", "p", "", "(Optional) Specify the platform of the image... i.e. linux/amd64 (defaults to all)")
	f.StringVar(&o.Rewrite, "rewrite", "", "(Optional) Rewrite artifact path to specified string (EXPERIMENTAL)")
}

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

	ChartOpts       *action.ChartPathOptions
	AddDependencies bool
	AddImages       bool
	HelmValues      string
	Platform        string
	KubeVersion     string
	Rewrite         string
}

func (o *AddChartOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringVar(&o.ChartOpts.RepoURL, "repo", "", "Location of the chart (https:// | http:// | oci://)")
	f.StringVar(&o.ChartOpts.Version, "version", "", "(Optional) Specify the version of the chart (v1.0.0 | 2.0.0 | ^2.0.0)")
	f.BoolVar(&o.ChartOpts.Verify, "verify", false, "(Optional) Verify the chart before fetching it")
	f.StringVar(&o.ChartOpts.Username, "username", "", "(Optional) Username to use for authentication")
	f.StringVar(&o.ChartOpts.Password, "password", "", "(Optional) Password to use for authentication")
	f.StringVar(&o.ChartOpts.CertFile, "cert-file", "", "(Optional) Location of the TLS Certificate to use for client authentication")
	f.StringVar(&o.ChartOpts.KeyFile, "key-file", "", "(Optional) Location of the TLS Key to use for client authentication")
	f.BoolVar(&o.ChartOpts.InsecureSkipTLSverify, "insecure-skip-tls-verify", false, "(Optional) Skip TLS certificate verification")
	f.StringVar(&o.ChartOpts.CaFile, "ca-file", "", "(Optional) Location of CA Bundle to enable certification verification")
	f.StringVar(&o.Rewrite, "rewrite", "", "(Optional) Rewrite artifact path to specified string (EXPERIMENTAL)")

	cmd.Flags().BoolVar(&o.AddDependencies, "add-dependencies", false, "(Optional) Fetch dependent helm charts (EXPERIMENTAL)")
	f.BoolVar(&o.AddImages, "add-images", false, "(Optional) Fetch images referenced in helm charts (EXPERIMENTAL)")
	f.StringVar(&o.HelmValues, "values", "", "(Optional) Specify helm chart values when fetching images (EXPERIMENTAL)")
	f.StringVarP(&o.Platform, "platform", "p", "", "(Optional) Specify the platform of the image, e.g. linux/amd64 (EXPERIMENTAL)")
	f.StringVar(&o.KubeVersion, "kube-version", "v1.34.1", "(Optional) Override the kubernetes version for helm template rendering (EXPERIMENTAL)")
}
