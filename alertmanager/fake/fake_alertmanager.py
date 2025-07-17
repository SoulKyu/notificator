#!/usr/bin/env python3

import json
import random
import time
from datetime import datetime, timedelta, UTC
from flask import Flask, jsonify, request
from threading import Thread
import uuid

app = Flask(__name__)

# Global storage for alerts and silences
alerts = []
alert_groups = []
silences = []

# Sample alert templates
ALERT_TEMPLATES = [
    {
        "alertname": "HighCPUUsage",
        "severity": "warning",
        "instance": "server-{instance}.example.com",
        "job": "node-exporter",
        "team": "infrastructure",
        "description": "CPU usage is above 80% for more than 5 minutes",
        "summary": "High CPU usage on {instance}"
    },
    {
        "alertname": "DiskSpaceLow",
        "severity": "critical",
        "instance": "server-{instance}.example.com",
        "job": "node-exporter",
        "team": "infrastructure",
        "description": "Disk space is below 10% on {instance}",
        "summary": "Low disk space on {instance}"
    },
    {
        "alertname": "ServiceDown",
        "severity": "critical",
        "instance": "service-{instance}.example.com",
        "job": "blackbox",
        "team": "platform",
        "description": "Service {instance} is not responding to health checks",
        "summary": "Service {instance} is down"
    },
    {
        "alertname": "HighMemoryUsage",
        "severity": "warning",
        "instance": "server-{instance}.example.com",
        "job": "node-exporter",
        "team": "infrastructure",
        "description": "Memory usage is above 90% for more than 10 minutes",
        "summary": "High memory usage on {instance}"
    },
    {
        "alertname": "DatabaseConnectionError",
        "severity": "critical",
        "instance": "db-{instance}.example.com",
        "job": "mysql-exporter",
        "team": "database",
        "description": "Cannot connect to database {instance}",
        "summary": "Database connection failed on {instance}"
    }
]

RECEIVER_NAMES = ["web.hook", "email-team", "slack-critical", "pagerduty", "discord-alerts"]

def generate_fingerprint():
    """Generate a unique fingerprint for an alert"""
    return ''.join(random.choices('0123456789abcdef', k=16))

def generate_random_alert():
    """Generate a random alert based on templates"""
    template = random.choice(ALERT_TEMPLATES)
    instance_id = random.randint(1, 20)
    instance_name = template["instance"].format(instance=instance_id)
    
    starts_at = datetime.now(UTC) - timedelta(minutes=random.randint(1, 30))
    ends_at = starts_at + timedelta(hours=random.randint(1, 6))
    
    alert = {
        "labels": {
            "alertname": template["alertname"],
            "severity": template["severity"],
            "instance": instance_name,
            "job": template["job"],
            "team": template["team"]
        },
        "annotations": {
            "description": template["description"].format(instance=instance_name),
            "summary": template["summary"].format(instance=instance_name),
            "runbook_url": f"https://runbooks.example.com/{template['alertname'].lower()}"
        },
        "startsAt": starts_at.isoformat() + "Z",
        "endsAt": ends_at.isoformat() + "Z",
        "updatedAt": datetime.now(UTC).isoformat() + "Z",
        "generatorURL": f"http://prometheus:9090/graph?g0.expr=up{{job=\"{template['job']}\"}}&g0.tab=1",
        "fingerprint": generate_fingerprint(),
        "receivers": [{"name": random.choice(RECEIVER_NAMES)}],
        "status": {
            "state": random.choice(["active", "suppressed", "unprocessed"]),
            "silencedBy": [],
            "inhibitedBy": [],
            "mutedBy": []
        }
    }
    
    # Randomly silence some alerts
    if random.random() < 0.2:  # 20% chance to be silenced
        silence_id = str(uuid.uuid4())
        alert["status"]["state"] = "suppressed"
        alert["status"]["silencedBy"] = [silence_id]
    
    return alert

def create_alert_groups():
    """Create alert groups from individual alerts according to OpenAPI spec"""
    global alert_groups
    groups = {}
    
    for alert in alerts:
        # Group by receiver and alertname
        receiver_name = alert["receivers"][0]["name"]
        alertname = alert["labels"]["alertname"]
        group_key = f"{receiver_name}_{alertname}"
        
        if group_key not in groups:
            # Create group labels (common labels across all alerts in the group)
            group_labels = {
                "alertname": alertname,
                "job": alert["labels"]["job"]
            }
            
            groups[group_key] = {
                "labels": group_labels,
                "receiver": alert["receivers"][0],
                "alerts": []
            }
        
        groups[group_key]["alerts"].append(alert)
    
    alert_groups = list(groups.values())

