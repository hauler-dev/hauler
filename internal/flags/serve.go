package flags

import (
	"github.com/spf13/cobra"
	"hauler.dev/go/hauler/v2/pkg/consts"
)

type ServeRegistryOpts struct {
	*StoreRootOpts

	Port           int
	RootDir        string
	ConfigFile     string
	ReadOnly       bool
	BasicAuth      string
	BasicAuthRealm string

	TLSCert string
	TLSKey  string
}

func (o *ServeRegistryOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.IntVarP(&o.Port, "port", "p", consts.DefaultRegistryPort, "(Optional) Set the port to use for incoming connections")
	f.StringVar(&o.RootDir, "directory", consts.DefaultRegistryRootDir, "(Optional) Directory to use for backend. Defaults to $PWD/registry")
	f.StringVarP(&o.ConfigFile, "config", "c", "", "(Optional) Location of the registry config file (overrides all flags)")
	f.BoolVar(&o.ReadOnly, "readonly", true, "(Optional) Run the registry as readonly")
	f.StringVar(&o.BasicAuth, "basic-auth", "", "(Optional) Location of the htpasswd file to use for basic authentication")
	f.StringVar(&o.BasicAuthRealm, "basic-auth-realm", consts.DefaultRegistryRealm, "(Optional) Realm to use for basic authentication")

	f.StringVar(&o.TLSCert, "tls-cert", "", "(Optional) Location of the TLS Certificate to use for server authenication")
	f.StringVar(&o.TLSKey, "tls-key", "", "(Optional) Location of the TLS Key to use for server authenication")

	cmd.MarkFlagsRequiredTogether("tls-cert", "tls-key")
}

type ServeFilesOpts struct {
	*StoreRootOpts

	Port           int
	Timeout        int
	RootDir        string
	BasicAuth      string
	BasicAuthRealm string

	TLSCert string
	TLSKey  string
}

func (o *ServeFilesOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.IntVarP(&o.Port, "port", "p", consts.DefaultFileserverPort, "(Optional) Set the port to use for incoming connections")
	f.IntVar(&o.Timeout, "timeout", consts.DefaultFileserverTimeout, "(Optional) Timeout duration for HTTP Requests in seconds for both reads/writes")
	f.StringVar(&o.RootDir, "directory", consts.DefaultFileserverRootDir, "(Optional) Directory to use for backend. (defaults to $PWD/fileserver)")
	f.StringVar(&o.BasicAuth, "basic-auth", "", "(Optional) Location of the htpasswd file to use for basic authentication")
	f.StringVar(&o.BasicAuthRealm, "basic-auth-realm", consts.DefaultFileserverRealm, "(Optional) Realm to use for basic authentication")

	f.StringVar(&o.TLSCert, "tls-cert", "", "(Optional) Location of the TLS Certificate to use for server authenication")
	f.StringVar(&o.TLSKey, "tls-key", "", "(Optional) Location of the TLS Key to use for server authenication")

	cmd.MarkFlagsRequiredTogether("tls-cert", "tls-key")
}

// ServeGitOpts mirrors ServeFilesOpts, since every git-kind artifact in the store gets extracted under RootDir (one subdirectory each) and served, same as fileserver does for files.
type ServeGitOpts struct {
	*StoreRootOpts

	Port           int
	Timeout        int
	RootDir        string
	BasicAuth      string
	BasicAuthRealm string

	TLSCert string
	TLSKey  string
}

func (o *ServeGitOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.IntVarP(&o.Port, "port", "p", consts.DefaultGitPort, "(Optional) Set the port to use for incoming connections")
	f.IntVar(&o.Timeout, "timeout", consts.DefaultGitTimeout, "(Optional) Timeout duration for HTTP Requests in seconds for both reads/writes")
	f.StringVar(&o.RootDir, "directory", consts.DefaultGitRootDir, "(Optional) Directory to use for backend. (defaults to $PWD/git)")
	f.StringVar(&o.BasicAuth, "basic-auth", "", "(Optional) Location of the htpasswd file to use for basic authentication")
	f.StringVar(&o.BasicAuthRealm, "basic-auth-realm", consts.DefaultGitRealm, "(Optional) Realm to use for basic authentication")

	f.StringVar(&o.TLSCert, "tls-cert", "", "(Optional) Location of the TLS Certificate to use for server authenication")
	f.StringVar(&o.TLSKey, "tls-key", "", "(Optional) Location of the TLS Key to use for server authenication")

	cmd.MarkFlagsRequiredTogether("tls-cert", "tls-key")
}
