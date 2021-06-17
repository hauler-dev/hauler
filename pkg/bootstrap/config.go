package bootstrap

import (
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

type BootSettings struct {
	config    *genericclioptions.ConfigFlags
	Namespace string
	KubeConfig string
}

func NewBootConfig(ns, kubepath string) *BootSettings {
	env := &BootSettings{
		Namespace:        ns,
		KubeConfig:	      kubepath,
	}

	env.config = &genericclioptions.ConfigFlags{
		Namespace:        &env.Namespace,
		KubeConfig:       &env.KubeConfig,
	}
	return env
}

// RESTClientGetter gets the kubeconfig from BootSettings
func (s *BootSettings) RESTClientGetter() genericclioptions.RESTClientGetter {
	return s.config
}