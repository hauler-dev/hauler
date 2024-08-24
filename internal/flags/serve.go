package flags

import (
	"fmt"
	"net/http"

	"github.com/distribution/distribution/v3/configuration"
	"github.com/spf13/cobra"
)

type ServeRegistryOpts struct {
	*RootOpts

	Port       int
	RootDir    string
	ConfigFile string
	ReadOnly   bool

	TLSCert string
	TLSKey  string
}

func (o *ServeRegistryOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.IntVarP(&o.Port, "port", "p", 5000, "Port used to accept incoming connections")
	f.StringVar(&o.RootDir, "directory", "registry", "Directory to use for backend. Defaults to $PWD/registry")
	f.StringVarP(&o.ConfigFile, "config", "c", "", "Path to config file, overrides all other flags")
	f.BoolVar(&o.ReadOnly, "readonly", true, "Run the registry as readonly")

	f.StringVar(&o.TLSCert, "tls-cert", "", "Location of the TLS Certificate")
	f.StringVar(&o.TLSKey, "tls-key", "", "Location of the TLS Key")

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
	*RootOpts

	Port    int
	Timeout int
	RootDir string

	TLSCert string
	TLSKey  string
}

func (o *ServeFilesOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.IntVarP(&o.Port, "port", "p", 8080, "Port used to accept incoming connections")
	f.IntVarP(&o.Timeout, "timeout", "t", 60, "Timeout duration for HTTP Requests in seconds for both reads/writes")
	f.StringVar(&o.RootDir, "directory", "fileserver", "Directory to use for backend. Defaults to $PWD/fileserver")

	f.StringVar(&o.TLSCert, "tls-cert", "", "Location of the TLS Certificate")
	f.StringVar(&o.TLSKey, "tls-key", "", "Location of the TLS Key")

	cmd.MarkFlagsRequiredTogether("tls-cert", "tls-key")
}
