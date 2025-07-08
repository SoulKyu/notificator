# Notificator

A GUI application for Alertmanager with sound and notification alerts on your laptop.

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
   ./notificator --alertmanager-url http://your-alertmanager:9093
   ```

3. The GUI will open showing your alerts with sound and notification capabilities enabled.

## Configuration

Notificator uses a JSON configuration file located at `~/.config/notificator/config.json`. The application will create a default configuration file on first run.

### Example Configuration

```json
{
  "alertmanager": {
    "url": "http://localhost:9093",
    "username": "",
    "password": "",
    "token": "",
    "headers": {},
    "oauth": {
      "enabled": false,
      "proxy_mode": true
    }
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

Perfect for keeping track of your infrastructure alerts directly from your laptop!
