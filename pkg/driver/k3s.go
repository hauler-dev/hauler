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
	"path/filepath"

	"github.com/imdario/mergo"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/yaml"
)

const (
	k3sReleaseUrl     = "https://github.com/k3s-io/k3s/releases/download"
	k3sDefaultVersion = "v1.21.4+k3s1"
)

//go:embed embed/k3s-init.sh
var k3sInit string

type K3s struct {
	Version string

	Config K3sConfig
}

//TODO: Would be nice if these just pointed to k3s/pkg/cli/cmds
type K3sConfig struct {
	DataDir        string `json:"data-dir,omitempty"`
	KubeConfig     string `json:"write-kubeconfig,omitempty"`
	KubeConfigMode string `json:"write-kubeconfig-mode,omitempty"`

	Disable []string `json:"disable,omitempty"`
}

func (k K3s) Name() string { return "k3s" }

func (k K3s) KubeConfigPath() string { return k.Config.KubeConfig }

func (k K3s) DataPath(elem ...string) string {
	base := []string{k.Config.DataDir}
	return filepath.Join(append(base, elem...)...)
}

func (k K3s) WriteConfig() error {
	kCfgPath := filepath.Dir(k.Config.KubeConfig)
	if err := os.MkdirAll(kCfgPath, os.ModePerm); err != nil {
		return err
	}

	data, err := yaml.Marshal(k.Config)

	c := make(map[string]interface{})
	if err := yaml.Unmarshal(data, &c); err != nil {
		return err
	}

	var uc map[string]interface{}
	path := filepath.Join(kCfgPath, "config.yaml")
	if data, err := os.ReadFile(path); err != nil {
		err := yaml.Unmarshal(data, &uc)
		if err != nil {
			return err
		}
	}

	//Merge with user defined configs taking precedence
	if err := mergo.Merge(&c, uc); err != nil {
		return err
	}

	mergedData, err := yaml.Marshal(&c)
	if err != nil {
		return err
	}

	return os.WriteFile(path, mergedData, 0644)
}

func (k K3s) Images(ctx context.Context) ([]string, error) {
	imgs, err := k.listImages()
	if err != nil {
		return nil, err
	}
	return imgs, nil
}

func (k K3s) BinaryFetchURL() string {
	p := fmt.Sprintf("%s/%s/%s", k3sReleaseUrl, k.Version, k.Name())
	u, err := url.Parse(p)
	if err != nil {
		p = path.Join(k3sReleaseUrl, k3sDefaultVersion, k.Name())
		u, _ = url.Parse(p)
	}
	return u.String()
}

//SystemObjects returns a slice of object.ObjMetadata required for driver to be functional and accept new resources
//hauler's bootstrapping sequence will always wait for SystemObjects to be in a Ready status before proceeding
func (k K3s) SystemObjects() (objs []object.ObjMetadata) {
	for _, dep := range []string{"coredns"} {
		objMeta, _ := object.CreateObjMetadata("kube-system", dep, schema.GroupKind{Kind: "Deployment", Group: "apps"})
		objs = append(objs, objMeta)
	}
	return objs
}

func (k K3s) Start(out io.Writer) error {
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

func (k K3s) listImages() ([]string, error) {
	u, err := url.Parse(fmt.Sprintf("%s/%s/k3s-images.txt", k3sReleaseUrl, k.Version))
	if err != nil {
		return nil, err
	}

	resp, err := http.Get(u.String())
	if err != nil || resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to return images for k3s %s from %s", k.Version, u.String())
	}
	defer resp.Body.Close()

	var imgs []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		imgs = append(imgs, scanner.Text())
	}

	return imgs, nil
}
