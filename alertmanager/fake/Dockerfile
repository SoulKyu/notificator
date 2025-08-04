# Dockerfile for Fake Alertmanager
FROM python:3.13-slim

WORKDIR /app

# Install system dependencies if needed
RUN apt-get update && apt-get install -y \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Copy dependency files
COPY pyproject.toml uv.lock* ./

# Install uv package manager
RUN pip install --no-cache-dir uv

# Install Python dependencies
RUN uv venv && \
    uv sync --no-dev

# Copy application files
COPY fake_alertmanager.py ./
COPY README.md ./

# Expose the Alertmanager port
EXPOSE 9093

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:9093/-/healthy || exit 1

# Run the fake alertmanager
CMD ["uv", "run", "fake_alertmanager.py"]