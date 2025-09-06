# Build Guide

## Quick Build

```bash
# Clone and build
git clone https://github.com/n0needt0/bytefreezer-proxy.git
cd bytefreezer-proxy
go build .

# Run
./bytefreezer-proxy --config config.yaml
```

## Development

```bash
# Install dependencies
go mod download

# Run tests
go test ./...

# Format code
go fmt ./...

# Build with version info
VERSION=$(git describe --tags --always)
go build -ldflags "-X main.version=$VERSION" .
```

## Cross-Platform Build

```bash
# Linux AMD64
GOOS=linux GOARCH=amd64 go build -o bytefreezer-proxy-linux-amd64

# Linux ARM64
GOOS=linux GOARCH=arm64 go build -o bytefreezer-proxy-linux-arm64

# Windows
GOOS=windows GOARCH=amd64 go build -o bytefreezer-proxy-windows.exe

# macOS
GOOS=darwin GOARCH=amd64 go build -o bytefreezer-proxy-darwin
```

## Docker

```bash
# Build image
docker build -t bytefreezer-proxy .

# Run container
docker run -p 8088:8088 -p 2056-2058:2056-2058/udp bytefreezer-proxy
```

## Release

GitHub Actions automatically builds and releases when you push tags:

```bash
git tag v1.0.0
git push origin v1.0.0
```

Creates:
- Multi-platform binaries
- Docker images
- GitHub release
- Debian packages

## Deployment

Use Ansible playbooks for production deployment:

```bash
cd ansible/playbooks
ansible-playbook -i inventory install.yml
```