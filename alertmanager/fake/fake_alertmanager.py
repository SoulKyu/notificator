#!/usr/bin/env python3

import json
import random
import time
import re
from datetime import datetime, timedelta, UTC
from flask import Flask, jsonify, request
from threading import Thread
import uuid

app = Flask(__name__)

# Global storage for alerts and silences
alerts = []
alert_groups = []
silences = []

# Sample alert templates with various severity levels
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
        "alertname": "CriticalCPUUsage",
        "severity": "critical",
        "instance": "server-{instance}.example.com",
        "job": "node-exporter",
        "team": "infrastructure",
        "description": "CPU usage is above 95% for more than 2 minutes",
        "summary": "Critical CPU usage on {instance}"
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
        "alertname": "DiskSpaceWarning",
        "severity": "warning",
        "instance": "server-{instance}.example.com",
        "job": "node-exporter",
        "team": "infrastructure",
        "description": "Disk space is below 20% on {instance}",
        "summary": "Disk space getting low on {instance}"
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
        "alertname": "ServiceSlowResponse",
        "severity": "warning",
        "instance": "service-{instance}.example.com",
        "job": "blackbox",
        "team": "platform",
        "description": "Service {instance} response time is above 2 seconds",
        "summary": "Service {instance} responding slowly"
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
    },
    {
        "alertname": "DatabaseSlowQuery",
        "severity": "warning",
        "instance": "db-{instance}.example.com",
        "job": "mysql-exporter",
        "team": "database",
        "description": "Database queries taking longer than 5 seconds on {instance}",
        "summary": "Slow database queries on {instance}"
    },
    {
        "alertname": "NetworkLatencyHigh",
        "severity": "minor",
        "instance": "network-{instance}.example.com",
        "job": "ping-exporter",
        "team": "network",
        "description": "Network latency is above 100ms on {instance}",
        "summary": "High network latency on {instance}"
    },
    {
        "alertname": "CertificateExpiringSoon",
        "severity": "info",
        "instance": "web-{instance}.example.com",
        "job": "cert-exporter",
        "team": "security",
        "description": "SSL certificate will expire in 30 days on {instance}",
        "summary": "Certificate expiring soon on {instance}"
    },
    {
        "alertname": "LogErrorRateHigh",
        "severity": "minor",
        "instance": "app-{instance}.example.com",
        "job": "log-exporter",
        "team": "application",
        "description": "Error rate in logs is above 5% on {instance}",
        "summary": "High error rate in logs on {instance}"
    },
    {
        "alertname": "BackupFailed",
        "severity": "major",
        "instance": "backup-{instance}.example.com",
        "job": "backup-exporter",
        "team": "operations",
        "description": "Daily backup failed on {instance}",
        "summary": "Backup failure on {instance}"
    },
    {
        "alertname": "LoadBalancerHealthy",
        "severity": "info",
        "instance": "lb-{instance}.example.com",
        "job": "haproxy-exporter",
        "team": "infrastructure",
        "description": "Load balancer {instance} is healthy and operational",
        "summary": "Load balancer {instance} operational"
    }
]

# Severity levels with weights for random selection (higher weight = more frequent)
SEVERITY_WEIGHTS = {
    "critical": 10,
    "major": 15,
    "warning": 30,
    "minor": 25,
    "info": 20
}

RECEIVER_NAMES = ["web.hook", "email-team", "slack-critical", "pagerduty", "discord-alerts"]

def generate_fingerprint():
    """Generate a unique fingerprint for an alert"""
    return ''.join(random.choices('0123456789abcdef', k=16))

def matches_label(matcher, label_value):
    """Check if a matcher matches a label value"""
    if label_value is None:
        label_value = ""
    
    matcher_value = matcher.get('value', '')
    is_regex = matcher.get('isRegex', False)
    is_equal = matcher.get('isEqual', True)
    
    if is_regex:
        try:
            pattern = re.compile(matcher_value)
            matches = bool(pattern.search(str(label_value)))
        except re.error:
            # Invalid regex pattern
            matches = False
    else:
        matches = str(label_value) == matcher_value
    
    # Handle isEqual flag (negation)
    if is_equal:
        return matches
    else:
        return not matches

def match_silence_to_alert(silence, alert):
    """Check if a silence applies to an alert based on matchers"""
    # Check if silence is active
    now = datetime.now(UTC)
    starts_at = datetime.fromisoformat(silence['startsAt'].replace('Z', '+00:00'))
    ends_at = datetime.fromisoformat(silence['endsAt'].replace('Z', '+00:00'))
    
    # Check if silence is within active time range
    if now < starts_at or now > ends_at:
        return False
    
    # All matchers must match for the silence to apply
    for matcher in silence.get('matchers', []):
        label_name = matcher.get('name', '')
        label_value = alert['labels'].get(label_name)
        
        if not matches_label(matcher, label_value):
            return False
    
    return True

