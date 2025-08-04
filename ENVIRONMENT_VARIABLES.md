# Environment Variables Reference

This document lists all supported environment variables for Notificator configuration.

## Configuration Precedence

Configuration values are applied in the following order (highest to lowest priority):
1. Command-line flags
2. Environment variables  
3. Configuration file (JSON)
4. Default values

## Environment Variable Format

All Notificator-specific environment variables use the `NOTIFICATOR_` prefix followed by the configuration path in uppercase with dots replaced by underscores.

Example: `backend.grpc_listen` → `NOTIFICATOR_BACKEND_GRPC_LISTEN`

## Backend Configuration

### Server Settings
- `NOTIFICATOR_BACKEND_ENABLED` - Enable backend server (true/false)
- `NOTIFICATOR_BACKEND_GRPC_LISTEN` - gRPC server listen address (default: ":50051")
- `NOTIFICATOR_BACKEND_GRPC_CLIENT` - gRPC client address (default: "localhost:50051")
- `NOTIFICATOR_BACKEND_HTTP_LISTEN` - HTTP server listen address (default: ":8080")

### Database Configuration
- `NOTIFICATOR_BACKEND_DATABASE_TYPE` - Database type: "sqlite" or "postgres"
- `NOTIFICATOR_BACKEND_DATABASE_HOST` - Database host
- `NOTIFICATOR_BACKEND_DATABASE_PORT` - Database port
- `NOTIFICATOR_BACKEND_DATABASE_NAME` - Database name
- `NOTIFICATOR_BACKEND_DATABASE_USER` - Database username
- `NOTIFICATOR_BACKEND_DATABASE_PASSWORD` - Database password
- `NOTIFICATOR_BACKEND_DATABASE_SSL_MODE` - SSL mode for PostgreSQL
- `NOTIFICATOR_BACKEND_DATABASE_SQLITE_PATH` - SQLite database file path

### Common Database Environment Variables
The following standard database environment variables are also supported:
- `DATABASE_URL` - Complete database connection string
- `DB_HOST` / `DATABASE_HOST` - Database host
- `DB_PORT` / `DATABASE_PORT` - Database port
- `DB_NAME` / `DATABASE_NAME` - Database name
- `DB_USER` / `DATABASE_USER` - Database username
- `DB_PASSWORD` / `DATABASE_PASSWORD` - Database password
- `DB_SSL_MODE` / `DATABASE_SSL_MODE` - SSL mode
- `DB_PATH` / `DATABASE_PATH` - SQLite file path

## WebUI Configuration

- `NOTIFICATOR_WEBUI_LISTEN` - WebUI server listen address (default: ":8081")
- `NOTIFICATOR_WEBUI_BACKEND` - Backend gRPC server address (default: "localhost:50051")
- `BACKEND_ADDRESS` - Alternative backend address (for Docker compatibility)

## Alertmanager Configuration

- `NOTIFICATOR_ALERTMANAGERS_0_NAME` - First alertmanager name
- `NOTIFICATOR_ALERTMANAGERS_0_URL` - First alertmanager URL
- `NOTIFICATOR_ALERTMANAGERS_0_USERNAME` - First alertmanager username
- `NOTIFICATOR_ALERTMANAGERS_0_PASSWORD` - First alertmanager password
- `NOTIFICATOR_ALERTMANAGERS_0_TOKEN` - First alertmanager token
- `NOTIFICATOR_ALERTMANAGERS_0_OAUTH_ENABLED` - Enable OAuth (true/false)
- `NOTIFICATOR_ALERTMANAGERS_0_OAUTH_PROXY_MODE` - OAuth proxy mode (true/false)

## GUI Configuration

