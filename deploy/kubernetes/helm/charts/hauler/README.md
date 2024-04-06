# Hauler Helm Chart

| Type        | Chart Version | App Version |
| ----------- | ------------- | ----------- |
| application | `0.1.0`       | `1.0.2`     |

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
