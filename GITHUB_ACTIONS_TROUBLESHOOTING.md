# GitHub Actions Troubleshooting Guide

## Common Issues and Solutions

### Issue 1: "Workflow file is invalid"

**Symptoms:**
- Red X in Actions tab
- "Invalid workflow file" message
- Workflows don't appear in Actions tab

**Solutions:**
```bash
# Check YAML syntax
cd .github/workflows/
yamllint ci.yml
yamllint build-and-release.yml

# Or use online YAML validator
# Copy/paste workflow content to: https://www.yamllint.com/
```

**Common YAML Issues:**
```yaml
# ❌ Wrong indentation
jobs:
test:  # Missing spaces
  runs-on: ubuntu-latest

# ✅ Correct indentation  
jobs:
  test:  # 2 spaces
    runs-on: ubuntu-latest

# ❌ Missing quotes in complex strings
run: echo ${{ github.ref }}

# ✅ Proper quoting
run: echo "${{ github.ref }}"
```

### Issue 2: "No such file or directory: go.mod"

**Symptoms:**
- Build fails with "go.mod not found"
- "cannot load module" errors

**Solutions:**
```bash
# 1. Check if go.mod exists in repository root
ls -la go.mod

# 2. If missing, initialize Go module
go mod init github.com/n0needt0/bytefreezer-proxy

# 3. Download dependencies
go mod tidy

# 4. Commit and push
git add go.mod go.sum
git commit -m "Add Go module files"
git push
```

### Issue 3: "Build command failed"

**Symptoms:**
- Build step fails
- "no Go files in /app" error
- "cannot find main module" error

**Solutions:**

**Check main.go location:**
```bash
# If main.go is in cmd/ directory
RUN go build -o bytefreezer-proxy ./cmd/main.go

# If main.go is in root directory  
RUN go build -o bytefreezer-proxy ./main.go

# If using package main in root
RUN go build -o bytefreezer-proxy .
```

**Update Dockerfile:**
```dockerfile
# Make sure build command matches your structure
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
    -o bytefreezer-proxy ./cmd/main.go
```

### Issue 4: "Permission denied" or "Command not found"

**Symptoms:**
- "./bytefreezer-proxy: permission denied"
- "curl: command not found"
- "nc: command not found"

**Solutions:**
```yaml
# Fix executable permissions
- name: Set executable permissions
  run: chmod +x ./bytefreezer-proxy

# Install missing commands in CI
- name: Install required packages
  run: |
    sudo apt-get update
    sudo apt-get install -y curl netcat-openbsd
```

### Issue 5: "Tests fail but work locally"

**Symptoms:**
- Tests pass locally but fail in CI
- "connection refused" errors in tests
- Race condition failures

**Solutions:**
```yaml
# Add proper service dependencies
services:
  mock-receiver:
    image: nginx:alpine
    ports:
      - 8080:80

# Wait for services to be ready
- name: Wait for services
  run: |
    timeout 30 bash -c 'until curl -f http://localhost:8080/health; do sleep 1; done'

# Run tests with race detection
- name: Run tests
  run: go test -race -v ./...
```

### Issue 6: "Docker build fails"

**Symptoms:**
- Docker build step fails
- "no such file or directory" in Docker
- "COPY failed" errors

**Solutions:**
```dockerfile
# Check file paths in Dockerfile
COPY go.mod go.sum ./     # Files must exist
COPY . .                  # Copies everything

# Make sure .dockerignore doesn't exclude needed files
# Check .dockerignore content:
# node_modules
# .git
# *.md (don't exclude if copying)
```

### Issue 7: "Secrets not available"

**Symptoms:**
- "secret not defined" errors
- Authentication failures
- Missing environment variables

**Solutions:**
```yaml
# Check secret names match exactly
env:
  GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}  # ✅ Correct
  GITHUB_TOKEN: ${{ secrets.github_token }}  # ❌ Wrong case

# Add secrets in repository settings:
# Settings > Secrets and variables > Actions
# Add repository secrets:
# - DOCKER_HUB_USERNAME
# - DOCKER_HUB_TOKEN
```

### Issue 8: "Matrix build failures"

**Symptoms:**
- Some matrix jobs fail
- Inconsistent results across OS/versions
- "command not found" on specific OS

**Solutions:**
```yaml
# Use conditional steps for different OS
- name: Install dependencies (Linux)
  if: runner.os == 'Linux'
  run: sudo apt-get install -y curl

- name: Install dependencies (macOS)  
  if: runner.os == 'macOS'
  run: brew install curl

- name: Install dependencies (Windows)
  if: runner.os == 'Windows'
  run: choco install curl
```

### Issue 9: "Workflow not triggering"

**Symptoms:**
- Push/PR doesn't trigger workflow
- No workflow runs appear
- "No workflows" message

**Solutions:**
```yaml
# Check trigger configuration
on:
  push:
    branches: [ main, develop ]    # Must match your branch names
  pull_request:
    branches: [ main ]             # Must match target branches

# Common issues:
# - Branch name mismatch (main vs master)
# - Workflow file not in .github/workflows/
# - Invalid YAML syntax
# - File not committed to default branch
```

