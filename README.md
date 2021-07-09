# Hauler - Kubernetes Air Gap Migration

__⚠️ WARNING: This is an experimental, work in progress project.  _Everything_ is subject to change, and it is actively in development, so let us know what you think!__

## Overview

Hauler is a collection of tools that makes moving Kubernetes-focused _stuff_ into an air gap easier.

Specifically, hauler wants to make the following processes easier while making minimal assumptions:
- deploying specific containerized workloads onto a host
- populating an OCI registry with container images
- downloading required files to a host

## Practical Example

As an introduction, let's look at a concrete example - what does it take to deploy a Docker registry, cert-manager, and a toy NGINX web server using Hauler.

### Hauler Package

In the current version of Hauler, we'll need a directory holding all of our configs.
Make a directory to hold everything and have a fresh working folder:
```bash
export hauler_ex_dir="$(pwd)/hauler-example"
mkdir -p "${hauler_ex_dir}"
cd "${hauler_ex_dir}"
```

A Hauler package defined using a config file needs static k3s and fleet versions defined, so let's set that up to start:
```bash
cat <<EOF > hauler-package-ex.yaml
apiVersion: hauler.cattle.io/v1alpha1
kind: Package
metadata:
  name: hauler-package-ex
spec:
  driver:
    type: k3s
    version: v1.20.8+k3s1
  fleet:
    version: v0.3.5
EOF
```

#### Docker Registry

TODO - WIP

#### cert-manager
Next, let's target cert-manager v1.4.0 to be installed into the Hauler cluster.

We'll first make a new directory for this workload to keep everything organized.
```bash
mkdir cert-manager
cd cert-manager
```

#### Helm Chart and YAML

Cert-manager needs custom resource definitions installed before its Helm chart is installed, so download that and place it into the folder alongside of the `fleet.yaml` specifying the chart to install as well.
```bash
curl -LO https://github.com/jetstack/cert-manager/releases/download/v1.4.0/cert-manager.crds.yaml
cat <<EOF > fleet.yaml
helm:
  repo: https://charts.jetstack.io
  chart: cert-manager
  version: v1.4.0
  releaseName: cert-manager
EOF
```

#### Extra containers

If you were to run `hauler package build -p ./` right now, you'd see output containing the following:
```
< other output ... >
 INFO  Packaging image (1/3): quay.io/jetstack/cert-manager-controller:v1.4.0
 INFO  Packaging image (2/3): quay.io/jetstack/cert-manager-webhook:v1.4.0
 INFO  Packaging image (3/3): quay.io/jetstack/cert-manager-cainjector:v1.4.0
< other output ... >
 ```

Hauler's auto-detection saw the chart launched 3 workloads using these images; **_EVEN SO,_** cert-manager also uses `quay.io/jetstack/cert-manager-acmesolver:v1.4.0` for short-lived workloads to fulfill specific needs.


This means we need to add this additional image to our Hauler package definition to ensure it's available for use inside the cluster:
```bash
cd cd "${hauler_ex_dir}"
cat <<EOF > hauler-package-ex.yaml
apiVersion: hauler.cattle.io/v1alpha1
kind: Package
metadata:
  name: hauler-package-ex
spec:
  driver:
    type: k3s
    version: v1.20.8+k3s1
  fleet:
    version: v0.3.5
  paths:
    - registry
    - cert-manager
  images:
    - quay.io/jetstack/cert-manager-acmesolver:v1.4.0
EOF
```

