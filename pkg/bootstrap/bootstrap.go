package bootstrap

import (
	"context"
	"errors"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/fs"
	"github.com/rancherfederal/hauler/pkg/kube"
	"github.com/sirupsen/logrus"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"os"
	"os/exec"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/yaml"
	"time"
)

func Boot(ctx context.Context, p v1alpha1.Package, fsys fs.PkgFs) error {
	d := v1alpha1.NewDriver(p.Spec.Driver.Kind)
	_ = d

	if err := fsys.MoveBin(); err != nil {
		return err
	}

	if err := fsys.MoveBundle(); err != nil {
		return err
	}

	if err := fsys.MoveImage(); err != nil {
		return err
	}

	if err := fsys.MoveChart(); err != nil {
		return err
	}

	if err := config(ctx, d); err != nil {
		return err
	}

	out, err := start(ctx, d)
	if err != nil {
		return err
	}

	//TODO: Don't global log in packages
	logrus.Infof(string(out))

	cfg, err := kube.NewKubeConfig()
	if err != nil {
		return err
	}

	// Wait for apiserver to be ready
	waitErr := waitForDriver(ctx, cfg)
	if waitErr != nil {
		return err
	}

	fleetErr := installFleet(ctx, fsys, cfg)
	if fleetErr != nil {
		return fleetErr
	}

	return nil
}

//config will write out the driver config to the appropriate location and merge with anything already there
func config(ctx context.Context, d v1alpha1.Drive) error {
	//TODO: should be typed
	c := make(map[string]interface{})
	c["write-kubeconfig-mode"] = "0644"

	//TODO: Randomize this name so multi-node works
	c["node-name"] = "hauler"

	//TODO: Lazy
	if err := os.MkdirAll("/etc/rancher/k3s", os.ModePerm); err != nil {
		return err
	}

	//Merge anything existing
	if data, err := os.ReadFile(d.ConfigFile()); err == nil {
		// file exists
		err := yaml.Unmarshal(data, &c)
		if err != nil {
			return err
		}
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(d.ConfigFile(), data, 0644)
}

//start will start the cluster using the appropriate driver
func start(ctx context.Context, d v1alpha1.Drive) ([]byte, error) {
	cmd := exec.Command("/bin/sh", "/opt/hauler/bin/k3s-init.sh")

	//General rule of thumb is keep as much configuration in config.yaml as possible, only set script args here
	cmd.Env	= append(os.Environ(), []string{
		"INSTALL_K3S_SKIP_DOWNLOAD=true",
		"INSTALL_K3S_SELINUX_WARN=true",
		"INSTALL_K3S_SKIP_SELINUX_RPM=true",
		"INSTALL_K3S_BIN_DIR=/opt/hauler/bin",
		//"INSTALL_K3S_SKIP_START=true",
	}...)

	return cmd.CombinedOutput()
}

func waitForDriver(ctx context.Context, cfg *rest.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 1*time.Minute)
	defer cancel()

	//TODO: This is a pretty janky way to wait for k3s to exist
	for {
		logrus.Infof("waiting for kubeconfig to exist")

		_, err := os.Stat("/etc/rancher/k3s/k3s.yaml")
		if err == nil {
			break
		}

		if ctx.Err() == context.DeadlineExceeded {
			return errors.New("timed out waiting for driver")
		}

		time.Sleep(5*time.Second)
	}

	logrus.Infof("waiting for k3s to be ready")
	sc, err := kube.NewStatusChecker(cfg, 5*time.Second, 5*time.Minute)
	if err != nil {
		return err
	}

	var objRefs []object.ObjMetadata

	//TODO: Source this from driver
	for _, dep := range []string{"coredns", "local-path-provisioner", "metrics-server", "traefik"} {
		objMeta, err := object.CreateObjMetadata("kube-system", dep, schema.GroupKind{Kind: "Deployment", Group: "apps"})
		if err != nil {
			return err
		}

		objRefs = append(objRefs, objMeta)
	}

	return sc.WaitForCondition(objRefs...)
}