### Issue 10: "Integration tests fail"

**Symptoms:**
- Health check failures
- "connection refused" in tests
- Service startup issues

**Solutions:**
```yaml
# Increase startup wait time
- name: Start service
  run: |
    ./bytefreezer-proxy --config test-config.yaml &
    echo $! > proxy.pid
    sleep 10  # Increased from 5 seconds

# Add retry logic for health checks
- name: Test health endpoint
  run: |
    for i in {1..30}; do
      if curl -f http://localhost:8088/health; then
        echo "Health check passed"
        break
      fi
      echo "Attempt $i failed, retrying..."
      sleep 2
    done
```

### Issue 11: "Deprecated GitHub Actions"

**Symptoms:**
- "This request has been automatically failed because it uses a deprecated version"
- Warnings about deprecated actions in workflow runs
- Build failures with deprecation notices

**Common Deprecated Actions and Their Replacements:**
```yaml
# ❌ Deprecated actions
uses: actions/upload-artifact@v3
uses: actions/download-artifact@v3
uses: actions/cache@v3
uses: actions/setup-python@v4
uses: actions/create-release@v1
uses: docker/setup-buildx-action@v3
uses: docker/login-action@v3
uses: docker/metadata-action@v5
uses: docker/build-push-action@v5

# ✅ Updated actions
uses: actions/upload-artifact@v4
uses: actions/download-artifact@v4
uses: actions/cache@v4
uses: actions/setup-python@v5
uses: softprops/action-gh-release@v2
uses: docker/setup-buildx-action@v4
uses: docker/login-action@v4
uses: docker/metadata-action@v6
uses: docker/build-push-action@v6
```

**Solutions:**
```bash
# Update all deprecated actions in your workflows
# Search and replace across all workflow files:
sed -i 's/actions\/upload-artifact@v3/actions\/upload-artifact@v4/g' .github/workflows/*.yml
sed -i 's/actions\/download-artifact@v3/actions\/download-artifact@v4/g' .github/workflows/*.yml
sed -i 's/actions\/cache@v3/actions\/cache@v4/g' .github/workflows/*.yml
sed -i 's/actions\/setup-python@v4/actions\/setup-python@v5/g' .github/workflows/*.yml

# For releases, replace deprecated create-release with modern approach:
# Replace actions/create-release@v1 with softprops/action-gh-release@v2
```

## Debugging Steps

### 1. Check Workflow Syntax
```bash
# Validate YAML syntax
yamllint .github/workflows/*.yml

# Check for common issues
grep -n "secrets\." .github/workflows/*.yml  # Check secret names
grep -n "github\." .github/workflows/*.yml   # Check context usage
```

### 2. Enable Debug Logging
```yaml
# Add to workflow for more detailed logs
env:
  ACTIONS_STEP_DEBUG: true
  ACTIONS_RUNNER_DEBUG: true
```

### 3. Test Locally with Act
```bash
# Install act (GitHub Actions local runner)
# macOS: brew install act
# Linux: curl https://raw.githubusercontent.com/nektos/act/master/install.sh | sudo bash

# Run workflow locally
act push
act pull_request
```

### 4. Check Dependencies
```bash
# Verify go.mod is correct
go mod verify
go mod tidy

# Check for missing dependencies
go list -m all
```

### 5. Verify File Structure
```bash
# Required files for ByteFreezer Proxy CI:
tree -a -I '.git'
.
├── .github/
│   └── workflows/
│       ├── ci.yml
│       └── build-and-release.yml
├── cmd/
│   └── main.go
├── go.mod
├── go.sum
├── config.yaml
├── Dockerfile
└── docker-compose.yml
```

## Quick Fixes Reference

| Error Message | Quick Fix |
|---------------|-----------|
| `go.mod not found` | `go mod init && go mod tidy` |
| `permission denied` | `chmod +x binary-name` |
| `command not found: curl` | Add `sudo apt-get install -y curl` |
| `invalid workflow file` | Check YAML indentation |
| `secret not defined` | Add in Settings > Secrets |
| `no such file` in Docker | Check COPY paths in Dockerfile |
| `tests fail in CI only` | Add service dependencies |
| `matrix job fails on Windows` | Use conditional steps |
| `workflow not triggering` | Check branch names in `on:` |
| `deprecated version of actions/upload-artifact: v3` | Update to `actions/upload-artifact@v4` |
| `deprecated version of actions/download-artifact: v3` | Update to `actions/download-artifact@v4` |
| `deprecated version of actions/cache: v3` | Update to `actions/cache@v4` |

## Getting Help

If you're still having issues, please share:

1. **Full error message** from Actions log
2. **Which workflow job is failing**
3. **Your repository structure** (`tree` output)
4. **Recent changes** that might have caused the issue
5. **Screenshots** of the error in GitHub UI

This will help identify the specific issue and provide a targeted solution.