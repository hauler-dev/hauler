# Hauler Helm Chart

### Airgap Swiss Army Knife

`Rancher Government Hauler` simplifies the airgap experience without requiring operators to adopt a specific workflow. **Hauler** simplifies the airgapping process, by representing assets (images, charts, files, etc...) as content and collections to allow operators to easily fetch, store, package, and distribute these assets with declarative manifests or through the command line.

`Hauler` does this by storing contents and collections as OCI Artifacts and allows operators to serve contents and collections with an embedded registry and fileserver. Additionally, `Hauler` has the ability to store and inspect various non-image OCI Artifacts.

**GitHub Repostiory:** https://github.com/rancherfederal/hauler

**Documentation:** http://hauler.dev

---

| Type        | Chart Version | App Version |
| ----------- | ------------- | ----------- |
| application | `1.0.3`       | `1.0.3`     |

## Installing the Chart

```bash
helm install hauler hauler/hauler -n hauler-system -f values.yaml
```

```bash
helm status hauler -n hauler-system
```

## Uninstalling the Chart

```bash
helm uninstall hauler -n hauler-system
```
