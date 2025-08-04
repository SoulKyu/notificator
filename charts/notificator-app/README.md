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

# With PostgreSQL connection URL (simplest database config)
helm install notificator oci://ghcr.io/soulkyu/notificator --version 0.1.0 \
  --set backend.database.postgresUrl="postgres://user:pass@host:5432/db?sslmode=require"

# Enable internal alertmanager for testing
helm install notificator oci://ghcr.io/soulkyu/notificator --version 0.1.0 \
  --set alertmanager.enabled=true

# With ingress annotations (e.g., for SSL redirect and cert-manager)
helm install notificator oci://ghcr.io/soulkyu/notificator --version 0.1.0 \
  --set webui.ingress.host=notificator.mycompany.com \
  --set webui.ingress.annotations."nginx\.ingress\.kubernetes\.io/ssl-redirect"="true" \
  --set webui.ingress.annotations."cert-manager\.io/cluster-issuer"="letsencrypt-prod"

# With custom labels and annotations for monitoring/service mesh
helm install notificator oci://ghcr.io/soulkyu/notificator --version 0.1.0 \
  --set backend.labels.environment=production \
  --set backend.labels.team=platform \
  --set backend.annotations."prometheus\.io/scrape"="true" \
  --set webui.podAnnotations."sidecar\.istio\.io/inject"="true"

# With security contexts for enhanced security
helm install notificator oci://ghcr.io/soulkyu/notificator --version 0.1.0 \
  --set backend.podSecurityContext.runAsNonRoot=true \
  --set backend.podSecurityContext.runAsUser=1001 \
  --set backend.securityContext.allowPrivilegeEscalation=false \
  --set webui.securityContext.readOnlyRootFilesystem=false

# With custom ServiceAccount for AWS IAM roles (IRSA)
helm install notificator oci://ghcr.io/soulkyu/notificator --version 0.1.0 \
  --set backend.serviceAccount.annotations."eks\.amazonaws\.com/role-arn"="arn:aws:iam::123456789012:role/notificator-backend" \
  --set webui.serviceAccount.annotations."eks\.amazonaws\.com/role-arn"="arn:aws:iam::123456789012:role/notificator-webui"
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
  - `backend.database.postgresUrl` - PostgreSQL connection URL (preferred)
  - `backend.database.postgres.*` - Individual PostgreSQL settings
- `webui.ingress.*` - Ingress settings for WebUI
- `webui.ingress.annotations` - Custom annotations for the ingress
- `alertmanager.enabled` - Enable/disable internal alertmanager (default: false)
- `alertmanagerConfig` - List of external Alertmanager instances to monitor
- `oauth.*` - OAuth authentication settings (optional)

**Labels and Annotations Support:**
- `<component>.labels` - Custom labels for all resources of a component
- `<component>.annotations` - Custom annotations for all resources of a component
- `<component>.podLabels` - Custom labels for pods only
- `<component>.podAnnotations` - Custom annotations for pods only
- `<component>.service.labels` - Custom labels for services only
- `<component>.service.annotations` - Custom annotations for services only

**Security Context Support:**
- `<component>.podSecurityContext` - Pod-level security context (runAsUser, fsGroup, etc.)
- `<component>.securityContext` - Container-level security context (capabilities, readOnlyRootFilesystem, etc.)

**ServiceAccount Support:**
- `<component>.serviceAccount.create` - Create ServiceAccount (default: true)
- `<component>.serviceAccount.name` - ServiceAccount name (auto-generated if empty)
- `<component>.serviceAccount.annotations` - ServiceAccount annotations (useful for AWS IRSA, etc.)
- `<component>.serviceAccount.labels` - ServiceAccount labels

Where `<component>` can be: `backend`, `webui`, or `alertmanager`

## Example values for production

```yaml
backend:
  # Custom labels and annotations for backend
  labels:
    environment: production
    team: platform
    version: v1.0.0
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "8080"
    prometheus.io/path: "/metrics"
  podAnnotations:
    sidecar.istio.io/inject: "true"
  service:
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
  
  # Security contexts for enhanced security
  podSecurityContext:
    runAsNonRoot: true
    runAsUser: 1001
    runAsGroup: 1001
    fsGroup: 1001
  securityContext:
    allowPrivilegeEscalation: false
    capabilities:
      drop:
      - ALL
    readOnlyRootFilesystem: false  # Set to false if app needs to write temp files
    runAsNonRoot: true
    runAsUser: 1001
  
  # ServiceAccount for AWS IAM roles (IRSA)
  serviceAccount:
    create: true
    annotations:
      eks.amazonaws.com/role-arn: "arn:aws:iam::123456789012:role/notificator-backend"
  
  database:
    type: postgres
    # Use PostgreSQL connection URL (recommended)
    postgresUrl: "postgres://notificator:secure-password@my-postgres.example.com:5432/notificator_prod?sslmode=require"
    
    # OR use individual settings
    # postgres:
    #   host: my-postgres.example.com
    #   database: notificator_prod
    #   user: notificator
    #   password: secure-password

webui:
  # Custom labels and annotations for webui
  labels:
    environment: production
    team: frontend
  podLabels:
    app.kubernetes.io/component: webui
  service:
    labels:
      service-type: web
    annotations:
      prometheus.io/scrape: "true"
      prometheus.io/port: "8081"
  
  # Security contexts for webui
  podSecurityContext:
    runAsNonRoot: true
    runAsUser: 1001
    fsGroup: 1001
  securityContext:
    allowPrivilegeEscalation: false
    capabilities:
      drop:
      - ALL
    readOnlyRootFilesystem: false
    runAsNonRoot: true
    runAsUser: 1001
  
  # ServiceAccount for AWS IAM roles (IRSA)
  serviceAccount:
    create: true
    annotations:
      eks.amazonaws.com/role-arn: "arn:aws:iam::123456789012:role/notificator-webui"
  
  ingress:
    enabled: true
    className: nginx
    annotations:
      nginx.ingress.kubernetes.io/ssl-redirect: "true"
      nginx.ingress.kubernetes.io/force-ssl-redirect: "true"
      cert-manager.io/cluster-issuer: "letsencrypt-prod"
      nginx.ingress.kubernetes.io/rate-limit: "100"
    host: notificator.mycompany.com
    tls:
      enabled: true
      secretName: notificator-tls

# Enable alertmanager with custom labels/annotations
alertmanager:
  enabled: true
  labels:
    environment: production
    team: sre
  podAnnotations:
    sidecar.istio.io/inject: "false"
  
  # Security contexts for alertmanager
  podSecurityContext:
    runAsNonRoot: true
    runAsUser: 1001
    fsGroup: 1001
  securityContext:
    allowPrivilegeEscalation: false
    capabilities:
      drop:
      - ALL
    readOnlyRootFilesystem: false
    runAsNonRoot: true
    runAsUser: 1001

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