#!/bin/bash
set -e

# Function to install Terragrunt
install_terragrunt() {
    local VERSION="${TERRAGRUNT_VERSION}"

    # Only install if version is specified
    if [ -z "$VERSION" ] || [ "$VERSION" = "" ]; then
        echo "Terragrunt version not specified, skipping installation"
        return 0
    fi

    echo "Installing Terragrunt v${VERSION}..."

    # Check if already installed with correct version
    if command -v terragrunt &> /dev/null; then
        INSTALLED_VERSION=$(terragrunt --version 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
        if [ "$INSTALLED_VERSION" = "$VERSION" ]; then
            echo "Terragrunt v${VERSION} already installed"
            return 0
        fi
    fi

    # Determine architecture
    ARCH=$(uname -m)
    if [ "$ARCH" = "x86_64" ]; then
        ARCH="amd64"
    elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
        ARCH="arm64"
    fi

    # Download and install
    TERRAGRUNT_URL="https://github.com/gruntwork-io/terragrunt/releases/download/v${VERSION}/terragrunt_linux_${ARCH}"
    curl -fsSLo /opt/tools/terragrunt "$TERRAGRUNT_URL"
    chmod +x /opt/tools/terragrunt

    # Verify installation
    /opt/tools/terragrunt --version
}

# Function to install OpenTofu
install_opentofu() {
    local VERSION="${OPENTOFU_VERSION}"

    # Only install if version is specified
    if [ -z "$VERSION" ] || [ "$VERSION" = "" ]; then
        echo "OpenTofu version not specified, skipping installation"
        return 0
    fi

    echo "Installing OpenTofu v${VERSION}..."

    # Check if already installed with correct version
    if command -v tofu &> /dev/null; then
        INSTALLED_VERSION=$(tofu version 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
        if [ "$INSTALLED_VERSION" = "$VERSION" ]; then
            echo "OpenTofu v${VERSION} already installed"
            return 0
        fi
    fi

    # Determine architecture
    ARCH=$(uname -m)
    if [ "$ARCH" = "x86_64" ]; then
        ARCH="amd64"
    elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
        ARCH="arm64"
    fi

    # Download and install
    TOFU_URL="https://github.com/opentofu/opentofu/releases/download/v${VERSION}/tofu_${VERSION}_linux_${ARCH}.tar.gz"
    curl -fsSLo /tmp/tofu.tar.gz "$TOFU_URL"
    tar -xzf /tmp/tofu.tar.gz -C /opt/tools/ tofu
    rm /tmp/tofu.tar.gz
    chmod +x /opt/tools/tofu

    # Create terraform symlink for compatibility
    ln -sf /opt/tools/tofu /opt/tools/terraform

    # Verify installation
    /opt/tools/tofu version
}

# Function to install Terraform
install_terraform() {
    local VERSION="${TERRAFORM_VERSION}"

    # Only install if version is specified
    if [ -z "$VERSION" ] || [ "$VERSION" = "" ]; then
        echo "Terraform version not specified, skipping installation"
        return 0
    fi

    echo "Installing Terraform v${VERSION}..."

    # Check if already installed with correct version
    if command -v terraform &> /dev/null; then
        INSTALLED_VERSION=$(terraform version 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)
        if [ "$INSTALLED_VERSION" = "$VERSION" ]; then
            echo "Terraform v${VERSION} already installed"
            return 0
        fi
    fi

    # Determine architecture
    ARCH=$(uname -m)
    if [ "$ARCH" = "x86_64" ]; then
        ARCH="amd64"
    elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
        ARCH="arm64"
    fi

    # Download and install
    TF_URL="https://releases.hashicorp.com/terraform/${VERSION}/terraform_${VERSION}_linux_${ARCH}.zip"
    curl -fsSLo /tmp/terraform.zip "$TF_URL"
    unzip -q -o /tmp/terraform.zip -d /opt/tools/
    rm /tmp/terraform.zip
    chmod +x /opt/tools/terraform

    # Verify installation
    /opt/tools/terraform version
}

# Add tools to PATH
export PATH="/opt/tools:$PATH"

# Install tools ONLY if their versions are explicitly set
echo "Checking for tool installation requirements..."

# Install Terragrunt if version is specified
if [ -n "$TERRAGRUNT_VERSION" ] && [ "$TERRAGRUNT_VERSION" != "" ]; then
    install_terragrunt
else
    echo "TERRAGRUNT_VERSION not set, skipping Terragrunt installation"
fi

# Install OpenTofu if version is specified
if [ -n "$OPENTOFU_VERSION" ] && [ "$OPENTOFU_VERSION" != "" ]; then
    install_opentofu
else
    echo "OPENTOFU_VERSION not set, skipping OpenTofu installation"
fi

# Install Terraform if version is specified (and OpenTofu is not being installed)
if [ -n "$TERRAFORM_VERSION" ] && [ "$TERRAFORM_VERSION" != "" ]; then
    if [ -z "$OPENTOFU_VERSION" ] || [ "$OPENTOFU_VERSION" = "" ]; then
        install_terraform
    else
        echo "Both OpenTofu and Terraform versions specified, OpenTofu takes precedence"
    fi
else
    echo "TERRAFORM_VERSION not set, skipping Terraform installation"
fi

# Verify at least one tool is available if needed
echo "Available tools:"
command -v terragrunt &> /dev/null && echo "  - Terragrunt: $(terragrunt --version 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
command -v tofu &> /dev/null && echo "  - OpenTofu: $(tofu version 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
command -v terraform &> /dev/null && echo "  - Terraform: $(terraform version 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"

# Execute the terragrunt-runner
exec /usr/local/bin/terragrunt-runner "$@"
