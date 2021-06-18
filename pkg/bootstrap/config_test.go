package bootstrap

import (
	"testing"
)

func TestBootSettings(t *testing.T) {

	ns := "test"
	kpath := "somepath"

	settings := NewBootConfig(ns, kpath)

	if settings.Namespace != ns {
		t.Errorf("expected namespace %q, got %q", ns, settings.Namespace)
	}
	if settings.KubeConfig != kpath {
		t.Errorf("expected kube-config %q, got %q", kpath, settings.KubeConfig)
	}
}
