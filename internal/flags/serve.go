package flags

import (
	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/pkg/consts"
)

type ServeRegistryOpts struct {
	*StoreRootOpts

	Port       int
	RootDir    string
	ConfigFile string
	ReadOnly   bool

	TLSCert string
	TLSKey  string
}

func (o *ServeRegistryOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.IntVarP(&o.Port, "port", "p", consts.DefaultRegistryPort, "(Optional) Set the port to use for incoming connections")
	f.StringVar(&o.RootDir, "directory", consts.DefaultRegistryRootDir, "(Optional) Directory to use for backend. Defaults to $PWD/registry")
	f.StringVarP(&o.ConfigFile, "config", "c", "", "(Optional) Location of config file (overrides all flags)")
	f.BoolVar(&o.ReadOnly, "readonly", true, "(Optional) Run the registry as readonly")

	f.StringVar(&o.TLSCert, "tls-cert", "", "(Optional) Location of the TLS Certificate to use for server authenication")
	f.StringVar(&o.TLSKey, "tls-key", "", "(Optional) Location of the TLS Key to use for server authenication")

	cmd.MarkFlagsRequiredTogether("tls-cert", "tls-key")
}

type ServeFilesOpts struct {
	*StoreRootOpts

	Port    int
	Timeout int
	RootDir string

	TLSCert string
	TLSKey  string
}

func (o *ServeFilesOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.IntVarP(&o.Port, "port", "p", consts.DefaultFileserverPort, "(Optional) Set the port to use for incoming connections")
	f.IntVarP(&o.Timeout, "timeout", "t", consts.DefaultFileserverTimeout, "(Optional) Timeout duration for HTTP Requests in seconds for both reads/writes")
	f.StringVar(&o.RootDir, "directory", consts.DefaultFileserverRootDir, "(Optional) Directory to use for backend. Defaults to $PWD/fileserver")

	f.StringVar(&o.TLSCert, "tls-cert", "", "(Optional) Location of the TLS Certificate to use for server authenication")
	f.StringVar(&o.TLSKey, "tls-key", "", "(Optional) Location of the TLS Key to use for server authenication")

	cmd.MarkFlagsRequiredTogether("tls-cert", "tls-key")
}
