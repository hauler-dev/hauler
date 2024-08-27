package flags

import (
	"fmt"
	"net/http"

	"github.com/distribution/distribution/v3/configuration"
	"github.com/spf13/cobra"
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

	f.IntVarP(&o.Port, "port", "p", 5000, "(Optional) Specify the port to use for incoming connections")
	f.StringVar(&o.RootDir, "directory", "registry", "(Optional) Directory to use for backend. Defaults to $PWD/registry")
	f.StringVarP(&o.ConfigFile, "config", "c", "", "(Optional) Location of config file (overrides all flags)")
	f.BoolVar(&o.ReadOnly, "readonly", true, "(Optional) Run the registry as readonly")

	f.StringVar(&o.TLSCert, "tls-cert", "", "(Optional) Location of the TLS Certificate to use for server authenication")
	f.StringVar(&o.TLSKey, "tls-key", "", "(Optional) Location of the TLS Key to use for server authenication")

	cmd.MarkFlagsRequiredTogether("tls-cert", "tls-key")
}

func (o *ServeRegistryOpts) DefaultRegistryConfig() *configuration.Configuration {
	cfg := &configuration.Configuration{
		Version: "0.1",
		Storage: configuration.Storage{
			"cache":      configuration.Parameters{"blobdescriptor": "inmemory"},
			"filesystem": configuration.Parameters{"rootdirectory": o.RootDir},
			"maintenance": configuration.Parameters{
				"readonly": map[any]any{"enabled": o.ReadOnly},
			},
		},
	}

	if o.TLSCert != "" && o.TLSKey != "" {
		cfg.HTTP.TLS.Certificate = o.TLSCert
		cfg.HTTP.TLS.Key = o.TLSKey
	}

	cfg.HTTP.Addr = fmt.Sprintf(":%d", o.Port)
	cfg.HTTP.Headers = http.Header{
		"X-Content-Type-Options": []string{"nosniff"},
	}

	cfg.Log.Level = "info"
	cfg.Validation.Manifests.URLs.Allow = []string{".+"}

	return cfg
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

	f.IntVarP(&o.Port, "port", "p", 8080, "(Optional) Specify the port to use for incoming connections")
	f.IntVarP(&o.Timeout, "timeout", "t", 60, "(Optional) Timeout duration for HTTP Requests in seconds for both reads/writes")
	f.StringVar(&o.RootDir, "directory", "fileserver", "(Optional) Directory to use for backend. Defaults to $PWD/fileserver")

	f.StringVar(&o.TLSCert, "tls-cert", "", "(Optional) Location of the TLS Certificate to use for server authenication")
	f.StringVar(&o.TLSKey, "tls-key", "", "(Optional) Location of the TLS Key to use for server authenication")

	cmd.MarkFlagsRequiredTogether("tls-cert", "tls-key")
}
