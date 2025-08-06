#!/bin/bash
# Development runner for Notificator Electron app

echo "Starting Notificator Desktop Client..."
echo "Connecting to: ${NOTIFICATOR_URL:-http://localhost:8081}"
echo ""
echo "Note: If running on Linux with sandboxing issues, try:"
echo "  npm start --no-sandbox"
echo ""

# Set development environment
export ELECTRON_ENABLE_LOGGING=1
export NODE_ENV=development

# Start the app
npm start