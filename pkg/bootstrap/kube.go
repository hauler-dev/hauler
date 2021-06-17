package bootstrap

import (
	"context"
	"errors"
	"github.com/rancherfederal/hauler/pkg/driver"
	"github.com/rancherfederal/hauler/pkg/kube"
	"github.com/rancherfederal/hauler/pkg/log"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"os"
	"time"
)

func waitForDriver(ctx context.Context, d driver.Driver) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	//TODO: This is a janky way of waiting for file to exist
	for {
		_, err := os.Stat(d.KubeConfigPath())
		if err == nil {
			break
		}

		if ctx.Err() == context.DeadlineExceeded {
			return errors.New("timed out waiting for driver to provision")
		}

		time.Sleep(1 * time.Second)
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
func installChart(cf *BootSettings, chart *chart.Chart, releaseName string, vals map[string]interface{}, logger log.Logger) (*release.Release, error) {
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(cf.RESTClientGetter(), cf.Namespace, os.Getenv("HELM_DRIVER"), logger.Debugf); err != nil {
		return nil, err
	}

	client := action.NewInstall(actionConfig)
	client.ReleaseName = releaseName
	client.CreateNamespace = true
	client.Wait = true

	//TODO: Do this better
	client.Namespace = cf.Namespace

	return client.Run(chart, vals)
}