- `NOTIFICATOR_GUI_WIDTH` - Window width
- `NOTIFICATOR_GUI_HEIGHT` - Window height
- `NOTIFICATOR_GUI_TITLE` - Window title
- `NOTIFICATOR_GUI_MINIMIZE_TO_TRAY` - Minimize to tray (true/false)
- `NOTIFICATOR_GUI_START_MINIMIZED` - Start minimized (true/false)
- `NOTIFICATOR_GUI_SHOW_TRAY_ICON` - Show tray icon (true/false)
- `NOTIFICATOR_GUI_BACKGROUND_MODE` - Background mode (true/false)

## Notification Configuration

- `NOTIFICATOR_NOTIFICATIONS_ENABLED` - Enable notifications (true/false)
- `NOTIFICATOR_NOTIFICATIONS_SOUND_ENABLED` - Enable sound (true/false)
- `NOTIFICATOR_NOTIFICATIONS_SOUND_PATH` - Sound file path
- `NOTIFICATOR_NOTIFICATIONS_AUDIO_OUTPUT_DEVICE` - Audio output device
- `NOTIFICATOR_NOTIFICATIONS_SHOW_SYSTEM` - Show system notifications (true/false)
- `NOTIFICATOR_NOTIFICATIONS_CRITICAL_ONLY` - Only critical notifications (true/false)
- `NOTIFICATOR_NOTIFICATIONS_MAX_NOTIFICATIONS` - Maximum notifications
- `NOTIFICATOR_NOTIFICATIONS_COOLDOWN_SECONDS` - Cooldown between notifications
- `NOTIFICATOR_NOTIFICATIONS_RESPECT_FILTERS` - Respect GUI filters (true/false)

## Polling Configuration

- `NOTIFICATOR_POLLING_INTERVAL` - Polling interval (e.g., "30s", "1m")

## Resolved Alerts Configuration

- `NOTIFICATOR_RESOLVED_ALERTS_ENABLED` - Enable resolved alerts tracking (true/false)
- `NOTIFICATOR_RESOLVED_ALERTS_NOTIFICATIONS_ENABLED` - Send resolved alert notifications (true/false)
- `NOTIFICATOR_RESOLVED_ALERTS_RETENTION_DURATION` - How long to keep resolved alerts (e.g., "1h", "24h")

## Global Settings

- `NOTIFICATOR_LOG_LEVEL` - Log level: debug, info, warn, error

## Examples

### Docker Compose
```yaml
services:
  notificator-backend:
    image: notificator:latest
    environment:
      - NOTIFICATOR_BACKEND_DATABASE_TYPE=postgres
      - DB_HOST=postgres
      - DB_PORT=5432
      - DB_NAME=notificator
      - DB_USER=notificator
      - DB_PASSWORD=secretpassword
      - NOTIFICATOR_ALERTMANAGERS_0_URL=http://alertmanager:9093
    command: backend

  notificator-webui:
    image: notificator:latest
    environment:
      - BACKEND_ADDRESS=notificator-backend:50051
    ports:
      - "8081:8081"
    command: webui
```

### Kubernetes
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: notificator-config
data:
  NOTIFICATOR_BACKEND_DATABASE_TYPE: "postgres"
  DB_HOST: "postgres-service"
  DB_NAME: "notificator"
  NOTIFICATOR_ALERTMANAGERS_0_URL: "http://alertmanager:9093"
---
apiVersion: v1
kind: Secret
metadata:
  name: notificator-secrets
data:
  DB_PASSWORD: <base64-encoded-password>
```

### Local Development
```bash
# Use PostgreSQL instead of SQLite
export NOTIFICATOR_BACKEND_DATABASE_TYPE=postgres
export DB_HOST=localhost
export DB_PORT=5432
export DB_NAME=notificator_dev
export DB_USER=dev_user
export DB_PASSWORD=dev_password

# Custom alertmanager
export NOTIFICATOR_ALERTMANAGERS_0_URL=http://my-alertmanager:9093

# Start backend
./bin/backend

# In another terminal, start webui
export BACKEND_ADDRESS=localhost:50051
./bin/webui
```