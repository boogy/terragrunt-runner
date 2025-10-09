#!/usr/bin/env bash
set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

print_step() {
    echo -e "${GREEN}[$1]${NC} $2"
}

print_info() {
    echo -e "${CYAN}ℹ${NC} $1"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗ Error:${NC} $1"
}

print_header() {
    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}========================================${NC}"
}

# Check if version argument provided
if [ -z "$1" ]; then
    print_error "Version required"
    echo ""
    echo -e "${YELLOW}Usage:${NC}"
    echo -e "  ./scripts/release.sh v1.0.1"
    echo ""
    echo -e "${YELLOW}What this script does:${NC}"
    echo -e "  1. Validates version format (v1.0.0, v1.0.1, etc.)"
    echo -e "  2. Ensures working directory is clean"
    echo -e "  3. Creates and pushes version tag (v1.0.1)"
    echo -e "  4. Waits for GitHub Actions to build and release"
    echo -e "  5. Automatically updates major version tag (v1)"
    echo ""
    exit 1
fi

VERSION=$1

# Validate version format
if ! [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    print_error "Invalid version format: $VERSION"
    echo -e "${YELLOW}Expected format: v1.0.0, v1.0.1, v2.0.0, etc.${NC}"
    exit 1
fi

# Extract major version
MAJOR=$(echo "$VERSION" | cut -d. -f1)

# Check if we're in a git repository
if ! git rev-parse --git-dir > /dev/null 2>&1; then
    print_error "Not in a git repository"
    exit 1
fi

print_header "Release: $VERSION"
echo -e "Version:      ${GREEN}${VERSION}${NC}"
echo -e "Major tag:    ${GREEN}${MAJOR}${NC}"
echo ""

# Step 1: Check working directory is clean
print_step "1/6" "Checking git status..."
if ! git diff-index --quiet HEAD --; then
    print_error "Working directory has uncommitted changes"
    echo ""
    git status --short
    echo ""
    echo -e "${YELLOW}Please commit or stash changes first${NC}"
    exit 1
fi
print_success "Working directory is clean"

# Step 2: Ensure we're on main/master
print_step "2/6" "Checking current branch..."
CURRENT_BRANCH=$(git branch --show-current)
if [ "$CURRENT_BRANCH" != "main" ] && [ "$CURRENT_BRANCH" != "master" ]; then
    print_error "Current branch is '$CURRENT_BRANCH'"
    echo -e "${YELLOW}Please switch to 'main' or 'master' branch${NC}"
    exit 1
fi
print_success "On branch: $CURRENT_BRANCH"

# Step 3: Pull latest changes
print_step "3/6" "Pulling latest changes..."
git pull --rebase
print_success "Up to date with remote"

# Step 4: Check if tag already exists
print_step "4/6" "Checking if tag exists..."
if git rev-parse "$VERSION" >/dev/null 2>&1; then
    print_error "Tag $VERSION already exists"
    echo ""
    echo -e "${YELLOW}To delete and recreate:${NC}"
    echo -e "  git tag -d $VERSION"
    echo -e "  git push origin :refs/tags/$VERSION"
    exit 1
fi
print_success "Tag $VERSION is available"

# Step 5: Create and push version tag
print_step "5/6" "Creating and pushing $VERSION tag..."
git tag -a "$VERSION" -m "Release $VERSION"
git push origin "$VERSION"
print_success "Tag $VERSION created and pushed"

# Step 6: Wait for GitHub Actions to complete
print_step "6/6" "Waiting for GitHub Actions to build release..."
print_info "This may take 1-2 minutes..."
echo ""

SLEEP_TIME=10
MAX_WAIT=300  # 5 minutes max
ELAPSED=0

while [ $ELAPSED -lt $MAX_WAIT ]; do
    if gh release view "$VERSION" >/dev/null 2>&1; then
        # Check if release has assets
        ASSET_COUNT=$(gh release view "$VERSION" --json assets --jq '.assets | length' 2>/dev/null || echo "0")
        if [ "$ASSET_COUNT" -gt 0 ]; then
            print_success "Release $VERSION is ready with $ASSET_COUNT assets"
            break
        else
            print_info "Release exists but no assets yet... (waiting ${ELAPSED}s)"
        fi
    else
        print_info "Waiting for release to be created... (${ELAPSED}s elapsed)"
    fi

    sleep $SLEEP_TIME
    ELAPSED=$((ELAPSED + SLEEP_TIME))
done

if [ $ELAPSED -ge $MAX_WAIT ]; then
    print_error "Timeout waiting for release"
    echo ""
    echo -e "${YELLOW}Check GitHub Actions manually:${NC}"
    REPO=$(git config --get remote.origin.url | sed 's/.*github.com[:/]\(.*\)\.git/\1/')
    echo -e "  https://github.com/${REPO}/actions"
    echo ""
    echo -e "${YELLOW}After workflow completes, update major tag manually:${NC}"
    echo -e "  git tag -f $MAJOR ${VERSION}^{}"
    echo -e "  git push origin $MAJOR --force"
    exit 1
fi

echo ""
print_header "Updating Major Version Release"

# Update major version tag
print_step "1/4" "Updating $MAJOR tag to point to $VERSION..."
git tag --no-sign -f "$MAJOR" "${VERSION}^{}"
git push origin "$MAJOR" --force
print_success "$MAJOR tag updated"

# Download binaries from the version release
print_step "2/4" "Downloading binaries from $VERSION release..."
TEMP_DIR=$(mktemp -d)
gh release download "$VERSION" -D "$TEMP_DIR"
print_success "Binaries downloaded to $TEMP_DIR"

# Delete existing major version release if it exists
print_step "3/4" "Updating $MAJOR release..."
gh release delete "$MAJOR" --yes 2>/dev/null || true

# Create new major version release as pre-release
gh release create "$MAJOR" \
  --title "$MAJOR" \
  --notes "Latest ${MAJOR}.x.x release (currently $VERSION)

This is a rolling release tag that always points to the latest ${MAJOR}.x.x version.

**Current version:** $VERSION

For production use, pin to a specific version like \`boogy/terragrunt-runner@$VERSION\`." \
  --prerelease \
  "$TEMP_DIR"/*

# Cleanup
rm -rf "$TEMP_DIR"
print_success "$MAJOR release created"

# Verify
print_step "4/4" "Verifying setup..."
print_success "All tags and releases created successfully"

# Final summary
echo ""
print_header "✓ Release Complete!"
echo ""
echo -e "${GREEN}Successfully created:${NC}"
echo -e "  • ${CYAN}${VERSION}${NC} - Specific version release (marked as Latest)"
echo -e "  • ${CYAN}${MAJOR}${NC}    - Major version release (Pre-release, always points to latest ${MAJOR}.x.x)"
echo ""

REPO=$(git config --get remote.origin.url | sed 's/.*github.com[:/]\(.*\)\.git/\1/')
echo -e "${YELLOW}Release URLs:${NC}"
echo -e "  • https://github.com/${REPO}/releases/tag/${VERSION}"
echo -e "  • https://github.com/${REPO}/releases/tag/${MAJOR}"
echo ""
echo -e "${YELLOW}Usage in GitHub Actions:${NC}"
echo -e "  ${BLUE}- uses: ${REPO}@${VERSION}${NC}  # Pin to specific version (recommended)"
echo -e "  ${BLUE}- uses: ${REPO}@${MAJOR}${NC}     # Use latest ${MAJOR}.x.x version (auto-updates)"
echo ""
echo -e "${GREEN}Note:${NC} Both releases contain binaries built from ${VERSION} with version info embedded."
echo ""