def apply_silences_to_alerts():
    """Apply all active silences to all alerts"""
    now = datetime.now(UTC)
    
    for alert in alerts:
        # Reset silence state
        silenced_by = []
        
        # Check each silence against this alert
        for silence in silences:
            if match_silence_to_alert(silence, alert):
                silenced_by.append(silence['id'])
        
        # Update alert status based on silences
        if silenced_by:
            alert['status']['state'] = 'suppressed'
            alert['status']['silencedBy'] = silenced_by
        else:
            # Only change back to active if it was suppressed
            if alert['status']['state'] == 'suppressed':
                alert['status']['state'] = 'active'
            alert['status']['silencedBy'] = []

def remove_silence_from_alerts(silence_id):
    """Remove a specific silence from all alerts"""
    for alert in alerts:
        if silence_id in alert['status'].get('silencedBy', []):
            alert['status']['silencedBy'].remove(silence_id)
            
            # If no more silences, change state back to active
            if not alert['status']['silencedBy']:
                if alert['status']['state'] == 'suppressed':
                    alert['status']['state'] = 'active'

def check_silence_expiration():
    """Check for expired silences and update their status"""
    now = datetime.now(UTC)
    
    for silence in silences:
        ends_at = datetime.fromisoformat(silence['endsAt'].replace('Z', '+00:00'))
        
        if now > ends_at and silence['status']['state'] == 'active':
            silence['status']['state'] = 'expired'
            # Remove from alerts
            remove_silence_from_alerts(silence['id'])

def get_weighted_severity():
    """Get a random severity based on weights"""
    severities = list(SEVERITY_WEIGHTS.keys())
    weights = list(SEVERITY_WEIGHTS.values())
    return random.choices(severities, weights=weights)[0]

