# Notificator Helm Chart

A simple Helm chart for deploying Notificator with WebUI, Backend, and Alertmanager services.

## Components

- **Backend**: gRPC and HTTP API server for alert management
- **WebUI**: Web interface for viewing and managing alerts  
- **Alertmanager**: Prometheus Alertmanager instance (for testing)

## Installation

```bash
helm install notificator ./helm/notificator
```

## Configuration

Key configuration options in `values.yaml`:

- `backend.database.*` - Database configuration (postgres or sqlite)
- `webui.ingress.*` - Ingress settings for WebUI
- `alertmanagerConfig` - List of Alertmanager instances to monitor
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