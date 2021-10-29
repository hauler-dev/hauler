package helmtemplater

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"

	"github.com/rancher/fleet/modules/agent/pkg/deployer"
	fleetapi "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"github.com/rancher/fleet/pkg/manifest"
	"github.com/rancher/wrangler/pkg/yaml"
	"github.com/sirupsen/logrus"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	BundleIDAnnotation           = "fleet.cattle.io/bundle-id"
	CommitAnnotation             = "fleet.cattle.io/commit"
	AgentNamespaceAnnotation     = "fleet.cattle.io/agent-namespace"
	ServiceAccountNameAnnotation = "fleet.cattle.io/service-account"
	DefaultServiceAccount        = "fleet-default"
)

var (
	ErrNoRelease = errors.New("failed to find release")
	DefaultKey   = "values.yaml"
)

func Template(bundleID string, manifest *manifest.Manifest, options fleetapi.BundleDeploymentOptions) ([]runtime.Object, error) {
	h := &helm{
		globalCfg:    action.Configuration{},
		useGlobalCfg: true,
		template:     true,
	}

	mem := driver.NewMemory()
	mem.SetNamespace("default")

	h.globalCfg.Capabilities = chartutil.DefaultCapabilities
	h.globalCfg.KubeClient = &fake.PrintingKubeClient{Out: ioutil.Discard}
	h.globalCfg.Log = logrus.Infof
	h.globalCfg.Releases = storage.Init(mem)

	resources, err := h.Deploy(bundleID, manifest, options)
	if err != nil {
		return nil, err
	}

	return resources.Objects, nil
}

func releaseToResources(release *release.Release) (*deployer.Resources, error) {
	var (
		err error
	)
	resources := &deployer.Resources{
		DefaultNamespace: release.Namespace,
		ID:               fmt.Sprintf("%s/%s:%d", release.Namespace, release.Name, release.Version),
	}

	resources.Objects, err = yaml.ToObjects(bytes.NewBufferString(release.Manifest))
	return resources, err
}

func processValuesFromObject(name, namespace, key string, secret *corev1.Secret, configMap *corev1.ConfigMap) (map[string]interface{}, error) {
	var m map[string]interface{}
	if secret != nil {
		values, ok := secret.Data[key]
		if !ok {
			return nil, fmt.Errorf("key %s is missing from secret %s/%s, can't use it in valuesFrom", key, namespace, name)
		}
		if err := yaml.Unmarshal(values, &m); err != nil {
			return nil, err
		}
	} else if configMap != nil {
		values, ok := configMap.Data[key]
		if !ok {
			return nil, fmt.Errorf("key %s is missing from configmap %s/%s, can't use it in valuesFrom", key, namespace, name)
		}
		if err := yaml.Unmarshal([]byte(values), &m); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func mergeMaps(base, other map[string]string) map[string]string {
	result := map[string]string{}
	for k, v := range base {
		result[k] = v
	}
	for k, v := range other {
		result[k] = v
	}
	return result
}

// mergeValues merges source and destination map, preferring values
// from the source values. This is slightly adapted from:
// https://github.com/helm/helm/blob/2332b480c9cb70a0d8a85247992d6155fbe82416/cmd/helm/install.go#L359
func mergeValues(dest, src map[string]interface{}) map[string]interface{} {
	for k, v := range src {
		// If the key doesn't exist already, then just set the key to that value
		if _, exists := dest[k]; !exists {
			dest[k] = v
			continue
		}
		nextMap, ok := v.(map[string]interface{})
		// If it isn't another map, overwrite the value
		if !ok {
			dest[k] = v
			continue
		}
		// Edge case: If the key exists in the destination, but isn't a map
		destMap, isMap := dest[k].(map[string]interface{})
		// If the source map has a map for this key, prefer it
		if !isMap {
			dest[k] = v
			continue
		}
		// If we got to this point, it is a map in both, so merge them
		dest[k] = mergeValues(destMap, nextMap)
	}
	return dest
}
