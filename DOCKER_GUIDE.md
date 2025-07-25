# 🐳 Docker Deployment Guide

This guide shows how to run the complete Notificator stack using Docker Compose.

## 🚀 Quick Start

**Start all services:**
```bash
docker-compose up -d
```

**Stop all services:**
```bash
docker-compose down
```

**View logs:**
```bash
docker-compose logs -f
```

## 📋 Services Overview

| Service | Image | Port | Description |
|---------|-------|------|-------------|
| **alertmanager** | `registry-1.docker.io/soulkyu/notificator-backend:alertmanager` | 9093 | Fake Alertmanager for testing |
| **backend** | `registry-1.docker.io/soulkyu/notificator-backend:backend` | 50051 (gRPC), 8080 (HTTP) | Notificator backend server |
| **webui** | `registry-1.docker.io/soulkyu/notificator-backend:webui` | 8081 | Web dashboard |

## 🌐 Access URLs

- **WebUI Dashboard**: http://localhost:8081
- **Fake Alertmanager**: http://localhost:9093
- **Backend HTTP API**: http://localhost:8080
- **Backend gRPC**: localhost:50051

## 🔧 Configuration

### Environment Variables

The services are configured via environment variables. Key configurations:

**Backend:**
```yaml
environment:
  - NOTIFICATOR_BACKEND_DATABASE_TYPE=sqlite
  - NOTIFICATOR_ALERTMANAGERS_0_URL=http://alertmanager:9093
  - NOTIFICATOR_LOG_LEVEL=info
```

**WebUI:**
```yaml
environment:
  - BACKEND_ADDRESS=backend:50051
  - NOTIFICATOR_LOG_LEVEL=info
```

### Custom Configuration

Create a `.env` file in the same directory as `docker-compose.yml`:

```bash
# .env file
NOTIFICATOR_LOG_LEVEL=debug
NOTIFICATOR_BACKEND_DATABASE_TYPE=sqlite
ALERTMANAGER_URL=http://alertmanager:9093
```

## 🗄️ Data Persistence

- **Backend data**: Stored in named volume `backend-data`
- **Database**: SQLite file persisted in `/data/notificator.db`

**View volume:**
```bash
docker volume ls
docker volume inspect notificator_backend-data
```

**Backup data:**
```bash
docker run --rm -v notificator_backend-data:/data -v $(pwd):/backup alpine tar czf /backup/notificator-backup.tar.gz -C /data .
```

**Restore data:**
```bash
docker run --rm -v notificator_backend-data:/data -v $(pwd):/backup alpine tar xzf /backup/notificator-backup.tar.gz -C /data
```

## 🏥 Health Checks

All services include health checks:

**Check service health:**
```bash
docker-compose ps
```

**Manual health checks:**
```bash
# Alertmanager
curl http://localhost:9093/-/healthy

# Backend
curl http://localhost:8080/health

# WebUI  
curl http://localhost:8081/health
```

## 🔍 Debugging

**View service logs:**
```bash
# All services
docker-compose logs

# Specific service
docker-compose logs backend
docker-compose logs webui
docker-compose logs alertmanager

# Follow logs
docker-compose logs -f backend
```

**Execute commands in containers:**
```bash
# Backend container
docker-compose exec backend /bin/sh

# Check backend binary help
docker-compose exec backend ./notificator-backend --help
```

**Restart specific service:**
```bash
docker-compose restart backend
```

## 🔧 Development Overrides

For development, use the provided override file:

```bash
# Rename override file
mv docker-compose.override.yml.example docker-compose.override.yml

# Start with overrides
docker-compose up -d
```

Override features:
- Debug logging
- PostgreSQL database option
- Development-specific configurations

## 🐘 PostgreSQL Setup

To use PostgreSQL instead of SQLite:

1. Uncomment PostgreSQL service in `docker-compose.override.yml`
2. Update backend environment variables:
   ```yaml
   environment:
     - NOTIFICATOR_BACKEND_DATABASE_TYPE=postgres
     - DB_HOST=postgres
     - DB_NAME=notificator
     - DB_USER=notificator
     - DB_PASSWORD=secretpassword
   ```

## 🔄 Updates

**Pull latest images:**
```bash
docker-compose pull
```

**Restart with new images:**
```bash
docker-compose down
docker-compose pull
docker-compose up -d
```

## 🧹 Cleanup

**Remove containers and networks:**
```bash
docker-compose down
```

**Remove containers, networks, and volumes:**
```bash
docker-compose down -v
```

**Remove everything including images:**
```bash
docker-compose down -v --rmi all
```

## 📊 Monitoring

**Resource usage:**
```bash
docker stats notificator-backend notificator-webui notificator-alertmanager
```

**Container information:**
```bash
docker-compose ps -a
docker-compose top
```

## 🚨 Troubleshooting

**Service won't start:**
1. Check logs: `docker-compose logs [service-name]`
2. Verify health checks: `docker-compose ps`
3. Check port conflicts: `netstat -tulpn | grep [port]`

**Can't connect between services:**
1. Verify network: `docker network ls`
2. Check service names in environment variables
3. Ensure services are in the same network

**Database issues:**
1. Check volume mount: `docker volume inspect notificator_backend-data`
2. Verify database type environment variable
3. Check database connectivity from backend logs

**WebUI can't reach backend:**
1. Verify `BACKEND_ADDRESS` environment variable
2. Check backend service is healthy
3. Ensure network connectivity between containers