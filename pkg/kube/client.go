package kube

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"path/filepath"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewKubeConfig() (*rest.Config, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{
		Precedence:          []string{
			filepath.Join("/etc/rancher/k3s/k3s.yaml"),
			filepath.Join("/etc/rancher/rke2/rke2.yaml"),
		},
		WarnIfAllMissing:    true,
	}

	cfgOverrides := &clientcmd.ConfigOverrides{}

	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, cfgOverrides)

	return kubeConfig.ClientConfig()
}

//NewClient returns a fresh kube client
func NewClient() (client.Client, error) {
	cfg, err := NewKubeConfig()
	if err != nil {
		return nil, err
	}

	scheme := runtime.NewScheme()

	return client.New(cfg, client.Options{
		Scheme: scheme,
	})
}
