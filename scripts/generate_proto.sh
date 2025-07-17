#!/bin/bash
# scripts/generate_proto.sh

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}🔄 Generating Go code from .proto files...${NC}"

# Clean up any existing generated files
echo -e "${YELLOW}🧹 Cleaning up existing generated files...${NC}"
rm -rf internal/backend/proto/auth
rm -rf internal/backend/proto/alert
rm -f internal/backend/proto/*.pb.go

# Create output directories
mkdir -p internal/backend/proto/auth
mkdir -p internal/backend/proto/alert

# Generate auth service
echo -e "${YELLOW}📝 Generating auth service...${NC}"
protoc \
  --go_out=. \
  --go_opt=module=notificator \
  --go-grpc_out=. \
  --go-grpc_opt=module=notificator \
  proto/auth.proto

# Generate alert service  
echo -e "${YELLOW}📝 Generating alert service...${NC}"
protoc \
  --go_out=. \
  --go_opt=module=notificator \
  --go-grpc_out=. \
  --go-grpc_opt=module=notificator \
  proto/alert.proto

echo -e "${GREEN}✅ Proto generation completed!${NC}"
echo -e "${GREEN}Generated files:${NC}"
echo -e "  - internal/backend/proto/auth/auth.pb.go"
echo -e "  - internal/backend/proto/auth/auth_grpc.pb.go"
echo -e "  - internal/backend/proto/alert/alert.pb.go"
echo -e "  - internal/backend/proto/alert/alert_grpc.pb.go"