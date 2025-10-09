# Release Process

This document describes how to create a new release of terragrunt-runner.

## Automated Release (Recommended)

The release process is **fully automated** via GitHub Actions. You only need to create and push a version tag.

### Steps

```bash
# 1. Ensure you're on main with latest changes
git checkout main
git pull

# 2. Create and push the version tag
git tag -a v1.0.1 -m "Release v1.0.1"
git push origin v1.0.1
```

**That's it!** GitHub Actions will automatically:
1. Build binaries for Linux (amd64 and arm64)
2. Create a GitHub release for `v1.0.1` (marked as "Latest")
3. Update the major version tag `v1` to point to `v1.0.1`
4. Create/update a `v1` release (marked as pre-release)

Check progress at: https://github.com/boogy/terragrunt-runner/actions

### What Gets Created

After pushing `v1.0.1`, you'll have:
- **Git Tag**: `v1.0.1` pointing to your commit
- **Git Tag**: `v1` pointing to the same commit (auto-updated)
- **GitHub Release**: `v1.0.1` with binaries (marked as "Latest")
- **GitHub Release**: `v1` with binaries (marked as pre-release, rolling release)

## Manual Release (No Automation Available)

If you need to release without GitHub Actions (e.g., in a fork without the workflow):

### 1. Build Binaries

```bash
# Ensure you're on main and up to date
git checkout main
git pull

# Create version tag
VERSION="v1.0.1"
COMMIT=$(git rev-parse --short HEAD)
BUILD_TIME=$(date -u +'%Y-%m-%dT%H:%M:%SZ')

# Build for Linux amd64
GOOS=linux GOARCH=amd64 go build \
  -o terragrunt-runner-linux-amd64 \
  -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME}" \
  main.go

# Build for Linux arm64
GOOS=linux GOARCH=arm64 go build \
  -o terragrunt-runner-linux-arm64 \
  -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME}" \
  main.go
```

### 2. Create GitHub Release

```bash
# Create and push the version tag
git tag -a v1.0.1 -m "Release v1.0.1"
git push origin v1.0.1

# Create the release with binaries using gh CLI
gh release create v1.0.1 \
  --title "v1.0.1" \
  --generate-notes \
  terragrunt-runner-linux-amd64 \
  terragrunt-runner-linux-arm64
```

### 3. Update Major Version (Optional)

To allow users to reference `@v1`:

```bash
# Extract major version (e.g., v1 from v1.0.1)
MAJOR=$(echo "v1.0.1" | cut -d. -f1)

# Update major version tag
git tag -f $MAJOR v1.0.1
git push origin $MAJOR --force

# Create/update major version release
gh release delete $MAJOR --yes || true
gh release create $MAJOR \
  --title "$MAJOR" \
  --notes "Latest ${MAJOR}.x.x release (currently v1.0.1)

This is a rolling release tag that always points to the latest ${MAJOR}.x.x version.

**Current version:** v1.0.1

For production use, pin to a specific version like \`boogy/terragrunt-runner@v1.0.1\`." \
  --prerelease \
  terragrunt-runner-linux-amd64 \
  terragrunt-runner-linux-arm64
```

## Version Numbering

Follow [Semantic Versioning](https://semver.org/):

- **Patch** (v1.0.1, v1.0.2): Bug fixes, no new features
- **Minor** (v1.1.0, v1.2.0): New features, backward compatible
- **Major** (v2.0.0, v3.0.0): Breaking changes

## How Users Reference Releases

After releasing `v1.0.1`, users can reference it in multiple ways:

```yaml
# Pin to specific version (recommended for production)
- uses: boogy/terragrunt-runner@v1.0.1

# Use latest v1.x.x (gets automatic updates)
- uses: boogy/terragrunt-runner@v1

# Use latest commit on main (not recommended)
- uses: boogy/terragrunt-runner@main
```

When users run with `@v1`, the binary shows the actual version:
```
Terragrunt Runner Version: v1.0.1, BuildTime: 2025-10-09T20:55:20Z, Commit: 1ac5211
```

## Workflow Details

The GitHub Actions workflow ([.github/workflows/build-release.yaml](.github/workflows/build-release.yaml)) consists of three jobs:

1. **build**: Builds binaries for Linux (amd64 and arm64) with version info embedded
2. **release**: Creates GitHub release for the specific version (e.g., `v1.0.1`)
3. **update-major-release**: Updates the major version tag and release (e.g., `v1`)

The workflow is triggered on any tag push matching `v*` pattern.

## Troubleshooting

### Tag Already Exists

If you need to recreate a tag:

```bash
# Delete local tag
git tag -d v1.0.1

# Delete remote tag
git push origin --delete v1.0.1

# Create new tag
git tag -a v1.0.1 -m "Release v1.0.1"
git push origin v1.0.1
```

### GitHub Actions Failed

Check the workflow status:
```bash
# View recent workflow runs
gh run list --workflow="Build and Release" --limit 5

# View logs for a specific run
gh run view <run-id> --log-failed

# Re-run a failed workflow
gh run rerun <run-id>
```

### Release Created But Major Version Not Updated

If the automated workflow partially failed and `v1` wasn't updated, you can trigger it manually:

```bash
# Ensure the specific version tag exists
git push origin v1.0.1

# Manually update the major version tag
git fetch --tags
git tag -f v1 v1.0.1
git push origin v1 --force
```

Then manually create/update the release:
```bash
gh release delete v1 --yes || true
gh release create v1 \
  --title "v1" \
  --notes "Latest v1.x.x release (currently v1.0.1)" \
  --prerelease
```

### Need to Update Binary in Existing Release

If you need to rebuild binaries for an existing release:

```bash
# Build locally
VERSION="v1.0.1"
COMMIT=$(git rev-parse --short HEAD)
BUILD_TIME=$(date -u +'%Y-%m-%dT%H:%M:%SZ')

GOOS=linux GOARCH=amd64 go build \
  -o terragrunt-runner-linux-amd64 \
  -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME}" \
  main.go

GOOS=linux GOARCH=arm64 go build \
  -o terragrunt-runner-linux-arm64 \
  -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME}" \
  main.go

# Upload to existing release
gh release upload v1.0.1 terragrunt-runner-linux-amd64 terragrunt-runner-linux-arm64 --clobber
```
