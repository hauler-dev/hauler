package flags

import (
	"fmt"

	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/v2/pkg/consts"
)

// ImageCopyOpts holds flags for `hauler copy` -- not to be confused with CopyOpts (`store copy`).
type ImageCopyOpts struct {
	InsecureSkipTLSVerify bool
	PlainHTTP             bool
	CaFile                string
	Retries               int
	Platform              string
}

func (o *ImageCopyOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.BoolVar(&o.InsecureSkipTLSVerify, "insecure-skip-tls-verify", false, "(Optional) Skip TLS certificate verification")
	f.BoolVar(&o.PlainHTTP, "plain-http", false, "(Optional) Allow plain HTTP connections")
	f.StringVar(&o.CaFile, "ca-file", "", "(Optional) Location of CA Bundle to enable certification verification")
	f.IntVarP(&o.Retries, "retries", "r", 0, fmt.Sprintf("Set the number of retries for operations (0 uses HAULER_RETRIES, otherwise defaults to %d)", consts.DefaultRetries))
	f.StringVarP(&o.Platform, "platform", "p", "", "(Optional) Specify the platform of the image... i.e. linux/amd64 (defaults to all)")
}
