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

> **🚀 Quick Start**: For a step-by-step setup guide, see [GITHUB_SETUP_CHECKLIST.md](GITHUB_SETUP_CHECKLIST.md)

##### General Settings
Navigate to `Settings > General` in your GitHub repository:

**Repository Visibility**:
- Choose `Public` for open-source or `Private` for internal projects
- Enable `Restrict pushes that create files` if needed for security

**Features Configuration**:
```yaml
✅ Issues: Enable for bug tracking and feature requests
✅ Projects: Enable for project management (optional)
✅ Wiki: Enable if you need additional documentation
✅ Discussions: Enable for community interaction (optional)
✅ Sponsorships: Enable if accepting sponsorships
❌ Preserve this repository: Leave unchecked unless archiving
```

**Pull Request Settings**:
```yaml
✅ Allow merge commits
✅ Allow squash merging (recommended for clean history)
❌ Allow rebase merging (can complicate history tracking)
✅ Automatically delete head branches (keeps repo clean)
✅ Allow auto-merge
❌ Allow update branch (can interfere with CI)
```

##### Branch Protection Rules
Navigate to `Settings > Branches` and add protection for `main` branch:

**Basic Protection**:
```yaml
Branch name pattern: main
✅ Restrict pushes that create files
✅ Require a pull request before merging
  ✅ Require approvals: 1 (minimum)
  ✅ Dismiss stale reviews when new commits are pushed
  ✅ Require review from code owners (if CODEOWNERS file exists)
  ✅ Restrict pushes that create files
```

**Status Check Requirements**:
```yaml
✅ Require status checks to pass before merging
✅ Require branches to be up to date before merging

Required Status Checks:
- Lint Code
- Test (1.21, 1.22, 1.23)
- Integration Tests  
- Vulnerability Scan
- Build Validation (ubuntu-latest, macos-latest, windows-latest)
- Ansible Validation
- Documentation Check
- Quality Gate
```

**Advanced Protection**:
```yaml
✅ Require signed commits (recommended for security)
✅ Require linear history (prevents merge commits)
✅ Include administrators (applies rules to repo admins)
❌ Allow force pushes (dangerous for main branch)
❌ Allow deletions (protects main branch from deletion)
```

##### Actions Configuration
Navigate to `Settings > Actions > General`:

**Actions Permissions**:
```yaml
✅ Allow all actions and reusable workflows
Or:
✅ Allow actions and reusable workflows from:
  - GitHub
  - Verified creators
  - Your organization (if applicable)
```

**Workflow Permissions**:
```yaml
✅ Read and write permissions (needed for releases)
✅ Allow GitHub Actions to create and approve pull requests
```

**Artifact and Log Retention**:
```yaml
Artifact retention: 90 days (balance storage vs. debugging needs)
Log retention: 90 days
```

##### Pages Configuration (Optional)
Navigate to `Settings > Pages` if hosting documentation:

```yaml
Source: Deploy from a branch
Branch: gh-pages or main
Folder: / (root) or /docs
Custom domain: proxy-docs.bytefreezer.com (optional)
✅ Enforce HTTPS
```

##### Security Settings
Navigate to `Settings > Security`:

**Vulnerability Alerts**:
```yaml
✅ Dependency graph
✅ Dependabot alerts
✅ Dependabot security updates
✅ Dependabot version updates (create dependabot.yml)
```

**Code Security and Analysis**:
```yaml
✅ Secret scanning (automatically enabled for public repos)
✅ Push protection (prevents committing secrets)
✅ Code scanning (configure CodeQL)
```

**Advanced Security Features** (GitHub Advanced Security required):
```yaml
✅ Secret scanning for non-provider patterns
✅ Code scanning with third-party tools
✅ Dependency review
```

##### Environment Configuration
Navigate to `Settings > Environments` for deployment environments:

**Production Environment**:
```yaml
Environment name: production
Protection rules:
  ✅ Required reviewers: [admin-team]
  ✅ Wait timer: 0 minutes
  ✅ Deployment branches: Selected branches (main only)
Environment secrets:
  - PRODUCTION_SSH_KEY
  - PRODUCTION_VAULT_PASSWORD
```

**Staging Environment**:
```yaml
Environment name: staging
Protection rules:
  ✅ Deployment branches: Selected branches (main, develop)
Environment secrets:
  - STAGING_SSH_KEY
```

##### Repository Rules (Beta)
Navigate to `Settings > Rules > Rulesets` for advanced rule management:

