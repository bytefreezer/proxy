# GitHub Repository Setup Checklist - ByteFreezer Proxy

This is a step-by-step checklist for setting up your ByteFreezer Proxy GitHub repository with proper CI/CD and security settings.

## ✅ Quick Setup Checklist

### Step 1: Enable GitHub Actions

1. **Navigate to Repository Settings**
   - Go to your repository on GitHub
   - Click `Settings` tab
   - Click `Actions` in the left sidebar
   - Click `General`

2. **Configure Actions Permissions**
   ```
   ✅ Actions permissions: "Allow all actions and reusable workflows"
   ✅ Workflow permissions: "Read and write permissions"
   ✅ Allow GitHub Actions to create and approve pull requests
   ```

3. **Verify Actions Are Enabled**
   - Go to the `Actions` tab in your repository
   - You should see the message "Get started with GitHub Actions"
   - If workflows exist, they should be listed here

### Step 2: Set Branch Protection Rules for Main Branch

1. **Navigate to Branch Settings**
   - Go to `Settings` > `Branches`
   - Click `Add rule` or `Add branch protection rule`

2. **Configure Branch Name Pattern**
   ```
   Branch name pattern: main
   ```

3. **Enable Basic Protection**
   ```yaml
   ✅ Restrict pushes that create files
   ✅ Require a pull request before merging
     ✅ Require approvals: 1
     ✅ Dismiss stale reviews when new commits are pushed
     ✅ Require review from code owners
   ✅ Require status checks to pass before merging
   ✅ Require branches to be up to date before merging
   ✅ Require signed commits (recommended)
   ✅ Require linear history
   ✅ Include administrators
   ❌ Allow force pushes
   ❌ Allow deletions
   ```

4. **Click "Create" to save the rule**

### Step 3: Configure Required Status Checks

**Important**: You must push your workflows to the repository FIRST before status checks will appear in the list.

1. **Push Your Workflow Files**
   ```bash
   # Make sure these files exist in your repo:
   .github/workflows/ci.yml
   .github/workflows/build-and-release.yml
   ```

2. **Trigger Initial Workflow Runs**
   ```bash
   # Push to main or create a PR to trigger workflows
   git push origin main
   # OR create a test PR
   ```

3. **Add Status Checks to Branch Protection**
   - Go back to `Settings` > `Branches`
   - Edit your `main` branch protection rule
   - In the "Require status checks to pass before merging" section
   - Search for and select these status checks:

   **Required Status Checks for ByteFreezer Proxy:**
   ```
   ✅ Lint Code
   ✅ Test (1.21)
   ✅ Test (1.22) 
   ✅ Test (1.23)
   ✅ Integration Tests
   ✅ Vulnerability Scan
   ✅ Build Validation (ubuntu-latest)
   ✅ Build Validation (macos-latest)
   ✅ Build Validation (windows-latest)
   ✅ Ansible Validation
   ✅ Documentation Check
   ✅ Quality Gate
   ```

4. **Save the Updated Rule**

## 🚀 Verification Steps

### Verify Actions Are Working

1. **Check Actions Tab**
   - Go to repository `Actions` tab
   - You should see workflow runs
   - Green checkmarks = passing, Red X = failing

2. **Test Branch Protection**
   ```bash
   # Try to push directly to main (should fail)
   git checkout main
   echo "test" >> README.md
   git add README.md
   git commit -m "test direct push"
   git push origin main
   # Should see: "error: failed to push some refs"
   ```

3. **Test PR Workflow**
   ```bash
   # Create a feature branch and PR
   git checkout -b test-protection
   echo "# Test" >> TEST.md
   git add TEST.md
   git commit -m "Add test file"
   git push origin test-protection
   # Create PR via GitHub UI
   # Verify all status checks run
   ```

### Verify Status Checks Are Required

1. **Create a Test PR**
   - Status checks should appear automatically
   - PR should show "Some checks haven't completed yet"
   - Merge button should be disabled until checks pass

2. **Check Protection Settings**
   - Go to `Settings` > `Branches`
   - Your `main` branch should show:
     - "Branch protection rules" badge
     - List of required status checks

## 🔧 Troubleshooting Common Issues

### Issue: "No status checks found"

**Solution:**
1. Ensure workflow files are pushed to repository
2. Trigger at least one workflow run (push/PR)
3. Wait for workflows to complete
4. Then add status checks to branch protection

### Issue: "Actions not running"

**Solution:**
```yaml
# Check workflow file syntax
cd .github/workflows/
yamllint ci.yml
yamllint build-and-release.yml

# Verify triggers are correct
on:
  push:
    branches: [ main, develop ]
  pull_request:
    branches: [ main, develop ]
```

### Issue: "Can't merge PR despite passing checks"

**Solution:**
1. Check if branch is up to date: `git pull origin main`
2. Verify all required status checks are listed
3. Check if signed commits are required: `git commit -S`

### Issue: "Status check names don't match"

**Solution:**
Check exact job names in workflow files:
```yaml
# In ci.yml
jobs:
  lint:           # Shows as "Lint Code" 
  test:           # Shows as "Test (1.21)" etc.
  integration-test: # Shows as "Integration Tests"
```

## 📋 Complete Configuration Commands

If you prefer command-line setup using GitHub CLI:

```bash
# Install GitHub CLI first: https://cli.github.com/

# Enable Actions (if not already enabled)
gh api repos/:owner/:repo -X PATCH -f has_issues=true -f has_projects=true -f has_wiki=true

# Create branch protection rule
gh api repos/:owner/:repo/branches/main/protection -X PUT --input - <<EOF
{
  "required_status_checks": {
    "strict": true,
    "contexts": [
      "Lint Code",
      "Test (1.21)",
      "Test (1.22)", 
      "Test (1.23)",
      "Integration Tests",
      "Vulnerability Scan",
      "Build Validation (ubuntu-latest)",
      "Build Validation (macos-latest)",
      "Build Validation (windows-latest)",
      "Ansible Validation",
      "Documentation Check",
      "Quality Gate"
    ]
  },
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "required_approving_review_count": 1,
    "dismiss_stale_reviews": true,
    "require_code_owner_reviews": true
  },
  "restrictions": null,
  "required_linear_history": true,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "required_conversation_resolution": true
}
EOF
```

## 🎯 Final Verification Checklist

- [ ] Actions tab shows workflow runs
- [ ] Branch protection rule exists for `main`
- [ ] All required status checks are configured
- [ ] Direct pushes to main are blocked
- [ ] PRs require approval and status checks
- [ ] Status checks run automatically on PRs
- [ ] Merge button is disabled until checks pass

## 📝 Next Steps After Setup

1. **Create Development Branch**
   ```bash
   git checkout -b develop
   git push origin develop
   # Add protection rules for develop if needed
   ```

2. **Set Up Team Permissions**
   - Go to `Settings` > `Manage access`
   - Add team members with appropriate permissions
   - Consider creating teams: @developers, @maintainers, @security

3. **Configure Notifications**
   - Set up Slack/Discord webhooks
   - Configure email notifications
   - Set up issue/PR assignment rules

4. **Test Full Workflow**
   - Create a feature branch
   - Make changes and push
   - Create PR and verify all checks run
   - Get approval and merge

Your ByteFreezer Proxy repository is now properly configured with enterprise-grade CI/CD and security settings!