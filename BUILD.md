# ByteFreezer Proxy - Build Documentation

This document describes how to build, test, and deploy the ByteFreezer Proxy project from source.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Local Development Build](#local-development-build)
- [GitHub Actions CI/CD](#github-actions-cicd)
- [Building for Multiple Architectures](#building-for-multiple-architectures)
- [Docker Build](#docker-build)
- [Release Process](#release-process)
- [Deployment](#deployment)

## Prerequisites

### Development Environment

- **Go**: Version 1.21 or later
- **Git**: For version control
- **Make**: For build automation (optional)
- **Docker**: For containerized builds (optional)

### System Dependencies

```bash
# Ubuntu/Debian
sudo apt update
sudo apt install -y git golang-go make curl wget

# RHEL/CentOS/Fedora  
sudo yum install -y git golang make curl wget
# or
sudo dnf install -y git golang make curl wget

# macOS (with Homebrew)
brew install go git make curl wget
```

## Local Development Build

### 1. Clone the Repository

```bash
git clone https://github.com/n0needt0/bytefreezer-proxy.git
cd bytefreezer-proxy
```

### 2. Initialize Go Module

```bash
go mod download
go mod tidy
```

### 3. Build for Current Platform

```bash
# Simple build
go build -o bytefreezer-proxy ./cmd/main.go

# Build with version info
VERSION=$(git describe --tags --always --dirty)
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
go build -ldflags "-X main.version=$VERSION -X main.buildTime=$BUILD_TIME" -o bytefreezer-proxy ./cmd/main.go
```

### 4. Run Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# Run tests with verbose output
go test -v ./...
```

### 5. Run Locally

```bash
# Run with default config
./bytefreezer-proxy

# Run with custom config
./bytefreezer-proxy --config /path/to/config.yaml

# Validate config only
./bytefreezer-proxy --validate-config
```

## GitHub Actions CI/CD

The project uses GitHub Actions for automated building, testing, and releasing.

### Workflow Files

- **`.github/workflows/ci.yml`**: Continuous Integration with comprehensive testing
- **`.github/workflows/build-and-release.yml`**: Release builds and Docker images
- **`.github/workflows/release.yml`**: Additional release automation

### CI Pipeline Features

#### Continuous Integration (ci.yml)
1. **Code Quality**: Go fmt, vet, staticcheck, gosec security scanning
2. **Multi-version Testing**: Tests against Go 1.21, 1.22, 1.23
3. **Coverage Reporting**: Upload to Codecov with race condition detection
4. **Integration Testing**: Full service testing with mock backends
5. **Vulnerability Scanning**: Nancy and govulncheck for dependency security
6. **Cross-platform Validation**: Build verification on Ubuntu, macOS, Windows
7. **Ansible Validation**: Playbook syntax and linting checks
8. **Documentation Checks**: Verify all required docs are present

#### Build and Release (build-and-release.yml)
1. **Multi-platform builds**: Linux (amd64, arm64)
2. **Automated testing**: Unit tests with coverage reporting  
3. **Docker images**: Multi-arch container builds with GHCR publishing
4. **Release automation**: GitHub releases with binary artifacts
5. **Container Security**: Distroless images with security scanning

### Setting Up GitHub Repository

#### 1. Repository Secrets

Configure these secrets in your GitHub repository (`Settings > Secrets and variables > Actions`):

```bash
# Docker Hub (optional, for pushing images)
DOCKER_HUB_USERNAME=your-dockerhub-username
DOCKER_HUB_TOKEN=your-dockerhub-token

# GitHub Token (automatically available)
GITHUB_TOKEN=<automatically-provided>
```

#### 2. Repository Settings

- Enable GitHub Actions in repository settings
- Set branch protection rules for `main` branch
- Configure required status checks before merging

### Triggering Builds

#### Continuous Integration
```bash
# Triggers on every push and PR
git push origin feature-branch
```

#### Release Build
```bash
# Create and push a tag to trigger release
git tag v1.0.0
git push origin v1.0.0
```

## Building for Multiple Architectures

### Cross-Compilation with Go

```bash
# Linux AMD64
GOOS=linux GOARCH=amd64 go build -o bytefreezer-proxy-linux-amd64 ./cmd/main.go

# Linux ARM64
GOOS=linux GOARCH=arm64 go build -o bytefreezer-proxy-linux-arm64 ./cmd/main.go

# Linux ARM (32-bit)
GOOS=linux GOARCH=arm go build -o bytefreezer-proxy-linux-arm ./cmd/main.go

# Windows AMD64
GOOS=windows GOARCH=amd64 go build -o bytefreezer-proxy-windows-amd64.exe ./cmd/main.go

# macOS AMD64
GOOS=darwin GOARCH=amd64 go build -o bytefreezer-proxy-darwin-amd64 ./cmd/main.go

# macOS ARM64 (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o bytefreezer-proxy-darwin-arm64 ./cmd/main.go
```

### Build Script for All Platforms

Create a `build.sh` script:

```bash
#!/bin/bash
set -e

VERSION=${VERSION:-$(git describe --tags --always --dirty)}
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS="-X main.version=$VERSION -X main.buildTime=$BUILD_TIME"

# Define platforms
platforms=(
    "linux/amd64"
    "linux/arm64" 
    "linux/arm"
    "windows/amd64"
    "darwin/amd64"
    "darwin/arm64"
)

for platform in "${platforms[@]}"; do
    IFS='/' read -r -a array <<< "$platform"
    GOOS=${array[0]}
    GOARCH=${array[1]}
    
    output_name="bytefreezer-proxy-$GOOS-$GOARCH"
    if [ $GOOS = "windows" ]; then
        output_name+='.exe'
    fi
    
    echo "Building for $GOOS/$GOARCH..."
    env GOOS=$GOOS GOARCH=$GOARCH go build -ldflags "$LDFLAGS" -o "dist/$output_name" ./cmd/main.go
    
    # Create archive
    if [ $GOOS = "windows" ]; then
        zip "dist/$output_name.zip" "dist/$output_name"
    else
        tar -czf "dist/$output_name.tar.gz" -C dist "$output_name"
    fi
done

echo "Build complete. Artifacts in dist/ directory."
```

Make it executable and run:
```bash
chmod +x build.sh
mkdir -p dist
./build.sh
```

## Docker Build

### Single Architecture Build

```bash
# Build for current architecture
docker build -t bytefreezer-proxy:latest .

# Run the container
docker run -d \
  --name bytefreezer-proxy \
  -p 8088:8088 \
  -p 2056:2056/udp \
  -v ./config.yaml:/etc/bytefreezer-proxy/config.yaml \
  bytefreezer-proxy:latest
```

### Multi-Architecture Build

```bash
# Create builder instance
docker buildx create --name multiarch --driver docker-container --use

# Build for multiple platforms
docker buildx build \
  --platform linux/amd64,linux/arm64,linux/arm/v7 \
  --tag ghcr.io/n0needt0/bytefreezer-proxy:latest \
  --push \
  .
```

### Dockerfile

The project includes an optimized multi-stage Dockerfile:

```dockerfile
# Located at ./Dockerfile
FROM golang:1.21-alpine AS builder
# ... (build stage)

FROM alpine:latest
# ... (runtime stage with minimal footprint)
```

## Release Process

### Automated Release (Recommended)

1. **Prepare Release**:
   ```bash
   # Update version in relevant files
   # Update CHANGELOG.md
   git add .
   git commit -m "Prepare release v1.0.0"
   git push origin main
   ```

2. **Create and Push Tag**:
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```

3. **GitHub Actions Automatically**:
   - Runs full test suite
   - Builds binaries for all platforms
   - Creates Docker images
   - Creates GitHub release with artifacts
   - Updates container registries

### Manual Release

If you need to create a release manually:

```bash
# Build all artifacts
./build.sh

# Create GitHub release using GitHub CLI
gh release create v1.0.0 \
  --title "ByteFreezer Proxy v1.0.0" \
  --notes-file CHANGELOG.md \
  dist/*.tar.gz \
  dist/*.zip
```

## Deployment

### Using Pre-built Binaries

```bash
# Download latest release
VERSION=$(curl -s https://api.github.com/repos/n0needt0/bytefreezer-proxy/releases/latest | grep tag_name | cut -d '"' -f 4)
ARCH="linux-amd64"  # or linux-arm64, linux-arm, etc.

wget "https://github.com/n0needt0/bytefreezer-proxy/releases/download/${VERSION}/bytefreezer-proxy-${ARCH}.tar.gz"
tar -xzf "bytefreezer-proxy-${ARCH}.tar.gz"
chmod +x bytefreezer-proxy-${ARCH}
```

### Using Ansible (Recommended)

The project includes comprehensive Ansible playbooks for deployment:

```bash
cd ansible

# Configure inventory
cp inventories/hosts.yml.example inventories/hosts.yml
# Edit inventories/hosts.yml with your target hosts

# Deploy using AWX (recommended)
# Import job templates from ansible/awx/ directory

# Or deploy directly with ansible-playbook
ansible-playbook -i inventories/hosts.yml playbooks/deploy.yml
```

### Using Docker

```bash
# Using Docker Compose
curl -O https://raw.githubusercontent.com/n0needt0/bytefreezer-proxy/main/docker-compose.yml
docker-compose up -d

# Using plain Docker
docker run -d \
  --name bytefreezer-proxy \
  --restart unless-stopped \
  -p 8088:8088 \
  -p 2056:2056/udp \
  -p 2057:2057/udp \
  -p 2058:2058/udp \
  -v /etc/bytefreezer-proxy:/etc/bytefreezer-proxy \
  -v /var/log/bytefreezer-proxy:/var/log/bytefreezer-proxy \
  -v /var/spool/bytefreezer-proxy:/var/spool/bytefreezer-proxy \
  ghcr.io/n0needt0/bytefreezer-proxy:latest
```

## Build Verification

### Health Checks

```bash
# Check if binary works
./bytefreezer-proxy --version
./bytefreezer-proxy --help

# Validate configuration
./bytefreezer-proxy --validate-config --config config.yaml

# Test API endpoint
curl http://localhost:8088/health
curl http://localhost:8088/metrics
```

### Load Testing

```bash
# Send test UDP packets
echo '{"test": "message", "timestamp": "'$(date -Iseconds)'"}' | nc -u localhost 2056

# Monitor metrics
curl -s http://localhost:8088/metrics | grep bytefreezer
```

## Troubleshooting

### Common Build Issues

1. **Go Module Issues**:
   ```bash
   go clean -modcache
   go mod download
   go mod tidy
   ```

2. **Cross-compilation Errors**:
   ```bash
   # Install cross-compilation toolchain
   go install golang.org/x/tools/cmd/goimports@latest
   ```

3. **Docker Build Issues**:
   ```bash
   # Clean Docker build cache
   docker builder prune -a
   
   # Check buildx availability
   docker buildx ls
   ```

4. **GitHub Actions Issues**:
   - Check repository secrets are properly set
   - Verify workflow file syntax
   - Check runner logs for detailed error messages

## Development Workflow

### Recommended Git Flow

```bash
# Create feature branch
git checkout -b feature/new-feature

# Make changes and commit
git add .
git commit -m "Add new feature"

# Push and create PR
git push origin feature/new-feature
# Create PR via GitHub UI

# After PR approval and CI passes
git checkout main
git pull origin main
git branch -d feature/new-feature
```

### Code Quality Checks

```bash
# Format code
go fmt ./...

# Run linter
golangci-lint run

# Run security scanner
gosec ./...

# Update dependencies
go get -u ./...
go mod tidy
```

This build documentation provides complete instructions for building, testing, and deploying the ByteFreezer Proxy project in various environments.