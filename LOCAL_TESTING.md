# Local Testing Before Commits

This guide shows you how to run the same checks locally that run in CI, preventing failed builds.

## Quick Start

### Option 1: Using Makefile (Recommended)

```bash
# Install required tools
make install-tools

# Run all pre-commit checks
make pre-commit

# Run individual checks
make fmt          # Check formatting
make vet          # Run go vet  
make test         # Run tests
make lint         # Run all linting (fmt, vet, staticcheck, gosec)
make build        # Build binary

# Run full CI pipeline locally
make ci-local
```

### Option 2: Using Git Hooks

```bash
# Set up the git hook (one-time setup)
git config core.hooksPath .githooks

# Now git commit will automatically run checks
git commit -m "your message"
```

### Option 3: Manual Commands

```bash
# 1. Install tools (one-time setup)
go install honnef.co/go/tools/cmd/staticcheck@latest
go install github.com/securego/gosec/v2/cmd/gosec@latest

# 2. Run checks manually
gofmt -s -l .                                    # Check formatting
go vet ./...                                     # Static analysis
go test -v ./...                                 # Run tests
staticcheck ./...                                # Advanced static analysis
gosec -severity medium -confidence medium ./...  # Security scan
```

## What Gets Checked

✅ **Go Formatting** - Code follows standard Go formatting  
✅ **Go Vet** - Catches common Go programming errors  
✅ **Tests** - All unit tests pass  
✅ **Staticcheck** - Advanced static analysis for bugs and inefficiencies  
⚠️ **Gosec** - Security vulnerability scanning (informational)

## Recommended Workflow

```bash
# Before starting work
make install-tools

# During development (run frequently)
make test

# Before committing
make pre-commit

# If you want to test the full CI pipeline
make ci-local
```

## IDE Integration

### VS Code
Add to your `.vscode/tasks.json`:
```json
{
    "version": "2.0.0",
    "tasks": [
        {
            "label": "pre-commit",
            "type": "shell",
            "command": "make",
            "args": ["pre-commit"],
            "group": "test",
            "presentation": {
                "echo": true,
                "reveal": "always",
                "focus": false,
                "panel": "shared"
            }
        }
    ]
}
```

### GoLand/IntelliJ
Create an External Tool:
- Name: Pre-commit checks
- Program: make  
- Arguments: pre-commit
- Working directory: $ProjectFileDir$

## Troubleshooting

**"staticcheck: command not found"**
```bash
go install honnef.co/go/tools/cmd/staticcheck@latest
```

**"gosec: command not found"**  
```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

**Formatting issues**
```bash
gofmt -s -w .  # Fix all formatting issues
```

**Tests failing**
```bash
go test -v ./... # Run with verbose output to see details
```

## Benefits

- 🚀 **Faster feedback** - Catch issues before pushing
- 🛡️ **Prevent broken builds** - Same checks as CI
- 🔧 **Easy to fix** - Issues caught locally are easier to debug
- ⚡ **Save time** - No waiting for CI to run
- 🎯 **Better code quality** - Consistent standards enforced