package flags

import (
	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/pkg/consts"
)

type SyncOpts struct {
	*StoreRootOpts
	FileName        []string
	Key             string
	Products        []string
	Platform        string
	Registry        string
	ProductRegistry string
}

func (o *SyncOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.StringSliceVarP(&o.FileName, "filename", "f", []string{consts.DefaultHaulerManifestName}, "Specify the name of manifest(s) to sync")
	f.StringVarP(&o.Key, "key", "k", "", "(Optional) Location of public key to use for signature verification")
	f.StringSliceVar(&o.Products, "products", []string{}, "(Optional) Specify the product name to fetch collections from the product registry i.e. rancher=v2.8.5,rke2=v1.28.11+rke2r1")
	f.StringVarP(&o.Platform, "platform", "p", "", "(Optional) Specify the platform of the image... i.e linux/amd64 (defaults to all)")
	f.StringVarP(&o.Registry, "registry", "g", "", "(Optional) Specify the registry of the image for images that do not alredy define one")
	f.StringVarP(&o.ProductRegistry, "product-registry", "c", "", "(Optional) Specify the product registry. Defaults to RGS Carbide Registry (rgcrprod.azurecr.us)")
}
