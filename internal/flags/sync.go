package flags

import (
	"github.com/spf13/cobra"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

type SyncOpts struct {
	*StoreRootOpts
	FileName                     []string
	ImageTxt                     []string
	Key                          string
	CertOidcIssuer               string
	CertOidcIssuerRegexp         string
	CertIdentity                 string
	CertIdentityRegexp           string
	CertGithubWorkflowRepository string
	Products                     []string
	Platform                     string
	Registry                     string
	RegistriesFilePath           string
	ProductRegistry              string
	Tlog                         bool
	ExcludeExtras                bool
	DryRun                       bool
	Concurrency                  int
	NoProgress                   bool
	CaFile                       string
	InsecureSkipTLSVerify        bool

	// Whether each of these flags was explicitly set on the CLI, captured in
	// sync's PreRunE. A plain bool (and a resolved store/retries value) has no
	// "unset" state, so the resolvers use these markers to let an explicit CLI
	// value win over per-item/annotation instead of only ever turning a flag on.
	TlogChanged          bool
	ExcludeExtrasChanged bool
	InsecureChanged      bool
	StoreChanged         bool
	RetriesChanged       bool
}

func (o *SyncOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringSliceVarP(&o.FileName, "filename", "f", []string{}, "Specify the name of manifest(s) to sync")
	f.StringSliceVarP(&o.ImageTxt, "image-txt", "i", []string{}, "Specify local or remote image.txt file(s) to sync images")
	f.StringVarP(&o.Key, "key", "k", "", "(Optional) Location of public key to use for signature verification")
	f.StringVar(&o.CertIdentity, "certificate-identity", "", "(Optional) Cosign certificate-identity (either --certificate-identity or --certificate-identity-regexp required for keyless verification)")
	f.StringVar(&o.CertIdentityRegexp, "certificate-identity-regexp", "", "(Optional) Cosign certificate-identity-regexp (either --certificate-identity or --certificate-identity-regexp required for keyless verification)")
	f.StringVar(&o.CertOidcIssuer, "certificate-oidc-issuer", "", "(Optional) Cosign option to validate oidc issuer")
	f.StringVar(&o.CertOidcIssuerRegexp, "certificate-oidc-issuer-regexp", "", "(Optional) Cosign option to validate oidc issuer with regex")
	f.StringVar(&o.CertGithubWorkflowRepository, "certificate-github-workflow-repository", "", "(Optional) Cosign certificate-github-workflow-repository option")
	f.StringSliceVar(&o.Products, "products", []string{}, "(Optional) Specify the product name to fetch collections from the product registry i.e. rancher=v2.10.1,rke2=v1.31.5+rke2r1")
	f.StringVarP(&o.Platform, "platform", "p", "", "(Optional) Specify the platform of the image... i.e linux/amd64 (defaults to all)")
	f.StringVarP(&o.Registry, "registry", "g", "", "(Optional) Specify the registry of the image for images that do not alredy define one")
	f.StringVar(&o.RegistriesFilePath, "registries-file-path", "", "(Optional) Specify the path to a registries.yaml file, to configure registry rewrites for pulling images")
	f.StringVarP(&o.ProductRegistry, "product-registry", "c", "", "(Optional) Specify the product registry. Defaults to RGS Carbide Registry (rgcrprod.azurecr.us)")
	f.BoolVar(&o.Tlog, "use-tlog-verify", false, "(Optional) Allow transparency log verification (defaults to false)")
	f.BoolVar(&o.ExcludeExtras, "exclude-extras", false, "(Optional) Exclude cosign signatures, attestations, SBOMs, and OCI referrers when pulling images")
	f.BoolVar(&o.DryRun, "dry-run", false, "(Optional) Output product manifest content to stdout instead of processing it (requires --products)")
	f.IntVarP(&o.Concurrency, "concurrency", "j", consts.DefaultConcurrency, "(Optional) Maximum number of artifacts to fetch and store concurrently (1 = serial; also via HAULER_CONCURRENCY, explicit flag wins)")
	f.BoolVar(&o.NoProgress, "no-progress", false, "(Optional) Disable the live progress display")
	f.StringVar(&o.CaFile, "ca-file", "", "(Optional) Location of CA Bundle to enable certification verification")
	f.BoolVar(&o.InsecureSkipTLSVerify, "insecure-skip-tls-verify", false, "(Optional) Skip TLS certificate verification")
}
