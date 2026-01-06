package flags

import (
	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/pkg/consts"
)

type SyncOpts struct {
	*StoreRootOpts
	FileName                     []string
	Key                          string
	CertOidcIssuer               string
	CertOidcIssuerRegexp         string
	CertIdentity                 string
	CertIdentityRegexp           string
	CertGithubWorkflowRepository string
	Products                     []string
	Platform                     string
	Registry                     string
	ProductRegistry              string
	TempOverride                 string
	Tlog                         bool
	Rewrite                      string
}

func (o *SyncOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringSliceVarP(&o.FileName, "filename", "f", []string{consts.DefaultHaulerManifestName}, "Specify the name of manifest(s) to sync")
	f.StringVarP(&o.Key, "key", "k", "", "(Optional) Location of public key to use for signature verification")
	f.StringVar(&o.CertIdentity, "certificate-identity", "", "(Optional) Cosign certificate-identity (either --certificate-identity or --certificate-identity-regexp required for keyless verification)")
	f.StringVar(&o.CertIdentityRegexp, "certificate-identity-regexp", "", "(Optional) Cosign certificate-identity-regexp (either --certificate-identity or --certificate-identity-regexp required for keyless verification)")
	f.StringVar(&o.CertOidcIssuer, "certificate-oidc-issuer", "", "(Optional) Cosign option to validate oidc issuer")
	f.StringVar(&o.CertOidcIssuerRegexp, "certificate-oidc-issuer-regexp", "", "(Optional) Cosign option to validate oidc issuer with regex")
	f.StringVar(&o.CertGithubWorkflowRepository, "certificate-github-workflow-repository", "", "(Optional) Cosign certificate-github-workflow-repository option")
	f.StringSliceVar(&o.Products, "products", []string{}, "(Optional) Specify the product name to fetch collections from the product registry i.e. rancher=v2.10.1,rke2=v1.31.5+rke2r1")
	f.StringVarP(&o.Platform, "platform", "p", "", "(Optional) Specify the platform of the image... i.e linux/amd64 (defaults to all)")
	f.StringVarP(&o.Registry, "registry", "g", "", "(Optional) Specify the registry of the image for images that do not alredy define one")
	f.StringVarP(&o.ProductRegistry, "product-registry", "c", "", "(Optional) Specify the product registry. Defaults to RGS Carbide Registry (rgcrprod.azurecr.us)")
	f.StringVarP(&o.TempOverride, "tempdir", "t", "", "(Optional) Override the default temporary directiory determined by the OS")
	f.BoolVarP(&o.Tlog, "use-tlog-verify", "v", false, "(Optional) Allow transparency log verification. (defaults to false)")
	f.StringVar(&o.Rewrite, "rewrite", "", "(Optional) Rewrite artifact path to specified string (experimental)")
}
