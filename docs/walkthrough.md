# Walkthrough

## Quickstart

The tl;dr for how to use `hauler` to fetch, transport, and distribute `content`:

```bash
# fetch some content
hauler store add file "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
hauler store add chart longhorn --repo "https://charts.longhorn.io"
hauler store add image "rancher/cowsay"

# transport the content
hauler store save

# <-airgap the haul.tar.zst file generated->

# load the content
hauler store load

# serve the content
hauler store serve
```

While the example above fits into a quickstart, it falls short of demonstrating all the capabilities `hauler` has to offer, including taking advantage of its fully declarative nature.  Keep reading the [Guided Examples](#Guided-Examples) below for a more thorough walkthrough of `haulers` full capabilities.

## Guided Examples

Since `hauler`'s primary objective is to simplify the content collection/distribution airgap process, a lot of the design revolves around the typical airgap workflow:

```bash
fetch -> save - | <airgap> | -> validate/load -> distribute
```

This is accomplished as follows:

```bash
# fetch content
hauler store add ...

# compress and archive content
hauler store save

# <airgap>

# validate/load content
hauler store load ...

# distribute content
hauler store serve
```

At this point you're probably wondering: what is `content`? In `hauler` land, there are a few important terms given to important resources:

* `artifact`: anything that can be represented as an [`oci artifact`](https://github.com/opencontainers/artifacts)
* `content`: built in "primitive" types of `artifacts` that `hauler` understands

### Built in content

As of today, `hauler` understands three types of `content`, one with a strong legacy of community support and consensus ([`image-spec`]()), one with a finalized spec and experimental support ([`chart-spec`]()), and one generic type created just for `hauler`.  These `content` types are outlined below:

__`files`__:

Generic content that can be represented as a file, either sourced locally or remotely.

```bash
# local file
hauler store add file path/to/local/file.txt

# remote file
hauler store add file https://get.k3s.io
```

__`images`__:

Any OCI compatible image can be fetched remotely.

```bash
# "shorthand"  image references
hauler store add image rancher/k3s:v1.22.2-k3s1

# fully qualified image references
hauler store add image ghcr.io/fluxcd/flux-cli@sha256:02aa820c3a9c57d67208afcfc4bce9661658c17d15940aea369da259d2b976dd
```

__`charts`__:

Helm charts represented as OCI content.

```bash
# add a helm chart (defaults to latest version)
hauler store add chart loki --repo "https://grafana.github.io/helm-charts"

# add a specific version of a helm chart
hauler store add chart loki --repo "https://grafana.github.io/helm-charts" --version 2.8.1

# install directly from the oci content
HELM_EXPERIMENTAL_OCI=1 helm install loki oci://localhost:3000/library/loki --version 2.8.1
```

> Note: `hauler` supports the currently experimental format of helm as OCI content, but can also be represented as the usual tarball if necessary

### Content API

While imperatively adding `content` to `hauler` is a simple way to get started, the recommended long term approach is to use the provided api that each `content` has, in conjunction with the `sync` command.

```bash
# create a haul from declaratively defined content
hauler store sync -f testdata/contents.yaml
```

> For a commented view of the `contents` api, take a look at the `testdata` folder in the root of the project.

The API for each type of built-in `content` allows you to easily and declaratively define all the `content` that exist within a `haul`, and ensures a more gitops compatible workflow for managing the lifecycle of your `hauls`.

### Collections

Earlier we referred to `content` as "primitives".  While the quotes justify the loose definition of that term, we call it that because they can be used to build groups of `content`, which we call `collections`.

`collections` are groups of 1 or more `contents` that collectively represent something desirable.  Just like `content`, there are a handful that are built in to `hauler`.

Since `collections` usually contain more purposefully crafted `contents`, we restrict their use to the declarative commands (`sync`):

```bash
# sync a collection
hauler store sync -f my-collection.yaml

# sync sets of content/collection
hauler store sync -f collection.yaml -f content.yaml
```

__`thickcharts`__:

Thick Charts represent the combination of `charts` and `images`.  When storing a thick chart, the chart _and_ the charts dependent images will be fetched and stored by `hauler`.  

```yaml
# thick-chart.yaml
apiVersion: collection.hauler.cattle.io/v1alpha1
kind: ThickCharts
metadata:
  name: loki
spec:
  charts:
    - name: loki
      repoURL: https://grafana.github.io/helm-charts
```

When syncing the collection above, `hauler` will identify the images the chart depends on and store those too

> The method for identifying images is constantly changing, as of today, the chart is rendered and a configurable set of container defining json path's are processed.  The most common paths are recognized by hauler, but this can be configured for the more niche CRDs out there.

__`k3s`__:

Combining `files` and `images`, full clusters can also be captured by `hauler` for further simplifying the already simple nature of `k3s`.

```yaml
# k3s.yaml
---
apiVersion: collection.hauler.cattle.io/v1alpha1
kind: K3s
metadata:
  name: k3s
spec:
  version: stable
```

Using the collection above, the dependent files (`k3s` executable and `https://get.k3s.io` script) will be fetched, as well as all the dependent images.

> We know not everyone uses the get.k3s.io script to provision k3s, in the future this may change, but until then you're welcome to mix and match the `collection` with any of your own additional `content` 

#### User defined `collections`

Although `content` and `collections` can only be used when they are baked in to `hauler`, the goal is to allow these to be securely user-defined, allowing you to define your own desirable `collection` types, and leave the heavy lifting to `hauler`.  Check out our [roadmap](../ROADMAP.md) and [milestones]() for more info on that.