def generate_random_alert():
    """Generate a random alert based on templates"""
    template = random.choice(ALERT_TEMPLATES)
    instance_id = random.randint(1, 20)
    instance_name = template["instance"].format(instance=instance_id)
    
    # 20% chance to override template severity with weighted random severity
    severity = get_weighted_severity() if random.random() < 0.2 else template["severity"]
    
    starts_at = datetime.now(UTC) - timedelta(minutes=random.randint(1, 30))
    ends_at = starts_at + timedelta(hours=random.randint(1, 6))
    
    alert = {
        "labels": {
            "alertname": template["alertname"],
            "severity": severity,
            "instance": instance_name,
            "job": template["job"],
            "team": template["team"]
        },
        "annotations": {
            "description": template["description"].format(instance=instance_name),
            "summary": template["summary"].format(instance=instance_name),
            "runbook_url": f"https://runbooks.example.com/{template['alertname'].lower()}"
        },
        "startsAt": starts_at.strftime('%Y-%m-%dT%H:%M:%S.%fZ'),
        "endsAt": ends_at.strftime('%Y-%m-%dT%H:%M:%S.%fZ'),
        "updatedAt": datetime.now(UTC).strftime('%Y-%m-%dT%H:%M:%S.%fZ'),
        "generatorURL": f"http://prometheus:9090/graph?g0.expr=up{{job=\"{template['job']}\"}}&g0.tab=1",
        "fingerprint": generate_fingerprint(),
        "receivers": [{"name": random.choice(RECEIVER_NAMES)}],
        "status": {
            "state": random.choice(["active", "active", "active", "active", "unprocessed"]),  # 80% active, 20% unprocessed
            "silencedBy": [],
            "inhibitedBy": [],
            "mutedBy": []
        }
    }
    
    # Don't randomly silence alerts - let the silence system handle it
    
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
    last_silence_check = datetime.now(UTC)
    
    while True:
        current_time = datetime.now(UTC)
        
        # Generate new alerts randomly (increased frequency for more alerts)
        if random.random() < 0.6:  # 60% chance every 15 seconds
            new_alert = generate_random_alert()
            alerts.append(new_alert)
            
            # Keep only last 150 alerts (increased for more active alerts)
            if len(alerts) > 150:
                alerts.pop(0)
            
            create_alert_groups()
        
        # Update existing alerts randomly
        for alert in alerts:
            if random.random() < 0.1:  # 10% chance to update
                alert["updatedAt"] = datetime.now(UTC).strftime('%Y-%m-%dT%H:%M:%S.%fZ')
                if alert["status"]["state"] == "unprocessed":
                    alert["status"]["state"] = "active"
        
        # Check and apply silences every 5 seconds
        if (current_time - last_silence_check).total_seconds() >= 5:
            # Check for expired silences
            check_silence_expiration()
            # Apply all active silences to alerts
            apply_silences_to_alerts()
            last_silence_check = current_time
        
        # Resolve alerts every 30 seconds (for testing resolved alerts feature)
        if (current_time - last_resolution_time).total_seconds() >= 30:
            print(f"[{current_time.strftime('%H:%M:%S')}] Resolving alerts for testing...", flush=True)
            
            # Resolve 15-25% of active alerts (reduced to keep more active alerts)
            active_alerts = [alert for alert in alerts if alert["status"]["state"] == "active"]
            if active_alerts:
                resolve_count = max(1, int(len(active_alerts) * random.uniform(0.15, 0.25)))
                alerts_to_resolve = random.sample(active_alerts, min(resolve_count, len(active_alerts)))
                
                for alert in alerts_to_resolve:
                    # Mark as resolved instead of removing completely
                    alert["status"]["state"] = "resolved"
                    alert["endsAt"] = datetime.now(UTC).strftime('%Y-%m-%dT%H:%M:%S.%fZ')
                    alert["updatedAt"] = datetime.now(UTC).strftime('%Y-%m-%dT%H:%M:%S.%fZ')
                    print(f"  Resolved: {alert['labels']['alertname']} on {alert['labels']['instance']}", flush=True)
                
                create_alert_groups()
                print(f"  Total resolved: {len(alerts_to_resolve)} alerts", flush=True)
            else:
                print("  No active alerts to resolve", flush=True)
            
            last_resolution_time = current_time
        
        # Clean up old resolved alerts (keep resolved alerts for 1 hour, then remove)
        if alerts and random.random() < 0.1:  # 10% chance to check for cleanup
            cleanup_count = 0
            one_hour_ago = current_time - timedelta(hours=1)
            
            alerts_to_remove = []
            for alert in alerts:
                if alert["status"]["state"] == "resolved":
                    ends_at = datetime.fromisoformat(alert["endsAt"].replace('Z', '+00:00'))
                    if ends_at < one_hour_ago:
                        alerts_to_remove.append(alert)
            
            for alert in alerts_to_remove:
                alerts.remove(alert)
                cleanup_count += 1
            
            if cleanup_count > 0:
                print(f"  Cleaned up {cleanup_count} old resolved alerts", flush=True)
                create_alert_groups()
        
        # Remove some old alerts randomly (less frequently now)
        if alerts and random.random() < 0.02:  # 2% chance to remove (reduced from 5%)
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
        "uptime": datetime.now(UTC).strftime('%Y-%m-%dT%H:%M:%S.%fZ')
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
                "startsAt": alert_data.get("startsAt", datetime.now(UTC).strftime('%Y-%m-%dT%H:%M:%S.%fZ')),
                "endsAt": alert_data.get("endsAt", (datetime.now(UTC) + timedelta(hours=1)).strftime('%Y-%m-%dT%H:%M:%S.%fZ')),
                "updatedAt": datetime.now(UTC).strftime('%Y-%m-%dT%H:%M:%S.%fZ'),
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
            "updatedAt": datetime.now(UTC).strftime('%Y-%m-%dT%H:%M:%S.%fZ')
        }
        
        # Check if updating existing silence
        existing_silence = next((s for s in silences if s["id"] == silence_id), None)
        if existing_silence:
            silences.remove(existing_silence)
        
        silences.append(silence)
        
        # Apply the new silence to existing alerts immediately
        apply_silences_to_alerts()
        
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
    
    # Remove the silence from any alerts before deleting
    remove_silence_from_alerts(silence_id)
    
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
    # Generate some initial alerts (increased for more active alerts)
    for _ in range(20):
        alerts.append(generate_random_alert())
    
    # Generate some initial silences (reduced for fewer silenced alerts)
    for _ in range(1):
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
            "startsAt": datetime.now(UTC).strftime('%Y-%m-%dT%H:%M:%S.%fZ'),
            "endsAt": (datetime.now(UTC) + timedelta(hours=2)).strftime('%Y-%m-%dT%H:%M:%S.%fZ'),
            "createdBy": "test-user@example.com",
            "comment": f"Test silence {_+1}",
            "status": {
                "state": "active"
            },
            "updatedAt": datetime.now(UTC).strftime('%Y-%m-%dT%H:%M:%S.%fZ')
        }
        silences.append(silence)
    
    create_alert_groups()
    
    # Apply any initial silences to the initial alerts
    apply_silences_to_alerts()
    
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