**Ruleset Configuration**:
```yaml
Ruleset name: Main Branch Protection
Target: Branch (main)
Rules:
  - Restrict creations
  - Restrict updates  
  - Restrict deletions
  - Require a pull request before merging
    - Required approving review count: 1
    - Dismiss stale reviews: true
  - Require status checks to pass
    - Strict: true (require up-to-date branches)
    - Status checks: [all CI workflow jobs]
  - Block force pushes
  - Require signed commits
```

##### Webhooks Configuration
Navigate to `Settings > Webhooks` for external integrations:

**AWX Integration Webhook** (if using):
```yaml
Payload URL: https://your-awx.example.com/api/v2/job_templates/123/github/
Content type: application/json
Secret: <your-webhook-secret>
Events:
  ✅ Push
  ✅ Pull requests
  ✅ Releases
SSL verification: ✅ Enable
```

**Slack/Discord Notifications**:
```yaml
Payload URL: https://hooks.slack.com/services/your/webhook/url
Events:
  ✅ Releases
  ✅ Issues
  ✅ Pull requests
  ✅ Workflow runs (for build notifications)
```

##### Advanced Repository Configuration

**CODEOWNERS File**:
Create `.github/CODEOWNERS`:
```bash
# Global owners
* @bytefreezer-team @security-team

# Go code specific
*.go @go-developers
go.mod @go-developers
go.sum @go-developers

# Ansible playbooks
ansible/ @devops-team @infrastructure-team
*.yml @devops-team

# CI/CD workflows
.github/workflows/ @devops-team @ci-maintainers

# Security sensitive files
Dockerfile @security-team @devops-team
docker-compose.yml @security-team
config.yaml @security-team

# Documentation
*.md @documentation-team
BUILD.md @devops-team @documentation-team
```

**Issue and PR Templates**:
Create `.github/ISSUE_TEMPLATE/bug_report.yml`:
```yaml
name: Bug Report
description: File a bug report
title: "[BUG]: "
labels: ["bug", "triage"]
body:
  - type: markdown
    attributes:
      value: |
        Thanks for taking the time to fill out this bug report!
  - type: input
    id: version
    attributes:
      label: Version
      description: What version of ByteFreezer Proxy are you running?
      placeholder: ex. v1.0.0
    validations:
      required: true
  - type: textarea
    id: what-happened
    attributes:
      label: What happened?
      description: Also tell us what you expected to happen
    validations:
      required: true
  - type: textarea
    id: config
    attributes:
      label: Configuration
      description: Relevant configuration (sanitized)
      render: yaml
  - type: dropdown
    id: deployment
    attributes:
      label: Deployment Method
      options:
        - Binary
        - Docker
        - Ansible
        - Kubernetes
    validations:
      required: true
```

**PR Template**:
Create `.github/pull_request_template.md`:
```markdown
## Description
Brief description of changes

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Breaking change
- [ ] Documentation update
- [ ] Performance improvement
- [ ] Security improvement

## Testing
- [ ] Unit tests pass
- [ ] Integration tests pass
- [ ] Manual testing completed
- [ ] Tested with real UDP traffic

## Checklist
- [ ] Code follows project style guidelines
- [ ] Self-review completed
- [ ] Code comments added for complex logic
- [ ] Documentation updated
- [ ] No new warnings introduced
- [ ] Backward compatibility maintained

## Related Issues
Closes #(issue_number)
```

**Dependabot Configuration**:
Create `.github/dependabot.yml`:
```yaml
version: 2
updates:
  # Go modules
  - package-ecosystem: "gomod"
    directory: "/"
    schedule:
      interval: "weekly"
      day: "monday"
      time: "04:00"
    reviewers:
      - "go-developers"
    assignees:
      - "security-team"
    commit-message:
      prefix: "deps"
      include: "scope"
    
  # GitHub Actions
  - package-ecosystem: "github-actions"
    directory: "/"
    schedule:
      interval: "weekly"
    reviewers:
      - "devops-team"
    commit-message:
      prefix: "ci"

  # Docker
  - package-ecosystem: "docker"
    directory: "/"
    schedule:
      interval: "weekly"
    reviewers:
      - "security-team"
```

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
  -p 2057:2057/udp \
  -p 2058:2058/udp \
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
# Send test UDP packets to different listeners
echo '{"test": "syslog message", "timestamp": "'$(date -Iseconds)'"}' | nc -u localhost 2056
echo '{"test": "ebpf data", "timestamp": "'$(date -Iseconds)'"}' | nc -u localhost 2057
echo '{"test": "app log", "timestamp": "'$(date -Iseconds)'"}' | nc -u localhost 2058

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