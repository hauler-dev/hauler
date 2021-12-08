package version

import (
	"encoding/json"
	"fmt"
	"path"
	"runtime"
	"strings"
	"text/tabwriter"
)

var (
	GitVersion = "devel"
	commit     = "unknown"
	buildDate  = "unknown"
)

type Info struct {
	GitVersion string
	GitCommit  string
	BuildDate  string

	GoVersion string
	Compiler  string
	Platform  string
}

func GetVersionInfo() Info {
	return Info{
		GitVersion: GitVersion,
		GitCommit:  commit,
		BuildDate:  buildDate,

		GoVersion: runtime.Version(),
		Compiler:  runtime.Compiler,
		Platform:  path.Join(runtime.GOOS, runtime.GOARCH),
	}
}

func (i Info) String() string {
	b := strings.Builder{}
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)

	fmt.Fprintf(w, "GitVersion:\t%s\n", i.GitVersion)
	fmt.Fprintf(w, "GitCommit:\t%s\n", i.GitCommit)
	fmt.Fprintf(w, "BuildDate:\t%s\n", i.BuildDate)
	fmt.Fprintf(w, "GoVersion:\t%s\n", i.GoVersion)
	fmt.Fprintf(w, "Compiler:\t%s\n", i.Compiler)
	fmt.Fprintf(w, "Platform:\t%s\n", i.Platform)

	w.Flush()
	return b.String()
}

func (i Info) JSONString() (string, error) {
	b, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
