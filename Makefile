.PHONY: help clean webui-setup webui-build webui-dev webui-css webui-css-prod webui-css-build webui-templates webui-full-rebuild go-build go-run-backend go-run-webui go-run-desktop run-all docker-build-webui docker-build-backend docker-build-alertmanager docker-build-all docker-push-webui docker-push-backend docker-push-alertmanager docker-push-all docker test

# Default target
help: ## Show this help message
	@echo "Notificator Build System"
	@echo "========================"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# Variables
WEBUI_CSS_INPUT = ./internal/webui/static/css/input.css
WEBUI_CSS_OUTPUT = ./internal/webui/static/css/output.css
GO_MAIN_CMD = .
DOCKER_REGISTRY = registry-1.docker.io/soulkyu
WEBUI_IMAGE = $(DOCKER_REGISTRY)/notificator-webui:latest
BACKEND_IMAGE = $(DOCKER_REGISTRY)/notificator-backend:latest
ALERTMANAGER_IMAGE = $(DOCKER_REGISTRY)/notificator-alertmanager:latest

# WebUI Setup and Dependencies
webui-setup: ## Install WebUI dependencies (npm install)
	@echo "Installing WebUI dependencies..."
	npm install

# WebUI CSS Building
webui-css: ## Build WebUI CSS (development mode)
	@echo "Building WebUI CSS (development)..."
	npx tailwindcss -i $(WEBUI_CSS_INPUT) -o $(WEBUI_CSS_OUTPUT)

webui-css-prod: ## Build WebUI CSS (production mode - minified)
	@echo "Building WebUI CSS (production)..."
	npx tailwindcss -i $(WEBUI_CSS_INPUT) -o $(WEBUI_CSS_OUTPUT) --minify

webui-css-watch: ## Watch and rebuild WebUI CSS on changes
	@echo "Watching WebUI CSS for changes..."
	npx tailwindcss -i $(WEBUI_CSS_INPUT) -o $(WEBUI_CSS_OUTPUT) --watch

webui-css-build: ## Build WebUI CSS using explicit build command
	@echo "Building WebUI CSS with explicit build command..."
	npx tailwindcss build -i $(WEBUI_CSS_INPUT) -o $(WEBUI_CSS_OUTPUT) --minify

# Template Generation
webui-templates: ## Generate Go templates from .templ files
	@echo "Generating WebUI templates..."
	templ generate

# Proto Generation
proto: ## Generate Go code from .proto files
	@echo "Generating proto files..."
	./scripts/generate_proto.sh

webui-templates-watch: ## Watch and regenerate templates on changes
	@echo "Watching templates for changes..."
	templ generate --watch

# WebUI Build Targets
webui-build: webui-setup webui-css-prod webui-templates ## Build WebUI (production ready)
	@echo "WebUI build completed!"

webui-dev: ## Start WebUI development mode (watch CSS and templates)
	@echo "Starting WebUI development mode..."
	npm run dev

webui-full-rebuild: clean webui-setup webui-css-build webui-templates ## Clean and rebuild WebUI completely
	@echo "Full WebUI rebuild completed!"

# Go Build Targets
go-build: ## Build Go binaries
	@echo "Building Go binaries..."
	@echo "Building backend..."
	go build -o bin/backend $(GO_MAIN_CMD)
	@echo "Building webui..."
	go build -o bin/webui $(GO_MAIN_CMD)
	@echo "Building desktop..."
	go build -o bin/notificator $(GO_MAIN_CMD)

go-build-backend: ## Build only the backend binary
	@echo "Building backend binary..."
	go build -o bin/backend $(GO_MAIN_CMD)

go-build-webui: ## Build only the webui binary
	@echo "Building webui binary..."
	go build -o bin/webui $(GO_MAIN_CMD)

go-build-desktop: ## Build only the desktop binary
	@echo "Building desktop binary..."
	go build -o bin/notificator $(GO_MAIN_CMD)

# Go Run Targets
go-run-backend: ## Run the backend server
	@echo "Starting backend server..."
	go run $(GO_MAIN_CMD) backend

go-run-webui: ## Run the webui server
	@echo "Starting webui server..."
	go run $(GO_MAIN_CMD) webui

go-run-desktop: ## Run the desktop application
	@echo "Starting desktop application..."
	go run $(GO_MAIN_CMD) desktop

# Full Build
build-all: webui-build go-build ## Build everything (WebUI + Go binaries)
	@echo "Full build completed!"

# Development
dev-backend: ## Start backend in development mode
	@echo "Starting backend in development mode..."
	go run $(GO_MAIN_CMD) backend

