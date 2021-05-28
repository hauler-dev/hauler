package kube

import (
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"path/filepath"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewKubeClient() (client.Client, error) {
	cfg, err := NewKubeClientConfig()
	if err != nil {
		return nil, err
	}

	scheme := apiruntime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	kubeClient, err := client.New(cfg, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, err
	}

	return kubeClient, nil
}

func NewKubeClientConfig() (*rest.Config, error) {
	loadingRules := loadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	cfg, err := kubeConfig.ClientConfig()
	return cfg, err
}

func loadingRules() *clientcmd.ClientConfigLoadingRules {
	return &clientcmd.ClientConfigLoadingRules{
		Precedence:          []string{
			filepath.Join(v1alpha1.DriverEtcPath, v1alpha1.DriverK3S, "k3s.yaml"),
			filepath.Join(v1alpha1.DriverEtcPath, v1alpha1.DriverRKE2, "rke2.yaml"),
		},
		WarnIfAllMissing:    true,
	}
}