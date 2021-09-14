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

> **_NOTE:_** blocks like these will indicate code examples that are NOT intended to be used as-is. These are intended to show in-progress steps for completeness.

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

#### Toy NGINX server

As a super simple example, we can craft a Deployment YAML to have a basic NGINX web server deployed by Hauler.

We'll start by creating a folder for this workload:
```bash
mkdir nginx
cd nginx
```

Since Hauler uses fleet under the hood, we can simply place a YAML in this folder to have fleet deploy that YAML:
```bash
cat <<EOF > nginx_deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: nginx
  name: nginx
spec:
  replicas: 1
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - image: nginx:1.17
        name: nginx
EOF
```

We'll add this folder into the Hauler Package to tell Hauler to deploy this workload:
```bash
cd "${hauler_ex_dir}"

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
    - nginx
EOF
```

#### cert-manager

Next, let's target cert-manager v1.4.0 to be installed into the Hauler cluster.

We'll first make another new directory for this workload to keep everything organized.
```bash
mkdir -p cert-manager/crds cert-manager/controller
cd cert-manager
```

#### Helm Chart and YAML

Cert-manager needs custom resource definitions installed before its Helm chart is installed, so download that and place it into the folder alongside of the `fleet.yaml` specifying the chart to install as well.
```bash
curl -L https://github.com/jetstack/cert-manager/releases/download/v1.4.0/cert-manager.crds.yaml \
  -o crds/cert-manager.crds.yaml

cat <<EOF > controller/fleet.yaml
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
 INFO  Creating bundle from path: cert-manager
 INFO  Packaging image (1/3): quay.io/jetstack/cert-manager-cainjector:v1.4.0
 INFO  Packaging image (2/3): quay.io/jetstack/cert-manager-controller:v1.4.0
 INFO  Packaging image (3/3): quay.io/jetstack/cert-manager-webhook:v1.4.0
 SUCCESS  Finished packaging 1 bundle(s) along with 3 autodetected image(s)
 INFO  Packaging 0 user defined images
 SUCCESS  Finished packaging 0 user defined images
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
    - nginx
    - cert-manager
  images:
    - quay.io/jetstack/cert-manager-acmesolver:v1.4.0
EOF
```

