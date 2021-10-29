package helmtemplater

import (
	"bytes"
	"fmt"

	"github.com/rancher/fleet/modules/agent/pkg/deployer"
	fleetapi "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"github.com/rancher/fleet/pkg/kustomize"
	"github.com/rancher/fleet/pkg/manifest"
	"github.com/rancher/fleet/pkg/rawyaml"
	"github.com/rancher/wrangler/pkg/apply"
	"github.com/rancher/wrangler/pkg/yaml"
	"helm.sh/helm/v3/pkg/chart"
	"k8s.io/apimachinery/pkg/api/meta"
)

type postRender struct {
	labelPrefix string
	bundleID    string
	manifest    *manifest.Manifest
	chart       *chart.Chart
	mapper      meta.RESTMapper
	opts        fleetapi.BundleDeploymentOptions
}

func (p *postRender) Run(renderedManifests *bytes.Buffer) (modifiedManifests *bytes.Buffer, err error) {
	data := renderedManifests.Bytes()

	objs, err := yaml.ToObjects(bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}

	if len(objs) == 0 {
		data = nil
	}

	newObjs, processed, err := kustomize.Process(p.manifest, data, p.opts.Kustomize.Dir)
	if err != nil {
		return nil, err
	}
	if processed {
		objs = newObjs
	}

	yamlObjs, err := rawyaml.ToObjects(p.chart)
	if err != nil {
		return nil, err
	}
	objs = append(objs, yamlObjs...)

	labels, annotations, err := apply.GetLabelsAndAnnotations(p.GetSetID(), nil)
	if err != nil {
		return nil, err
	}

	for _, obj := range objs {
		m, err := meta.Accessor(obj)
		if err != nil {
			return nil, err
		}
		m.SetLabels(mergeMaps(m.GetLabels(), labels))
		m.SetAnnotations(mergeMaps(m.GetAnnotations(), annotations))

		if p.opts.TargetNamespace != "" {
			if p.mapper != nil {
				gvk := obj.GetObjectKind().GroupVersionKind()
				mapping, err := p.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
				if err != nil {
					return nil, err
				}
				if mapping.Scope.Name() == meta.RESTScopeNameRoot {
					apiVersion, kind := gvk.ToAPIVersionAndKind()
					return nil, fmt.Errorf("invalid cluster scoped object [name=%s kind=%v apiVersion=%s] found, consider using \"defaultNamespace\", not \"namespace\" in fleet.yaml", m.GetName(),
						kind, apiVersion)
				}
			}
			m.SetNamespace(p.opts.TargetNamespace)
		}
	}

	data, err = yaml.ToBytes(objs)
	return bytes.NewBuffer(data), err
}

func (p *postRender) GetSetID() string {
	return deployer.GetSetID(p.bundleID, p.labelPrefix)
}