////TODO: Debating whether to do this via client & helm or autodeploy with k3s, both implementations are hacked together below
//func installFleet(ctx context.Context, fsys fs.PkgFs, cfg *rest.Config) error {
//	fleetCrd := v1.HelmChart{
//		TypeMeta:   metav1.TypeMeta{
//			Kind:       "HelmChart",
//			APIVersion: "helm.cattle.io/v1",
//		},
//		ObjectMeta: metav1.ObjectMeta{
//			Name: "fleet-crd",
//			Namespace: "kube-system",
//		},
//		Spec:       v1.HelmChartSpec{
//			Chart:           "https://%{KUBERNETES_API}%/static/charts/hauler/fleet-crd-0.3.5.tgz",
//			TargetNamespace: "fleet-system",
//		},
//	}
//
//	if err := write(fsys, fleetCrd, "fleet-crd.json"); err != nil {
//		return err
//	}
//
//	fleet := v1.HelmChart{
//		TypeMeta:   metav1.TypeMeta{
//			Kind:       "HelmChart",
//			APIVersion: "helm.cattle.io/v1",
//		},
//		ObjectMeta: metav1.ObjectMeta{
//			Name:                       "fleet",
//			Namespace:                  "kube-system",
//		},
//		Spec:       v1.HelmChartSpec{
//			Chart: "https://%{KUBERNETES_API}%/static/charts/hauler/fleet-0.3.5.tgz",
//			TargetNamespace: "fleet-system",
//		},
//	}
//
//	if err := write(fsys, fleet, "fleet.json"); err != nil {
//		return err
//	}
//
//	return nil
//}
//
//func write(fsys fs.PkgFs, chart v1.HelmChart, name string) error {
//	data, err := json.Marshal(chart)
//	if err != nil {
//		return err
//	}
//
//	return os.WriteFile(filepath.Join("/var/lib/rancher/k3s/server/manifests/hauler", name), data, 0600)
//}

//TODO: Install with helm
func installFleet(ctx context.Context, fsys fs.PkgFs, cfg *rest.Config) error {

	cf := genericclioptions.NewConfigFlags(true)
	cf.KubeConfig = stringptr("/etc/rancher/k3s/k3s.yaml")

	logrus.Infof("installing fleet crds")
	if _, err := installChart(cf, "/var/lib/rancher/k3s/server/static/charts/hauler/fleet-crd-0.3.5.tgz", "fleet-crd", "fleet-system"); err != nil {
		return err
	}

	logrus.Infof("installing fleet")
	if _, err := installChart(cf, "/var/lib/rancher/k3s/server/static/charts/hauler/fleet-0.3.5.tgz", "fleet", "fleet-system"); err != nil {
		return err
	}

	return nil
}


//installChart is a helper function to install a chart located _on disk_
//TODO: This is probably wrong since it makes several fleet assumptions
func installChart(cf *genericclioptions.ConfigFlags, path string, name string, namespace string) (*release.Release, error) {
	aCfg := new(action.Configuration)
	if err := aCfg.Init(cf, namespace, os.Getenv("HELM_DRIVER"), logrus.Infof); err != nil {
		return nil, err
	}

	cf.Namespace = stringptr(namespace)

	logrus.Infof("loading chart")
	chart, err := loader.Load(path)
	if err != nil {
		return nil, err
	}

	logrus.Infof("new install")
	client := action.NewInstall(aCfg)
	client.ReleaseName = name
	client.Namespace = namespace
	client.CreateNamespace = true
	client.Wait = true

	logrus.Infof("vals")

	p := getter.All(cli.New())
	valueOpts := &values.Options{}
	vals, err := valueOpts.MergeValues(p)
	if err != nil {
		return nil, err
	}

	//TODO: More than just chart default vals
	logrus.Infof("install")
	return client.Run(chart, vals)
}

//still can't figure out why helm does it this way
func stringptr(val string) *string { return &val }
