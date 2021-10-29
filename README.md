# Hauler: Airgap Assistant

__⚠️ WARNING: This is an experimental, work in progress project.  _Everything_ is subject to change, and it is actively in development, so let us know what you think!__

`hauler` is a command line tool for that aims to simplify the painpoints that exist around airgapped Kubernetes deployments.
It remains as unopinionated as possible, and does _not_ attempt to enforce a specific cluster type or application deployment model.
Instead, it focuses solely on simplifying the primary airgap pain points:
* artifact collection
* artifact distribution

`hauler` achieves this by leaning heavily on the [oci spec](https://github.com/opencontainers), and the vast ecosystem of tooling available for fetching and distributing oci content.
