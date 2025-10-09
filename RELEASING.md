# Release Process

This document describes how to create a new release of terragrunt-runner.

## Quick Release (Using Script)

If you have the local helper script `scripts/release.sh`:

```bash
# Make sure you're on main with latest changes
git checkout main
git pull

# Run the release script
./scripts/release.sh v1.0.1
```

The script will:
1. Validate version format and git status
2. Create and push the version tag (v1.0.1)
3. Wait for GitHub Actions to build the release
4. Automatically update the major version tag (v1)

## Manual Release

If you don't have the script, follow these steps:

### 1. Create Version Tag

```bash
# Ensure you're on main and up to date
git checkout main
git pull

# Create and push the version tag
git tag -a v1.0.1 -m "Release v1.0.1"
git push origin v1.0.1
```

### 2. Wait for GitHub Actions

The workflow will automatically:
- Build binaries for Linux (amd64 and arm64)
- Create a GitHub release with the binaries
- Mark it as "Latest"

Check progress at: https://github.com/boogy/terragrunt-runner/actions

### 3. Update Major Version Tag

After the release is published (~60 seconds):

```bash
# Update v1 to point to v1.0.1
git tag -f v1 v1.0.1^{}
git push origin v1 --force
```

## Version Numbering

Follow [Semantic Versioning](https://semver.org/):

- **Patch** (v1.0.1, v1.0.2): Bug fixes, no new features
- **Minor** (v1.1.0, v1.2.0): New features, backward compatible
- **Major** (v2.0.0, v3.0.0): Breaking changes

## What Happens

### Releases Created
- **v1.0.1**: Specific version release (marked as "Latest")
- **v1**: Git tag only (no separate release, points to v1.0.1)

### For Users
```yaml
# Pin to specific version (recommended for production)
- uses: boogy/terragrunt-runner@v1.0.1

# Use latest v1.x.x (gets automatic updates)
- uses: boogy/terragrunt-runner@v1
```

### Binary Version
When users run with `@v1`, the binary shows the full version:
```
Terragrunt Runner Version: v1.0.1, BuildTime: ..., Commit: ...
```

## Troubleshooting

### Tag Already Exists

```bash
# Delete local tag
git tag -d v1.0.1

# Delete remote tag
git push origin :refs/tags/v1.0.1

# Try again
git tag -a v1.0.1 -m "Release v1.0.1"
git push origin v1.0.1
```

### GitHub Actions Failed

Check the workflow logs:
```bash
gh run list --workflow="Build and Release" --limit 5
gh run view <run-id> --log-failed
```

### Need to Update v1 Only

If you already have v1.0.2 released but forgot to update v1:

```bash
git tag -f v1 v1.0.2^{}
git push origin v1 --force
```

## Helper Script

Create `scripts/release.sh` locally (not committed due to .gitignore):

```bash
#!/usr/bin/env bash
set -e

# See full script content in the automation section below
# Or ask for the script content
```

Make it executable:
```bash
chmod +x scripts/release.sh
```

Usage:
```bash
./scripts/release.sh v1.0.1
```
