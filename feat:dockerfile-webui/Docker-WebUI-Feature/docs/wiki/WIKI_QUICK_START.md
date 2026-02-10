# Quick Start Guide

Get Hauler UI running in under 5 minutes!

## Prerequisites

- Docker & Docker Compose installed
- 2GB RAM minimum
- 10GB disk space for store

## Installation Steps

### 1. Clone the Repository

```bash
git clone <your-gitlab-repo-url>
cd hauler-ui
```

### 2. Start the Application

```bash
docker compose up -d
```

### 3. Access the UI

Open your browser to: **http://localhost:8080**

## First Tasks

### Add a Helm Repository

1. Navigate to the **Repositories** tab
2. Click **Add Repository**
3. Enter:
   - **Name**: `bitnami`
   - **URL**: `https://charts.bitnami.com/bitnami`
4. Click **Add Repository**

### Browse and Add Charts

1. Click **Browse** next to your repository
2. Select charts you want to add
3. Choose versions
4. Click **Add Selected Charts to Store**

### Create a Haul

1. Go to the **Store** tab
2. Click **Save to Haul**
3. Enter filename: `my-haul.tar.zst`
4. Click **Save**
5. Download the generated haul file

## Next Steps

- [Store Management](Store-Management) - Learn about store operations
- [Registry Operations](Registry-Operations) - Push content to registries
- [Airgap Workflows](Airgap-Workflows) - Complete airgap preparation

## Troubleshooting

**Container won't start?**
```bash
docker compose logs -f
```

**Port 8080 already in use?**
Edit `docker-compose.yml` and change the port mapping:
```yaml
ports:
  - "8081:8080"  # Use port 8081 instead
```

**Need to reset everything?**
```bash
docker compose down -v
docker compose up -d
```

## Support

- [Troubleshooting Guide](Troubleshooting)
- [FAQ](FAQ)
- [GitLab Issues](<your-issues-url>)
