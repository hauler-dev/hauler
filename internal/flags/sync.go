package flags

import "github.com/spf13/cobra"

type SyncOpts struct {
	*StoreRootOpts
	ContentFiles    []string
	Key             string
	Products        []string
	Platform        string
	Registry        string
	ProductRegistry string
}

func (o *SyncOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringSliceVarP(&o.ContentFiles, "files", "f", []string{}, "Path(s) to local content files (Manifests). i.e. '--files ./rke2-files.yml")
	f.StringVarP(&o.Key, "key", "k", "", "(Optional) Path to the key for signature verification")
	f.StringSliceVar(&o.Products, "products", []string{}, "(Optional) Feature for RGS Carbide customers to fetch collections and content from the Carbide Registry. i.e. '--product rancher=v2.8.5,rke2=v1.28.11+rke2r1'")
	f.StringVarP(&o.Platform, "platform", "p", "", "(Optional) Specific platform to save. i.e. linux/amd64. Defaults to all if flag is omitted.")
	f.StringVarP(&o.Registry, "registry", "r", "", "(Optional) Default pull registry for image refs that are not specifying a registry name.")
	f.StringVarP(&o.ProductRegistry, "product-registry", "c", "", "(Optional) Specific Product Registry to use. Defaults to RGS Carbide Registry (rgcrprod.azurecr.us).")
}