def alert_generator():
    """Background thread to generate random alerts and resolve them periodically"""
    last_resolution_time = datetime.now(UTC)
    
    while True:
        current_time = datetime.now(UTC)
        
        # Generate new alerts randomly
        if random.random() < 0.4:  # 40% chance every 15 seconds
            new_alert = generate_random_alert()
            alerts.append(new_alert)
            
            # Keep only last 100 alerts
            if len(alerts) > 100:
                alerts.pop(0)
            
            create_alert_groups()
        
        # Update existing alerts randomly
        for alert in alerts:
            if random.random() < 0.1:  # 10% chance to update
                alert["updatedAt"] = datetime.now(UTC).isoformat() + "Z"
                if alert["status"]["state"] == "unprocessed":
                    alert["status"]["state"] = "active"
        
        # Resolve alerts every 30 seconds (for testing resolved alerts feature)
        if (current_time - last_resolution_time).total_seconds() >= 30:
            print(f"[{current_time.strftime('%H:%M:%S')}] Resolving alerts for testing...", flush=True)
            
            # Resolve 30-50% of active alerts
            active_alerts = [alert for alert in alerts if alert["status"]["state"] == "active"]
            if active_alerts:
                resolve_count = max(1, int(len(active_alerts) * random.uniform(0.3, 0.5)))
                alerts_to_resolve = random.sample(active_alerts, min(resolve_count, len(active_alerts)))
                
                for alert in alerts_to_resolve:
                    alerts.remove(alert)
                    print(f"  Resolved: {alert['labels']['alertname']} on {alert['labels']['instance']}", flush=True)
                
                create_alert_groups()
                print(f"  Total resolved: {len(alerts_to_resolve)} alerts", flush=True)
            else:
                print("  No active alerts to resolve", flush=True)
            
            last_resolution_time = current_time
        
        # Remove some old alerts randomly (less frequently now)
        if alerts and random.random() < 0.05:  # 5% chance to remove
            alerts.pop(0)
            create_alert_groups()
        
        time.sleep(15)

def filter_alerts_by_params(alert_list, args):
    """Filter alerts based on query parameters"""
    filtered = []
    
    active = args.get('active', 'true').lower() == 'true'
    silenced = args.get('silenced', 'true').lower() == 'true'
    inhibited = args.get('inhibited', 'true').lower() == 'true'
    unprocessed = args.get('unprocessed', 'true').lower() == 'true'
    
    for alert in alert_list:
        state = alert["status"]["state"]
        
        if state == "active" and not active:
            continue
        if state == "suppressed" and not silenced:
            continue
        if state == "unprocessed" and not unprocessed:
            continue
        if alert["status"]["inhibitedBy"] and not inhibited:
            continue
            
        filtered.append(alert)
    
    return filtered

# API v2 Endpoints (OpenAPI compliant)

@app.route('/api/v2/status', methods=['GET'])
def get_status():
    """Get current status of Alertmanager instance and its cluster"""
    return jsonify({
        "cluster": {
            "name": "fake-alertmanager-cluster",
            "status": "ready",
            "peers": [
                {
                    "name": "fake-alertmanager-1",
                    "address": "127.0.0.1:9093"
                }
            ]
        },
        "versionInfo": {
            "version": "0.26.0-fake",
            "revision": "fake-revision-12345",
            "branch": "main",
            "buildUser": "fake-user@example.com",
            "buildDate": "2024-01-01T00:00:00Z",
            "goVersion": "go1.21.0"
        },
        "config": {
            "original": "global:\n  smtp_smarthost: 'localhost:587'\nroute:\n  group_by: ['alertname']\n  receiver: 'web.hook'\nreceivers:\n- name: 'web.hook'\n  webhook_configs:\n  - url: 'http://localhost:5001/'"
        },
        "uptime": datetime.now(UTC).isoformat() + "Z"
    })

@app.route('/api/v2/receivers', methods=['GET'])
def get_receivers():
    """Get list of all receivers"""
    return jsonify([{"name": name} for name in RECEIVER_NAMES])

