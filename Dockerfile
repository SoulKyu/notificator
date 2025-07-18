# Build stage
FROM golang:1.23-alpine AS build

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the code
COPY . ./

# Build the backend binary from the backend-specific entry point
# This avoids GUI dependencies
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /notificator-backend ./cmd/backend

# Final stage - distroless for security
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /

# Copy the binary from build stage
COPY --from=build /notificator-backend /notificator-backend

# Run as non-root user
USER 65534

# Expose backend ports
# 50051 - gRPC API
# 8080  - HTTP health/metrics
EXPOSE 50051 8080

ENTRYPOINT ["/notificator-backend"]