dev-webui: webui-templates ## Start webui in development mode (assumes CSS is built)
	@echo "Starting webui in development mode..."
	go run $(GO_MAIN_CMD) webui

dev-desktop: ## Start desktop in development mode
	@echo "Starting desktop in development mode..."
	go run $(GO_MAIN_CMD) desktop

run-all: ## Run both backend and webui servers concurrently
	@echo "Starting both backend and webui servers..."
	@echo "Backend will run on :50051, WebUI on :8081"
	@echo "Press Ctrl+C to stop both servers"
	@(trap 'kill 0' SIGINT; \
		go run $(GO_MAIN_CMD) backend & \
		sleep 2 && go run $(GO_MAIN_CMD) webui & \
		wait)

# Cleaning
clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	rm -rf node_modules/
	rm -f $(WEBUI_CSS_OUTPUT)
	find . -name "*_templ.go" -delete

clean-css: ## Clean only CSS build artifacts
	@echo "Cleaning CSS artifacts..."
	rm -f $(WEBUI_CSS_OUTPUT)

clean-templates: ## Clean only generated template files
	@echo "Cleaning generated templates..."
	find . -name "*_templ.go" -delete

# Quick rebuild commands
quick-css: webui-css webui-templates ## Quick CSS rebuild and template generation
	@echo "Quick CSS rebuild completed!"

quick-build: webui-css-prod webui-templates go-build-webui ## Quick production build for webui
	@echo "Quick webui build completed!"

# Development workflow shortcuts
fix-webui: webui-full-rebuild dev-webui ## Fix WebUI issues by full rebuild and restart
	@echo "WebUI fix workflow completed!"

# Status check
status: ## Check build status and dependencies
	@echo "Checking build status..."
	@echo "Node.js version:"
	@node --version 2>/dev/null || echo "Node.js not found"
	@echo "npm version:"
	@npm --version 2>/dev/null || echo "npm not found"
	@echo "Go version:"
	@go version 2>/dev/null || echo "Go not found"
	@echo "templ version:"
	@templ version 2>/dev/null || echo "templ not found"
	@echo "Tailwind CSS:"
	@npx tailwindcss --help > /dev/null 2>&1 && echo "Available" || echo "Not available"
	@echo ""
	@echo "Files status:"
	@echo "CSS Input: $(WEBUI_CSS_INPUT)" $(if $(shell test -f $(WEBUI_CSS_INPUT) && echo "exists"), "✓", "✗")
	@echo "CSS Output: $(WEBUI_CSS_OUTPUT)" $(if $(shell test -f $(WEBUI_CSS_OUTPUT) && echo "exists"), "✓", "✗")
	@echo "node_modules:" $(if $(shell test -d node_modules && echo "exists"), "✓", "✗")

# Docker build commands
docker-build-webui: ## Build WebUI Docker image
	@echo "Building WebUI Docker image..."
	docker build -t $(WEBUI_IMAGE) -f Dockerfile.webui .

docker-build-backend: ## Build Backend Docker image
	@echo "Building Backend Docker image..."
	docker build -t $(BACKEND_IMAGE) -f Dockerfile.backend .

docker-build-alertmanager: ## Build Alertmanager Docker image
	@echo "Building Alertmanager Docker image..."
	cd alertmanager/fake && docker build -t $(ALERTMANAGER_IMAGE) -f Dockerfile .

docker-build-all: docker-build-webui docker-build-backend docker-build-alertmanager ## Build all Docker images
	@echo "All Docker images built successfully!"

# Docker push commands
docker-push-webui: ## Push WebUI Docker image to registry
	@echo "Pushing WebUI Docker image..."
	docker push $(WEBUI_IMAGE)

docker-push-backend: ## Push Backend Docker image to registry
	@echo "Pushing Backend Docker image..."
	docker push $(BACKEND_IMAGE)

docker-push-alertmanager: ## Push Alertmanager Docker image to registry
	@echo "Pushing Alertmanager Docker image..."
	docker push $(ALERTMANAGER_IMAGE)

docker-push-all: docker-push-webui docker-push-backend docker-push-alertmanager ## Push all Docker images to registry
	@echo "All Docker images pushed successfully!"

# Combined build and push
docker: docker-build-all docker-push-all ## Build and push all Docker images
	@echo "All Docker images built and pushed successfully!"

# Test command - build all images and restart docker-compose
test: webui-full-rebuild docker-build-all ## Build all Docker images and restart docker-compose
	@echo "Stopping docker-compose services..."
	docker-compose down
	@echo "Starting docker-compose services..."
	docker-compose up -d
	@echo "Test environment is ready!"