# Notificator

A GUI application for multiple Alertmanagers with sound and notification alerts on your laptop.

![alt text](img/preview.gif "Preview")

## Purpose

Notificator provides a desktop interface for monitoring Prometheus Alertmanager alerts with:

- **Visual Dashboard**: Clean GUI to view and manage alerts
- **Sound Notifications**: Audio alerts when new critical alerts arrive
- **Desktop Notifications**: System notifications for important alerts
- **Real-time Monitoring**: Auto-refresh to stay updated with latest alerts
- **Search & Filter**: Easy filtering by severity, status, team, and text search
- **Alert Management**: View alert details and create silences

## Quick Start

1. Build the application:
   ```bash
   go build -o notificator
   ```

2. Run with your Alertmanager URL:
   ```bash
   ./notificator --alertmanager-url http://127.0.0.1:9093
   ```

3. The GUI will open showing your alerts with sound and notification capabilities enabled.

## Configuration

Notificator uses a JSON configuration file located at `~/.config/notificator/config.json`. The application will create a default configuration file on first run.

### Example Configuration

```json
{
  "alertmanagers": [
    {
      "name": "default",
      "url": "http://localhost:9093",
      "username": "",
      "password": "",
      "token": "",
      "headers": {},
      "oauth": {
        "enabled": false,
        "proxy_mode": true
      }
    }
  ],
  "backend": {
    "enabled": true,
    "grpc_listen": ":50051",
    "grpc_client": "localhost:50051",
    "http_listen": ":8080"
  },
  "gui": {
    "width": 1200,
    "height": 800,
    "title": "Notificator - Alert Dashboard"
  },
  "notifications": {
    "enabled": true,
    "sound_enabled": true,
    "sound_path": "",
    "show_system": true,
    "critical_only": false,
    "max_notifications": 5,
    "cooldown_seconds": 300,
    "severity_rules": {
      "critical": true,
      "warning": true,
      "info": false,
      "unknown": false
    }
  },
  "polling": {
    "interval": "30s"
  }
}
```

### Configuration Options

- **alertmanager.url**: Alertmanager API endpoint
- **alertmanager.headers**: Custom HTTP headers for authentication
- **backend.enabled**: Enable/disable backend collaboration features
- **backend.grpc_listen**: Port for gRPC server to listen on (e.g., ":50051")
- **backend.grpc_client**: Address for gRPC client connections (e.g., "localhost:50051")
- **backend.http_listen**: Port for HTTP server (health checks, metrics) (e.g., ":8080")
- **notifications.enabled**: Enable/disable all notifications
- **notifications.sound_enabled**: Enable/disable sound alerts
- **notifications.critical_only**: Only notify for critical alerts
- **notifications.severity_rules**: Which severity levels trigger notifications
- **polling.interval**: How often to check for new alerts

### Environment Variables

You can also set headers via environment variable:

```bash
export METRICS_PROVIDER_HEADERS="X-API-Key=your-key,Authorization=Bearer token"
```

## Features

- Real-time alert monitoring
- Sound alerts for critical notifications
- Desktop notification system
- Search and filtering capabilities
- Alert silencing functionality
- Customizable notification settings
- Light/dark theme support
- Multiple Alertmanagers

Perfect for keeping track of your infrastructure alerts directly from your laptop!
