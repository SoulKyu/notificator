# Fake Alertmanager

A comprehensive, OpenAPI v2-compliant fake Alertmanager for testing purposes. This tool generates realistic alerts and provides the same API endpoints as Prometheus Alertmanager, making it perfect for integration testing without needing a full Prometheus stack.

## 🚀 Quick Start

### Prerequisites

- Python 3.9+
- [uv](https://github.com/astral-sh/uv) package manager

### Installation

1. **Clone or download the project files**
2. **Initialize the Python environment with uv:**
   ```bash
   uv venv
   source .venv/bin/activate  # On Windows: .venv\Scripts\activate
   ```

3. **Install dependencies:**
   ```bash
   uv sync
   ```

4. **Run the fake Alertmanager:**
   ```bash
   uv run fake_alertmanager.py
   ```

The fake Alertmanager will start on `http://localhost:9093` and begin generating random alerts automatically.

## 📋 Features

### ✅ Complete OpenAPI v2 Implementation
- **Full API compliance** with Alertmanager OpenAPI v2 specification
- **Realistic alert generation** with multiple alert types and severities
- **Alert grouping** by receiver and alertname
- **Silence management** with full CRUD operations
- **Query filtering** support for all list endpoints
- **Legacy v1 API** compatibility for backward compatibility

### 🎯 Supported Endpoints

#### API v2 (Primary)
```
GET    /api/v2/status              # Cluster and version info
GET    /api/v2/receivers           # List notification receivers
GET    /api/v2/alerts              # Get alerts with filtering
POST   /api/v2/alerts              # Create new alerts
GET    /api/v2/alerts/groups       # Get grouped alerts
GET    /api/v2/silences            # List silences
POST   /api/v2/silences            # Create/update silences
GET    /api/v2/silence/{id}        # Get specific silence
DELETE /api/v2/silence/{id}        # Delete silence
```

#### Health Checks
```
GET    /-/healthy                  # Health check
GET    /-/ready                    # Ready check
```

#### Legacy v1 (Compatibility)
```
GET    /api/v1/alerts              # Legacy alerts endpoint
GET    /api/v1/alerts/groups       # Legacy alert groups
GET    /api/v1/silences            # Legacy silences
POST   /api/v1/silences            # Legacy create silence
GET    /api/v1/status              # Legacy status
GET    /api/v1/receivers           # Legacy receivers
```

## 🧪 Testing Your Integration

### 1. Basic Health Check
```bash
curl http://localhost:9093/-/healthy
```

### 2. Get All Alerts
```bash
curl http://localhost:9093/api/v2/alerts
```

### 3. Filter Alerts
```bash
# Get only active alerts
curl "http://localhost:9093/api/v2/alerts?active=true&silenced=false"

# Filter by severity
curl "http://localhost:9093/api/v2/alerts?filter=severity=critical"

# Filter by multiple criteria
curl "http://localhost:9093/api/v2/alerts?filter=alertname=HighCPUUsage&filter=team=infrastructure"
```

### 4. Get Alert Groups
```bash
curl http://localhost:9093/api/v2/alerts/groups
```

### 5. Create Custom Alerts
```bash
curl -X POST http://localhost:9093/api/v2/alerts \
  -H "Content-Type: application/json" \
  -d '[{
    "labels": {
      "alertname": "TestAlert",
      "severity": "warning",
      "instance": "test-server-1",
      "job": "test-job",
      "team": "platform"
    },
    "annotations": {
      "description": "This is a test alert for integration testing",
      "summary": "Test alert on test-server-1",
      "runbook_url": "https://runbooks.example.com/testalert"
    },
    "generatorURL": "http://prometheus:9090/graph?g0.expr=up"
  }]'
```

### 6. Silence Management
```bash
# Create a silence
curl -X POST http://localhost:9093/api/v2/silences \
  -H "Content-Type: application/json" \
  -d '{
    "matchers": [{
      "name": "alertname",
      "value": "TestAlert",
      "isRegex": false,
      "isEqual": true
    }],
    "startsAt": "2025-07-09T10:00:00Z",
    "endsAt": "2025-07-09T18:00:00Z",
    "createdBy": "devops@example.com",
    "comment": "Planned maintenance window"
  }'

# List all silences
curl http://localhost:9093/api/v2/silences

# Delete a silence (replace {id} with actual silence ID)
curl -X DELETE http://localhost:9093/api/v2/silence/{id}
```

## ⚙️ Configuration

### Customizing Alert Types

Edit the `ALERT_TEMPLATES` list in `fake_alertmanager.py`:

```python
ALERT_TEMPLATES = [
    {
        "alertname": "YourCustomAlert",
        "severity": "critical",
        "instance": "server-{instance}.yourdomain.com",
        "job": "your-exporter",
        "team": "your-team",
        "description": "Your custom alert description",
        "summary": "Custom alert on {instance}"
    },
    # Add more templates...
]
```

### Adjusting Alert Generation

Modify the `alert_generator()` function:

```python
def alert_generator():
    while True:
        # Increase/decrease generation frequency
        if random.random() < 0.4:  # 40% chance every 15 seconds
            new_alert = generate_random_alert()
            alerts.append(new_alert)
        
        # Change retention policy
        if len(alerts) > 100:  # Keep last 100 alerts
            alerts.pop(0)
        
        time.sleep(15)  # Generation interval
```