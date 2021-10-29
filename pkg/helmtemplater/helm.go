package helmtemplater

import (
	"time"

	"github.com/rancher/fleet/modules/agent/pkg/deployer"
	fleetapi "github.com/rancher/fleet/pkg/apis/fleet.cattle.io/v1alpha1"
	"github.com/rancher/fleet/pkg/manifest"
	"github.com/rancher/fleet/pkg/render"
	corecontrollers "github.com/rancher/wrangler/pkg/generated/controllers/core/v1"
	"github.com/rancher/wrangler/pkg/name"
	"github.com/sirupsen/logrus"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

type helm struct {
	agentNamespace      string
	serviceAccountCache corecontrollers.ServiceAccountCache
	configmapCache      corecontrollers.ConfigMapCache
	secretCache         corecontrollers.SecretCache
	getter              genericclioptions.RESTClientGetter
	globalCfg           action.Configuration
	useGlobalCfg        bool
	template            bool
	defaultNamespace    string
	labelPrefix         string
}

func (h *helm) Deploy(bundleID string, manifest *manifest.Manifest, options fleetapi.BundleDeploymentOptions) (*deployer.Resources, error) {
	if options.Helm == nil {
		options.Helm = &fleetapi.HelmOptions{}
	}
	if options.Kustomize == nil {
		options.Kustomize = &fleetapi.KustomizeOptions{}
	}

	tar, err := render.ToChart(bundleID, manifest, options)
	if err != nil {
		return nil, err
	}

	chart, err := loader.LoadArchive(tar)
	if err != nil {
		return nil, err
	}

	if chart.Metadata.Annotations == nil {
		chart.Metadata.Annotations = map[string]string{}
	}
	chart.Metadata.Annotations[ServiceAccountNameAnnotation] = options.ServiceAccount
	chart.Metadata.Annotations[BundleIDAnnotation] = bundleID
	chart.Metadata.Annotations[AgentNamespaceAnnotation] = h.agentNamespace
	if manifest.Commit != "" {
		chart.Metadata.Annotations[CommitAnnotation] = manifest.Commit
	}

	if resources, err := h.install(bundleID, manifest, chart, options, true); err != nil {
		return nil, err
	} else if h.template {
		return releaseToResources(resources)
	}

	release, err := h.install(bundleID, manifest, chart, options, false)
	if err != nil {
		return nil, err
	}

	return releaseToResources(release)
}

func (h *helm) install(bundleID string, manifest *manifest.Manifest, chart *chart.Chart, options fleetapi.BundleDeploymentOptions, dryRun bool) (*release.Release, error) {
	timeout, defaultNamespace, releaseName := h.getOpts(bundleID, options)

	values, err := h.getValues(options, defaultNamespace)
	if err != nil {
		return nil, err
	}

	cfg, err := h.getCfg(defaultNamespace, options.ServiceAccount)
	if err != nil {
		return nil, err
	}

	pr := &postRender{
		labelPrefix: h.labelPrefix,
		bundleID:    bundleID,
		manifest:    manifest,
		opts:        options,
		chart:       chart,
	}

	if !h.useGlobalCfg {
		mapper, err := cfg.RESTClientGetter.ToRESTMapper()
		if err != nil {
			return nil, err
		}
		pr.mapper = mapper
	}

	u := action.NewInstall(&cfg)
	u.ClientOnly = h.template || dryRun

	// NOTE: All this copy pasta for this :|
	// u.ForceAdopt = options.Helm.TakeOwnership

	u.Replace = true
	u.ReleaseName = releaseName
	u.CreateNamespace = true
	u.Namespace = defaultNamespace
	u.Timeout = timeout
	u.DryRun = dryRun
	u.PostRenderer = pr
	if u.Timeout > 0 {
		u.Wait = true
	}
	if !dryRun {
		logrus.Infof("Helm: Installing %s", bundleID)
	}
	return u.Run(chart, values)
}

func (h *helm) getCfg(namespace, serviceAccountName string) (action.Configuration, error) {
	var (
		cfg    action.Configuration
		getter = h.getter
	)

	if h.useGlobalCfg {
		return h.globalCfg, nil
	}

	serviceAccountNamespace, serviceAccountName, err := h.getServiceAccount(serviceAccountName)
	if err != nil {
		return cfg, err
	}

	if serviceAccountName != "" {
		getter, err = newImpersonatingGetter(serviceAccountNamespace, serviceAccountName, h.getter)
		if err != nil {
			return cfg, err
		}
	}

	kClient := kube.New(getter)
	kClient.Namespace = namespace

	err = cfg.Init(getter, namespace, "secrets", logrus.Infof)
	cfg.Releases.MaxHistory = 5
	cfg.KubeClient = kClient

	return cfg, err
}

func (h *helm) getOpts(bundleID string, options fleetapi.BundleDeploymentOptions) (time.Duration, string, string) {
	if options.Helm == nil {
		options.Helm = &fleetapi.HelmOptions{}
	}

	var timeout time.Duration
	if options.Helm.TimeoutSeconds > 0 {
		timeout = time.Second * time.Duration(options.Helm.TimeoutSeconds)
	}

	if options.TargetNamespace != "" {
		options.DefaultNamespace = options.TargetNamespace
	}

	if options.DefaultNamespace == "" {
		options.DefaultNamespace = h.defaultNamespace
	}

	// releaseName has a limit of 53 in helm https://github.com/helm/helm/blob/main/pkg/action/install.go#L58
	releaseName := name.Limit(bundleID, 53)
	if options.Helm != nil && options.Helm.ReleaseName != "" {
		releaseName = options.Helm.ReleaseName
	}

	return timeout, options.DefaultNamespace, releaseName
}

func (h *helm) getValues(options fleetapi.BundleDeploymentOptions, defaultNamespace string) (map[string]interface{}, error) {
	if options.Helm == nil {
		return nil, nil
	}

	var values map[string]interface{}
	if options.Helm.Values != nil {
		values = options.Helm.Values.Data
	}

	// do not run this when using template
	if !h.template {
		for _, valuesFrom := range options.Helm.ValuesFrom {
			var tempValues map[string]interface{}
			if valuesFrom.SecretKeyRef != nil {
				name := valuesFrom.SecretKeyRef.Name
				namespace := valuesFrom.SecretKeyRef.Namespace
				if namespace == "" {
					namespace = defaultNamespace
				}
				key := valuesFrom.SecretKeyRef.Key
				if key == "" {
					key = DefaultKey
				}
				secret, err := h.secretCache.Get(namespace, name)
				if err != nil {
					return nil, err
				}
				tempValues, err = processValuesFromObject(name, namespace, key, secret, nil)
				if err != nil {
					return nil, err
				}
			} else if valuesFrom.ConfigMapKeyRef != nil {
				name := valuesFrom.ConfigMapKeyRef.Name
				namespace := valuesFrom.ConfigMapKeyRef.Namespace
				if namespace == "" {
					namespace = defaultNamespace
				}
				key := valuesFrom.ConfigMapKeyRef.Key
				if key == "" {
					key = DefaultKey
				}
				configMap, err := h.configmapCache.Get(namespace, name)
				if err != nil {
					return nil, err
				}
				tempValues, err = processValuesFromObject(name, namespace, key, nil, configMap)
				if err != nil {
					return nil, err
				}
			}
			if tempValues != nil {
				values = mergeValues(values, tempValues)
			}
		}
	}

	return values, nil
}
