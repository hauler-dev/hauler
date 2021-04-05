package app

const (
	packageConfigStr = `apiVersion: hauler.cattle.io/v1alpha1
kind: PackageConfig
metadata:
  name: hauler-test
packages:
  - name: k3s-main
    type: K3s
    release: v1.20.5+k3s1
    installScriptRef: 355fff3017b06cde44dbd879408a3a6826fa7125
`
)
