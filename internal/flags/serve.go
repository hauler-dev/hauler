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
}

func (o *ServeRegistryOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.IntVarP(&o.Port, "port", "p", 5000, "Port to listen on.")
	f.StringVar(&o.RootDir, "directory", "registry", "Directory to use for backend.  Defaults to $PWD/registry")
	f.StringVarP(&o.ConfigFile, "config", "c", "", "Path to a config file, will override all other configs")
	f.BoolVar(&o.ReadOnly, "readonly", true, "Run the registry as readonly.")
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

	// Add validation configuration
	cfg.Validation.Manifests.URLs.Allow = []string{".+"}

	cfg.Log.Level = "info"
	cfg.HTTP.Addr = fmt.Sprintf(":%d", o.Port)
	cfg.HTTP.Headers = http.Header{
		"X-Content-Type-Options": []string{"nosniff"},
	}

	return cfg
}

type ServeFilesOpts struct {
	*RootOpts

	Port    int
	Timeout int
	RootDir string
}

func (o *ServeFilesOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()

	f.IntVarP(&o.Port, "port", "p", 8080, "Port to listen on.")
	f.IntVarP(&o.Timeout, "timeout", "t", 60, "Set the http request timeout duration in seconds for both reads and write.")
	f.StringVar(&o.RootDir, "directory", "fileserver", "Directory to use for backend.  Defaults to $PWD/fileserver")
}
