package serve

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/distribution/distribution/v3/configuration"
	dcontext "github.com/distribution/distribution/v3/context"
	"github.com/distribution/distribution/v3/version"
	"github.com/spf13/cobra"

	"github.com/kardianos/service"

	"github.com/rancherfederal/hauler/internal/server"
	"github.com/rancherfederal/hauler/pkg/log"
)

type RegistryOpts struct {
	Root       string
	Port       int
	ConfigFile string

	Service bool
}

func (o *RegistryOpts) AddFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&o.Root, "root", "r", ".", "Path to root of the directory to serve")
	f.IntVarP(&o.Port, "port", "p", 5000, "Port to listen on")
	f.StringVarP(&o.ConfigFile, "config", "c", "", "Path to a config file, will override all other configs")
	f.BoolVar(&o.Service, "service", false, "Initialize hauler's registry as a service, this must be configured with a config file (-c).  Note: this requires root privileges.")
}

func RegistryCmd(ctx context.Context, o *RegistryOpts) error {
	l := log.FromContext(ctx)

	if o.Service {
		args := []string{"serve", "registry"}
		if o.ConfigFile == "" {
			l.Warnf("no config file set, defaults will be used in the service file")
		} else {
			args = append(args, []string{"-c", o.ConfigFile}...)
		}
		if err := ensureService(args); err != nil {
			return err
		}

		l.Infof("successfully created service file [%s], enable and start it", "hauler-registry")
		return nil
	}
	ctx = dcontext.WithVersion(ctx, version.Version)

	cfg := o.defaultConfig()
	if o.ConfigFile != "" {
		ucfg, err := loadConfig(o.ConfigFile)
		if err != nil {
			return err
		}
		cfg = ucfg
	}

	s, err := server.NewRegistry(ctx, cfg)
	if err != nil {
		return err
	}

	if err := s.ListenAndServe(); err != nil {
		return err
	}
	return nil
}

func loadConfig(filename string) (*configuration.Configuration, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	return configuration.Parse(f)
}

func (o *RegistryOpts) defaultConfig() *configuration.Configuration {
	cfg := &configuration.Configuration{
		Version: "0.1",
		Storage: configuration.Storage{
			"cache":      configuration.Parameters{"blobdescriptor": "inmemory"},
			"filesystem": configuration.Parameters{"rootdirectory": o.Root},

			// TODO: Ensure this is toggleable via cli arg if necessary
			// "maintenance": configuration.Parameters{"readonly.enabled": false},
		},
	}
	cfg.Log.Level = "info"
	cfg.HTTP.Addr = fmt.Sprintf(":%d", o.Port)
	cfg.HTTP.Headers = http.Header{
		"X-Content-Type-Options": []string{"nosniff"},
	}

	return cfg
}

func ensureService(args []string) error {
	prg := prog{}

	cfg := &service.Config{
		Name:        "hauler-registry",
		DisplayName: "Hauler Registry",
		Description: "Hauler's embedded registry",
	}
	s, err := service.New(prg, cfg)
	if err != nil {
		return err
	}

	var (
		deps []string
	)

	switch s.Platform() {
	case "linux-openrc":
		deps = []string{"need net", "use dns", "after firewall"}

	case "linux-systemd":
		deps = []string{"After=network-online.target", "Wants=network-online.target"}
		cfg.Option = map[string]interface{}{
			"SystemdScript": systemdUnit,
		}
	}

	cfg.Arguments = args
	cfg.Dependencies = deps
	return s.Install()
}

type prog struct{}

func (p prog) Start(s service.Service) error {
	return nil
}

func (p prog) Stop(s service.Service) error {
	return nil
}

const systemdUnit = `[Unit]
Description={{.Description}}
Documentation=https://hauler.dev
ConditionFileIsExecutable={{.Path|cmdEscape}}
{{range $i, $dep := .Dependencies}}
{{$dep}} {{end}}
[Service]
StartLimitInterval=5
StartLimitBurst=10
ExecStart={{.Path|cmdEscape}}{{range .Arguments}} {{.|cmdEscape}}{{end}}
RestartSec=120
Delegate=yes
KillMode=process
LimitCORE=infinity
TasksMax=infinity
TimeoutStartSec=0
{{- if .ChRoot}}RootDirectory={{.ChRoot|cmd}}{{- end}}
{{- if .WorkingDirectory}}WorkingDirectory={{.WorkingDirectory|cmdEscape}}{{- end}}
{{- if .UserName}}User={{.UserName}}{{end}}
{{- if .ReloadSignal}}ExecReload=/bin/kill -{{.ReloadSignal}} "$MAINPID"{{- end}}
{{- if .PIDFile}}PIDFile={{.PIDFile|cmd}}{{- end}}
{{- if and .LogOutput .HasOutputFileSupport -}}
StandardOutput=file:/var/log/{{.Name}}.out
StandardError=file:/var/log/{{.Name}}.err
{{- end}}
{{- if .SuccessExitStatus}}SuccessExitStatus={{.SuccessExitStatus}}{{- end}}
{{ if gt .LimitNOFILE -1 }}LimitNOFILE={{.LimitNOFILE}}{{- end}}
{{ if .Restart}}Restart={{.Restart}}{{- end}}
[Install]
WantedBy=multi-user.target
`