@app.route('/api/v2/alerts', methods=['GET'])
def get_alerts():
    """Get a list of alerts with filtering support"""
    filtered_alerts = filter_alerts_by_params(alerts, request.args)
    
    # Apply additional filters
    filters = request.args.getlist('filter')
    receiver_filter = request.args.get('receiver')
    
    if receiver_filter:
        filtered_alerts = [
            alert for alert in filtered_alerts
            if any(receiver_filter in recv["name"] for recv in alert["receivers"])
        ]
    
    # Apply label filters (simplified implementation)
    for filter_str in filters:
        if '=' in filter_str:
            key, value = filter_str.split('=', 1)
            filtered_alerts = [
                alert for alert in filtered_alerts
                if alert["labels"].get(key) == value
            ]
    
    return jsonify(filtered_alerts)

@app.route('/api/v2/alerts', methods=['POST'])
def post_alerts():
    """Create new alerts"""
    try:
        new_alerts = request.get_json()
        
        if not isinstance(new_alerts, list):
            return jsonify("Request body must be a list of alerts"), 400
        
        for alert_data in new_alerts:
            # Validate required fields
            if not alert_data.get('labels'):
                return jsonify("Alert must have labels"), 400
            
            # Create alert with defaults
            alert = {
                "labels": alert_data["labels"],
                "annotations": alert_data.get("annotations", {}),
                "startsAt": alert_data.get("startsAt", datetime.now(UTC).isoformat() + "Z"),
                "endsAt": alert_data.get("endsAt", (datetime.now(UTC) + timedelta(hours=1)).isoformat() + "Z"),
                "updatedAt": datetime.now(UTC).isoformat() + "Z",
                "generatorURL": alert_data.get("generatorURL", ""),
                "fingerprint": generate_fingerprint(),
                "receivers": [{"name": random.choice(RECEIVER_NAMES)}],
                "status": {
                    "state": "active",
                    "silencedBy": [],
                    "inhibitedBy": [],
                    "mutedBy": []
                }
            }
            
            alerts.append(alert)
        
        create_alert_groups()
        return jsonify({"message": "Alerts created successfully"}), 200
        
    except Exception as e:
        return jsonify(f"Error creating alerts: {str(e)}"), 500

@app.route('/api/v2/alerts/groups', methods=['GET'])
def get_alert_groups():
    """Get a list of alert groups"""
    # Apply the same filtering as individual alerts
    filtered_groups = []
    
    for group in alert_groups:
        filtered_alerts = filter_alerts_by_params(group["alerts"], request.args)
        
        # Apply receiver filter
        receiver_filter = request.args.get('receiver')
        if receiver_filter and receiver_filter not in group["receiver"]["name"]:
            continue
        
        # Apply label filters
        filters = request.args.getlist('filter')
        for filter_str in filters:
            if '=' in filter_str:
                key, value = filter_str.split('=', 1)
                filtered_alerts = [
                    alert for alert in filtered_alerts
                    if alert["labels"].get(key) == value
                ]
        
        if filtered_alerts:
            filtered_group = group.copy()
            filtered_group["alerts"] = filtered_alerts
            filtered_groups.append(filtered_group)
    
    return jsonify(filtered_groups)

@app.route('/api/v2/silences', methods=['GET'])
def get_silences():
    """Get a list of silences"""
    # Apply filter parameter
    filters = request.args.getlist('filter')
    filtered_silences = silences.copy()
    
    # Simple filter implementation
    for filter_str in filters:
        if '=' in filter_str:
            key, value = filter_str.split('=', 1)
            filtered_silences = [
                silence for silence in filtered_silences
                if any(matcher["name"] == key and matcher["value"] == value 
                      for matcher in silence["matchers"])
            ]
    
    return jsonify(filtered_silences)

@app.route('/api/v2/silences', methods=['POST'])
def post_silences():
    """Create a new silence"""
    try:
        silence_data = request.get_json()
        
        # Validate required fields
        required_fields = ['matchers', 'startsAt', 'endsAt', 'createdBy', 'comment']
        for field in required_fields:
            if field not in silence_data:
                return jsonify(f"Missing required field: {field}"), 400
        
        # Validate matchers
        if not isinstance(silence_data['matchers'], list) or len(silence_data['matchers']) == 0:
            return jsonify("Matchers must be a non-empty list"), 400
        
        for matcher in silence_data['matchers']:
            if not all(key in matcher for key in ['name', 'value', 'isRegex']):
                return jsonify("Each matcher must have name, value, and isRegex fields"), 400
        
        silence_id = silence_data.get('id', str(uuid.uuid4()))
        
        silence = {
            "id": silence_id,
            "matchers": silence_data["matchers"],
            "startsAt": silence_data["startsAt"],
            "endsAt": silence_data["endsAt"],
            "createdBy": silence_data["createdBy"],
            "comment": silence_data["comment"],
            "status": {
                "state": "active"
            },
            "updatedAt": datetime.now(UTC).isoformat() + "Z"
        }
        
        # Check if updating existing silence
        existing_silence = next((s for s in silences if s["id"] == silence_id), None)
        if existing_silence:
            silences.remove(existing_silence)
        
        silences.append(silence)
        
        return jsonify({"silenceID": silence_id}), 200
        
    except Exception as e:
        return jsonify(f"Error creating silence: {str(e)}"), 400

