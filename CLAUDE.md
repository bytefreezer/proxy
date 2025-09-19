# Instructions for Claude

## Default Workflow
When working on this project, always follow these steps:

### 1. Before Making Changes
- [ ] Read this file first for any specific instructions
- [ ] Check RELEASENOTES.md to understand recent changes
- [ ] Review the current todo list if applicable

### 2. When Adding Features
- [ ] Implement the feature with proper error handling
- [ ] Add appropriate logging (use existing log patterns)
- [ ] Build and test the changes (`go build`)
- [ ] Update RELEASENOTES.md with feature description
- [ ] Create example usage documentation if it's an API feature

### 3. When Fixing Bugs
- [ ] Identify root cause and document it
- [ ] Implement fix with error handling
- [ ] Test the fix thoroughly
- [ ] Update RELEASENOTES.md with bug fix details

### 4. Code Standards
- [ ] Follow existing Go conventions in the codebase
- [ ] Use existing imports and patterns (don't add new dependencies without asking)
- [ ] Add proper error logging for user-facing issues
- [ ] Keep functions focused and well-documented

### 5. API Changes
- [ ] Follow existing API patterns in `/api/handlers.go`
- [ ] Add new endpoints to `/api/api.go` router
- [ ] Create request/response structs with JSON tags
- [ ] Test with curl examples

### 6. Always Update
- [ ] RELEASENOTES.md with clear feature/fix descriptions
- [ ] Any relevant example files or documentation
- [ ] Build the binary to ensure it compiles

## Specific Project Notes
- This is a UDP data proxy that forwards to bytefreezer-receiver
- Focus on reliability, error handling, and observability
- All API endpoints should follow the `/api/v2/` pattern
- Use structured logging with appropriate levels (debug, info, warn, error)

## Current Priority Areas
1. Stability and error handling
2. API functionality and testing
3. Documentation and examples
4. Performance monitoring

## Don't Do This
- Don't add complex automation without asking
- Don't change core architecture without discussion
- Don't add new external dependencies without approval
- Don't modify the Makefile or CI without asking

## When In Doubt
- Ask before making architectural changes
- Follow existing patterns in the codebase
- Keep it simple and maintainable