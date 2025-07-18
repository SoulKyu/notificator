# Notificator Backend - Helm Chart

So you want to deploy the Notificator backend on Kubernetes? This chart will help you get it running with all the collaborative features.

## What you need before starting

- Kubernetes cluster (1.19 or newer should work fine)
- Helm 3.x installed
- Some storage for SQLite OR a PostgreSQL database
- Basic knowledge of Kubernetes (you'll figure it out!)

## Quick deployment

The easiest way to get started:

```bash
helm install my-notificator ./notificator-backend
```

That's it! It will use SQLite by default, which is perfect for testing.

## Configuration options

Here's the main stuff you can configure. Don't worry about setting everything - the defaults work fine for most cases.

### Basic settings

```yaml
# How many pods you want (start with 1)
replicaCount: 1

# Your Docker image
image:
  repository: notificator-backend
  tag: "latest"  # or whatever version you built
  pullPolicy: IfNotPresent

# Ports the service will use
service:
  grpcPort: 50051  # for the gRPC API
  httpPort: 8080   # for health checks
```

### Database setup

You have two choices here:

#### Option 1: SQLite (simple, good for dev)
```yaml
backend:
  database:
    type: sqlite
    sqlite:
      path: "/data/notificator.db"

# Don't forget to enable storage
persistence:
  enabled: true
  size: 1Gi
```

#### Option 2: PostgreSQL (better for production)
```yaml
backend:
  database:
    type: postgres
    postgres:
      host: "your-postgres-host"
      database: "notificator"
      user: "notificator"
      password: "your-password"  # or use secrets, see below
```

### Making it accessible (Ingress)

If you want to access the backend from outside the cluster:

```yaml
ingress:
  enabled: true
  className: "nginx"  # or whatever ingress you use
  hosts:
    - host: notificator.example.com
      paths:
        - path: /
          pathType: Prefix
  
  # Important: this annotation is required for gRPC!
  annotations:
    nginx.ingress.kubernetes.io/backend-protocol: "GRPC"
```

## Real-world examples

### Development setup
Perfect for testing on your laptop with minikube:

```bash
# Create a simple dev deployment
helm install notificator-dev ./notificator-backend \
  --set backend.database.type=sqlite \
  --set persistence.size=500Mi
```

### Production with PostgreSQL
What I use in production:

```yaml
# production-values.yaml
replicaCount: 2

backend:
  database:
    type: postgres
    postgres:
      host: "postgres.database.svc.cluster.local"
      database: "notificator"
      user: "notificator"
      existingSecret: "postgres-secret"  # much better than plain password

ingress:
  enabled: true
  className: "nginx"
  annotations:
    nginx.ingress.kubernetes.io/backend-protocol: "GRPC"
    cert-manager.io/cluster-issuer: "letsencrypt"
  hosts:
    - host: alerts.mycompany.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: alerts-tls
      hosts:
        - alerts.mycompany.com

resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi
```

Then deploy with:
```bash
helm install notificator-prod ./notificator-backend -f production-values.yaml
```

## Setting up the database

### PostgreSQL preparation
First, create your database and user:

```sql
CREATE DATABASE notificator;
CREATE USER notificator WITH PASSWORD 'your-secure-password';
GRANT ALL PRIVILEGES ON DATABASE notificator TO notificator;
```

Then create a Kubernetes secret:
```bash
kubectl create secret generic postgres-secret \
  --from-literal=postgres-password=your-secure-password
```

### Database migrations
Database migrations are run automatically on startup, so no manual intervention is needed. The migrations are idempotent, so they can be run multiple times safely.

## Connecting your desktop clients

Once the backend is running, configure your desktop clients to connect:

```json
{
  "backend": {
    "enabled": true,
    "grpc_client": "alerts.mycompany.com:443"
  }
}
```

For development (port-forward):
```bash
kubectl port-forward svc/notificator-backend 50051:50051
```

Then use:
```json
{
  "backend": {
    "enabled": true,
    "grpc_client": "localhost:50051"
  }
}
```

## Troubleshooting

### gRPC connection not working?

1. Check if the annotation is correct:
```bash
kubectl get ingress notificator-backend -o yaml | grep backend-protocol
```

2. Test the connection:
```bash
# From inside the cluster
kubectl run test-pod --rm -i --tty --image=alpine \
  -- telnet notificator-backend 50051
```

3. Check the backend logs:
```bash
kubectl logs -l app.kubernetes.io/name=notificator-backend
```

### Database issues?

The most common problem is wrong credentials. Double-check your secret:
```bash
kubectl get secret postgres-secret -o yaml
```

### Pod not starting?

Usually it's a config issue. Check the logs:
```bash
kubectl describe pod -l app.kubernetes.io/name=notificator-backend
kubectl logs -l app.kubernetes.io/name=notificator-backend
```

## Advanced stuff

### Auto-scaling
If you have lot of users:

```yaml
autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70
```

### Security hardening
For production environments:

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  readOnlyRootFilesystem: true
  
podSecurityContext:
  fsGroup: 1000
```

### Monitoring
The backend exposes some endpoints:
- `/health` - is the service alive?
- `/ready` - is it ready to serve requests?
- `/metrics` - Prometheus metrics (if you need them)

## Clean up

To remove everything:
```bash
helm uninstall notificator-backend
```

Note: This won't delete the PVC (persistent volume), so your data is safe. To delete everything including data:
```bash
kubectl delete pvc -l app.kubernetes.io/name=notificator-backend
```