@app.route('/api/v2/silence/<silence_id>', methods=['GET'])
def get_silence(silence_id):
    """Get a silence by its ID"""
    silence = next((s for s in silences if s["id"] == silence_id), None)
    if not silence:
        return jsonify("Silence not found"), 404
    
    return jsonify(silence)

@app.route('/api/v2/silence/<silence_id>', methods=['DELETE'])
def delete_silence(silence_id):
    """Delete a silence by its ID"""
    silence = next((s for s in silences if s["id"] == silence_id), None)
    if not silence:
        return jsonify("Silence not found"), 404
    
    silences.remove(silence)
    return jsonify({"message": "Silence deleted successfully"}), 200

# Health check endpoints
@app.route('/-/healthy', methods=['GET'])
def health_check():
    """Health check endpoint"""
    return jsonify({"status": "healthy"})

@app.route('/-/ready', methods=['GET'])
def ready_check():
    """Ready check endpoint"""
    return jsonify({"status": "ready"})

# Legacy v1 API endpoints for backward compatibility
@app.route('/api/v1/alerts', methods=['GET'])
def get_alerts_v1():
    """Legacy v1 alerts endpoint"""
    return get_alerts()

@app.route('/api/v1/alerts/groups', methods=['GET'])
def get_alert_groups_v1():
    """Legacy v1 alert groups endpoint"""
    return get_alert_groups()

@app.route('/api/v1/silences', methods=['GET'])
def get_silences_v1():
    """Legacy v1 silences endpoint"""
    return get_silences()

@app.route('/api/v1/silences', methods=['POST'])
def post_silences_v1():
    """Legacy v1 create silence endpoint"""
    return post_silences()

@app.route('/api/v1/status', methods=['GET'])
def get_status_v1():
    """Legacy v1 status endpoint"""
    return get_status()

@app.route('/api/v1/receivers', methods=['GET'])
def get_receivers_v1():
    """Legacy v1 receivers endpoint"""
    return get_receivers()

if __name__ == '__main__':
    # Generate some initial alerts
    for _ in range(10):
        alerts.append(generate_random_alert())
    
    # Generate some initial silences
    for _ in range(3):
        silence = {
            "id": str(uuid.uuid4()),
            "matchers": [
                {
                    "name": "alertname",
                    "value": random.choice(["HighCPUUsage", "DiskSpaceLow"]),
                    "isRegex": False,
                    "isEqual": True
                }
            ],
            "startsAt": datetime.now(UTC).isoformat() + "Z",
            "endsAt": (datetime.now(UTC) + timedelta(hours=2)).isoformat() + "Z",
            "createdBy": "test-user@example.com",
            "comment": f"Test silence {_+1}",
            "status": {
                "state": "active"
            },
            "updatedAt": datetime.now(UTC).isoformat() + "Z"
        }
        silences.append(silence)
    
    create_alert_groups()
    
    # Start background alert generator
    generator_thread = Thread(target=alert_generator, daemon=True)
    generator_thread.start()
    
    print("Starting fake Alertmanager on http://localhost:9093")
    print("OpenAPI v2 compliant endpoints:")
    print("  GET  /api/v2/status")
    print("  GET  /api/v2/receivers")
    print("  GET  /api/v2/alerts")
    print("  POST /api/v2/alerts")
    print("  GET  /api/v2/alerts/groups")
    print("  GET  /api/v2/silences")
    print("  POST /api/v2/silences")
    print("  GET  /api/v2/silence/<id>")
    print("  DELETE /api/v2/silence/<id>")
    print("  GET  /-/healthy")
    print("  GET  /-/ready")
    print("\nLegacy v1 endpoints also available for backward compatibility")
    
    app.run(host='0.0.0.0', port=9093, debug=False)