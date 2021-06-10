package bootstrap

import (
	"context"
	"errors"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/kube"
	log "github.com/sirupsen/logrus"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"os"
	"path/filepath"
	"time"
)

func waitForDriver(ctx context.Context, d v1alpha1.Drive) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	//TODO: This is a janky way of waiting for file to exist
	path := filepath.Join(d.EtcPath(), "k3s.yaml")
	for {
		_, err := os.Stat(path)
		if err == nil {
			break
		}

		if ctx.Err() == context.DeadlineExceeded {
			return errors.New("timed out waiting for driver to provision")
		}

		time.Sleep(1*time.Second)
	}

	cfg, err := kube.NewKubeConfig()
	if err != nil {
		return err
	}

	sc, err := kube.NewStatusChecker(cfg, 5*time.Second, 5*time.Minute)
	if err != nil {
		return err
	}

	return sc.WaitForCondition(d.SystemObjects()...)
}

//TODO: This is likely way too fleet specific
func installChart(cf *genericclioptions.ConfigFlags, chart *chart.Chart, releaseName, namespace string, vals map[string]interface{}) (*release.Release, error) {
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(cf, namespace, os.Getenv("HELM_DRIVER"), log.Debugf); err != nil {
		return nil, err
	}

	client := action.NewInstall(actionConfig)
	client.ReleaseName = releaseName
	client.Namespace, cf.Namespace = namespace, stringptr(namespace) 	// TODO: Not sure why this needs to be set twice
	client.CreateNamespace = true
	client.Wait = true

	return client.Run(chart, vals)
}

//still can't figure out why helm does it this way
func stringptr(val string) *string { return &val }
