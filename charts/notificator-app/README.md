# Notificator Helm Chart

A simple Helm chart for deploying Notificator with WebUI, Backend, and Alertmanager services.

## Components

- **Backend**: gRPC and HTTP API server for alert management
- **WebUI**: Web interface for viewing and managing alerts  
- **Alertmanager**: Optional Prometheus Alertmanager instance (disabled by default, for testing)

## Prerequisites

- Kubernetes 1.19+
- Helm 3.8+

## Installation

### Option 1: Install from GitHub Container Registry (Recommended)

Install directly from the published chart:

```bash
# Install with default values
helm install notificator oci://ghcr.io/soulkyu/notificator --version 0.1.0

# Install with custom values
helm install notificator oci://ghcr.io/soulkyu/notificator --version 0.1.0 \
  --set webui.ingress.host=notificator.mycompany.com \
  --set backend.database.type=postgres

# Enable internal alertmanager for testing
helm install notificator oci://ghcr.io/soulkyu/notificator --version 0.1.0 \
  --set alertmanager.enabled=true
```

### Option 2: Pull and Install Locally

```bash
# Pull the chart
helm pull oci://ghcr.io/soulkyu/notificator --version 0.1.0

# Extract and install
tar -xzvf notificator-0.1.0.tgz
helm install notificator ./notificator
```

### Option 3: Install from Source (Development)

```bash
# Clone the repository and install locally
git clone https://github.com/soulkyu/notificator.git
cd notificator
helm install notificator ./charts/notificator-app
```

## Upgrading

```bash
# Upgrade to latest version
helm upgrade notificator oci://ghcr.io/soulkyu/notificator --version 0.1.0

# Upgrade with new values
helm upgrade notificator oci://ghcr.io/soulkyu/notificator --version 0.1.0 \
  --set backend.database.postgres.password=new-secure-password
```

## Uninstalling

```bash
helm uninstall notificator
```

## Configuration

Key configuration options in `values.yaml`:

- `backend.database.*` - Database configuration (postgres or sqlite)
- `webui.ingress.*` - Ingress settings for WebUI
- `alertmanager.enabled` - Enable/disable internal alertmanager (default: false)
- `alertmanagerConfig` - List of external Alertmanager instances to monitor
- `oauth.*` - OAuth authentication settings (optional)

## Example values for production

```yaml
backend:
  database:
    type: postgres
    postgres:
      host: my-postgres.example.com
      database: notificator_prod
      user: notificator
      password: secure-password

webui:
  ingress:
    enabled: true
    host: notificator.mycompany.com
    tls:
      enabled: true
      secretName: notificator-tls

alertmanagerConfig:
  - name: "Production Alertmanager"
    url: "http://alertmanager.monitoring.svc.cluster.local:9093"

# OAuth example (GitHub and Google)
oauth:
  enabled: true
  disableClassicAuth: true
  redirectUrl: "https://notificator.mycompany.com/api/v1/oauth"
  sessionKey: "your-secure-random-session-key-here"
  
  github:
    enabled: true
    clientId: "your-github-oauth-app-client-id"
    clientSecret: "your-github-oauth-app-client-secret"
  
  google:
    enabled: true
    clientId: "your-google-oauth-client-id.apps.googleusercontent.com"
    clientSecret: "your-google-oauth-client-secret"
```