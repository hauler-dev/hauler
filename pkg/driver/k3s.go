package driver

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
)

const (
	k3sReleaseUrl     = "https://github.com/k3s-io/k3s/releases/download"
	k3sDefaultVersion = "v1.21.4+k3s1"
)

//go:embed embed/k3s-init.sh
var k3sInit string

type K3s struct {
	version string
}

func (k K3s) Name() string { return "k3s" }

// Version returns the RFC 1123 compliant version
func (k K3s) Version() string {
	return strings.ReplaceAll(k.version, "+", "-")
}

// Images docs
// TODO: Use context for timeouts
func (k K3s) Images(ctx context.Context) ([]string, error) {
	u, err := url.Parse(fmt.Sprintf("%s/%s/k3s-images.txt", k3sReleaseUrl, k.version))
	if err != nil {
		return nil, err
	}

	resp, err := http.Get(u.String())
	if err != nil || resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to return images for k3s %s from %s", k.version, u.String())
	}
	defer resp.Body.Close()

	var imgs []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		imgs = append(imgs, scanner.Text())
	}

	return imgs, nil
}

func (k K3s) BinaryFetchURL() string {
	p := fmt.Sprintf("%s/%s/%s", k3sReleaseUrl, k.version, k.Name())
	u, err := url.Parse(p)
	if err != nil {
		p = path.Join(k3sReleaseUrl, k3sDefaultVersion, k.Name())
		u, _ = url.Parse(p)
	}
	return u.String()
}

func (k K3s) Template() []byte {
	return []byte(k3sInit)
}

func (k K3s) Start(ctx context.Context, out io.Writer) error {
	if err := os.WriteFile("/opt/hauler/bin/k3s-init.sh", []byte(k3sInit), 0755); err != nil {
		return err
	}

	cmd := exec.Command("/bin/sh", "/opt/hauler/bin/k3s-init.sh")

	cmd.Env = append(os.Environ(), []string{
		"INSTALL_K3S_SKIP_DOWNLOAD=true",
		"INSTALL_K3S_SELINUX_WARN=true",
		"INSTALL_K3S_SKIP_SELINUX_RPM=true",
		"INSTALL_K3S_BIN_DIR=/opt/hauler/bin",

		//TODO: Provide a real dryrun option
		//"INSTALL_K3S_SKIP_START=true",
	}...)

	cmd.Stdout = out
	return cmd.Run()
}
