# Terragrunt Runner Action

A GitHub Action that executes Terragrunt commands across multiple folders and posts formatted results as comments on Pull Requests. Built with Go and optimized for speed with Docker.

## Features

- **Multi-folder execution**: Run Terragrunt in multiple directories sequentially or in parallel
- **Auto-detection**: Automatically find Terragrunt folders by walking up from changed files
- **Smart PR commenting**: Collapsible output sections with automatic splitting for large outputs
- **Resource change tracking**: Parses and summarizes Terraform/OpenTofu plan outputs
- **Clean output**: Shows only important Terraform/OpenTofu changes, filtering out noise
- **Comprehensive summary**: Generates a summary comment with resource changes across all folders
- **Comment management**: Automatically replaces old comments on re-runs to keep PR discussions clean
- **Execution limits**: Prevent runaway executions with configurable maximum runs
- **Error visualization**: Clear error indicators (❌) for failed executions
- **Support for run-all**: Execute `terragrunt run-all` commands with parallel support
- **Dynamic tool installation**: Installs Terragrunt, OpenTofu, or Terraform at runtime based on configuration
- **Flexible configuration**: Customize Terragrunt arguments and behavior
- **Choice of IaC tool**: Use either OpenTofu (default) or Terraform

## Table of Contents

- [Terragrunt Runner Action](#terragrunt-runner-action)
  - [Features](#features)
  - [Table of Contents](#table-of-contents)
  - [Usage](#usage)
    - [Basic Example](#basic-example)
    - [Advanced Example with Parallel Execution](#advanced-example-with-parallel-execution)
    - [Auto-Detection Mode](#auto-detection-mode)
    - [Hybrid Mode (Manual + Auto-Detection)](#hybrid-mode-manual--auto-detection)
    - [Apply on Merge](#apply-on-merge)
    - [Limiting Executions](#limiting-executions)
    - [Custom File Patterns](#custom-file-patterns)
    - [Multi-Environment Setup](#multi-environment-setup)
    - [With Changed Files Detection](#with-changed-files-detection)
    - [Advanced Changed Files Detection](#advanced-changed-files-detection)
    - [Tool Installation](#tool-installation)
      - [Using Pre-installed Tools](#using-pre-installed-tools)
    - [Using Terraform Instead of OpenTofu](#using-terraform-instead-of-opentofu)
    - [Full Feature Example](#full-feature-example)
  - [Inputs](#inputs)
  - [Outputs](#outputs)
  - [Using Action Outputs](#using-action-outputs)
  - [Security Notes](#security-notes)
    - [Best Practices](#best-practices)
    - [Required Permissions](#required-permissions)
    - [Security Features](#security-features)
  - [Comment Format](#comment-format)
    - [Individual Folder Comments](#individual-folder-comments)
  - [📊 Terragrunt Execution Summary](#-terragrunt-execution-summary)
    - [Results by Folder](#results-by-folder)
    - [Overall Statistics](#overall-statistics)
    - [Total Resource Changes](#total-resource-changes)
    - [Testing Locally](#testing-locally)
    - [Docker Development](#docker-development)
    - [Using the Makefile](#using-the-makefile)
  - [Troubleshooting](#troubleshooting)
    - [Common Issues](#common-issues)
      - [1. "Too many Terragrunt folders to process"](#1-too-many-terragrunt-folders-to-process)
      - [2. Comments Not Being Posted](#2-comments-not-being-posted)
      - [3. Auto-Detection Not Finding Folders](#3-auto-detection-not-finding-folders)
      - [4. Large Output Truncation](#4-large-output-truncation)
      - [5. Permission Denied Errors](#5-permission-denied-errors)
    - [Debug Mode](#debug-mode)
  - [Security Considerations](#security-considerations)
    - [GitHub Token](#github-token)
    - [Cloud Credentials](#cloud-credentials)
    - [State Files](#state-files)
    - [Sensitive Output](#sensitive-output)
    - [Best Practices](#best-practices-1)
  - [Requirements](#requirements)
  - [License](#license)
  - [Contributing](#contributing)
    - [Contribution Guidelines](#contribution-guidelines)
    - [Development Setup](#development-setup)
  - [Support](#support)
    - [Getting Help](#getting-help)

## Usage

### Basic Example

Simple setup for running Terragrunt plan on specified folders:

```yaml
name: Terragrunt Plan

on:
  pull_request:
    types: [opened, synchronize, reopened]

jobs:
  terragrunt:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Run Terragrunt
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          folders: |
            environments/dev
            environments/staging
            environments/prod
          command: plan
```

### Advanced Example with Parallel Execution

Run Terragrunt with parallel execution and custom arguments:

```yaml
name: Terragrunt Plan - Advanced

on:
  pull_request:
    types: [opened, synchronize, reopened]
    paths:
      - '**.hcl'
      - '**.tf'
      - '**.json'
      - '**.yaml'

jobs:
  terragrunt:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::123456789012:role/TerragruntRole
          aws-region: us-east-1

      - name: Run Terragrunt Plan
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          folders: |
            infrastructure/networking
            infrastructure/compute
            infrastructure/storage
          command: run-all plan
          terragrunt-args: |
            --terragrunt-non-interactive
            --terragrunt-include-external-dependencies
            --terragrunt-log-level info
          parallel: true
          delete-old-comments: true
          max-runs: 30
```

### Auto-Detection Mode

Automatically detect Terragrunt folders by walking up from changed files:

```yaml
name: Terragrunt Auto-Detect

on:
  pull_request:
    types: [opened, synchronize, reopened]

jobs:
  terragrunt:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0  # Required for detecting changed files

      - name: Get changed files
        id: changed-files
        uses: tj-actions/changed-files@v41
        with:
          separator: ','

      - name: Run Terragrunt with Auto-Detection
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          auto-detect: true
          changed-files: ${{ steps.changed-files.outputs.all_changed_files }}
          file-patterns: '*.tf,*.hcl,*.json,*.yaml,*.yml'
          terragrunt-file: 'terragrunt.hcl'
          max-walk-up: 5
          command: plan
```

### Hybrid Mode (Manual + Auto-Detection)

Combine manual folder specification with auto-detection:

```yaml
name: Hybrid Terragrunt Execution

on:
  pull_request:

jobs:
  terragrunt:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Run Terragrunt
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          folders: |
            critical/infrastructure
            core/networking
          auto-detect: true
          file-patterns: '*.tf,*.json,*.yaml'
          max-walk-up: 3
          command: plan
```

In this example:
- The action will always run in `critical/infrastructure` and `core/networking`
- It will also auto-detect folders from changed files
- If a file like `modules/vpc/policies/bucket-policy.json` is changed, it will walk up to find `modules/vpc/terragrunt.hcl`
- Maximum 3 directories will be walked up before giving up

### Apply on Merge

Automatically apply changes when PRs are merged to main:

```yaml
name: Terragrunt Apply

on:
  push:
    branches:
      - main

jobs:
  apply:
    runs-on: ubuntu-latest
    environment: production
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::123456789012:role/TerragruntRole
          aws-region: us-east-1

      - name: Run Terragrunt Apply
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          folders: |
            environments/prod
          command: run-all apply
          terragrunt-args: |
            --terragrunt-non-interactive
            --terragrunt-auto-approve
          parallel: true
```

### Limiting Executions

Prevent runaway executions when too many files change:

```yaml
name: Terragrunt with Run Limits

on:
  pull_request:

jobs:
  terragrunt:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Run Terragrunt with Limits
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          auto-detect: true
          max-runs: 10  # Fail if more than 10 folders detected
          command: plan
```

To disable the limit entirely:

```yaml
      - name: Run Terragrunt with No Limits
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          auto-detect: true
          max-runs: 0  # 0 = unlimited
          command: plan
```

### Custom File Patterns

Track specific file types for auto-detection:

```yaml
name: Terragrunt with Custom Patterns

on:
  pull_request:

jobs:
  terragrunt:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Run Terragrunt for Policy Changes
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          auto-detect: true
          file-patterns: '*.json,*.yaml,policy-*.tf'
          terragrunt-file: 'terragrunt.hcl'
          max-walk-up: 7
          command: plan
```

### Multi-Environment Setup

Run different commands for different environments:

```yaml
name: Multi-Environment Terragrunt

on:
  pull_request:

jobs:
  dev-staging:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        environment: [dev, staging]
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::${{ secrets.AWS_ACCOUNT_ID }}:role/TerragruntRole-${{ matrix.environment }}
          aws-region: us-east-1

      - name: Run Terragrunt Plan
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          folders: environments/${{ matrix.environment }}
          command: plan
          terragrunt-args: --terragrunt-non-interactive

  production:
    runs-on: ubuntu-latest
    if: github.base_ref == 'main'
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::${{ secrets.PROD_AWS_ACCOUNT_ID }}:role/TerragruntRole-prod
          aws-region: us-east-1

      - name: Run Terragrunt Plan
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          folders: environments/prod
          command: plan
          terragrunt-args: |
            --terragrunt-non-interactive
            --terragrunt-strict-include
          max-runs: 5  # More restrictive for production
```

### With Changed Files Detection

Detect changes and run only on affected modules:

```yaml
name: Smart Terragrunt Execution

on:
  pull_request:

jobs:
  detect-changes:
    runs-on: ubuntu-latest
    outputs:
      changed-files: ${{ steps.changed-files.outputs.all_changed_files }}
    steps:
      - uses: actions/checkout@v4

      - name: Get changed files
        id: changed-files
        uses: tj-actions/changed-files@v45
        with:
          files: |
            **/*.tf
            **/*.hcl
            **/*.json
            **/*.yaml
            **/*.yml

  terragrunt:
    needs: detect-changes
    if: ${{ needs.detect-changes.outputs.changed-files != '' }}
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Run Terragrunt with Auto-Detection
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          auto-detect: true
          changed-files: ${{ needs.detect-changes.outputs.changed-files }}
          terragrunt-file: terragrunt.hcl
          max-walk-up: 5
          command: plan
```

### Advanced Changed Files Detection

Detect changes per module and run in parallel:

```yaml
name: Module-Specific Terragrunt

on:
  pull_request:

jobs:
  detect-changes:
    runs-on: ubuntu-latest
    outputs:
      networking: ${{ steps.changed-files-networking.outputs.any_changed }}
      compute: ${{ steps.changed-files-compute.outputs.any_changed }}
      storage: ${{ steps.changed-files-storage.outputs.any_changed }}
    steps:
      - uses: actions/checkout@v4

      - name: Check networking changes
        id: changed-files-networking
        uses: tj-actions/changed-files@v45
        with:
          files: modules/networking/**

      - name: Check compute changes
        id: changed-files-compute
        uses: tj-actions/changed-files@v45
        with:
          files: modules/compute/**

      - name: Check storage changes
        id: changed-files-storage
        uses: tj-actions/changed-files@v45
        with:
          files: modules/storage/**

  terragrunt-networking:
    needs: detect-changes
    if: needs.detect-changes.outputs.networking == 'true'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          folders: modules/networking
          command: plan

  terragrunt-compute:
    needs: detect-changes
    if: needs.detect-changes.outputs.compute == 'true'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          folders: modules/compute
          command: plan

  terragrunt-storage:
    needs: detect-changes
    if: needs.detect-changes.outputs.storage == 'true'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          folders: modules/storage
          command: plan
```

### Tool Installation

The action only installs tools when their versions are explicitly specified:

```yaml
name: With Tool Installation

on:
  pull_request:

jobs:
  terragrunt:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Run with specific tool versions
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          folders: environments/dev
          terragrunt-version: '0.55.1'  # Will install Terragrunt
          opentofu-version: '1.6.2'     # Will install OpenTofu
          # terraform-version not set, so Terraform won't be installed
```

#### Using Pre-installed Tools

If your runner already has the tools installed, leave the version inputs empty:

```yaml
      - name: Setup Terragrunt
        uses: autero1/setup-terragrunt@v3
        with:
          terragrunt_version: 0.55.1

      - name: Setup OpenTofu
        uses: opentofu/setup-opentofu@v1
        with:
          tofu_version: 1.6.2

      - name: Run with pre-installed tools
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          folders: environments/dev
          # No tool versions specified - will use pre-installed versions
```

### Using Terraform Instead of OpenTofu

To use Terraform, specify `terraform-version` and leave `opentofu-version` empty:

```yaml
name: Terragrunt with Terraform

on:
  pull_request:

jobs:
  terragrunt:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Run Terragrunt with Terraform
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          folders: |
            environments/dev
          command: plan
          terragrunt-version: '0.55.1'  # Install Terragrunt
          terraform-version: '1.7.0'     # Install Terraform
          # opentofu-version not set, so OpenTofu won't be installed
```

### Full Feature Example

Complete example showcasing all features:

```yaml
name: Complete Terragrunt Pipeline

on:
  pull_request:
    types: [opened, synchronize, reopened]

env:
  TF_LOG: INFO
  AWS_REGION: us-east-1

jobs:
  terragrunt:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
      id-token: write  # For OIDC
    steps:
      - name: Checkout code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ secrets.AWS_ROLE_ARN }}
          aws-region: ${{ env.AWS_REGION }}
          role-session-name: GitHubActions-${{ github.run_id }}

      - name: Get changed files
        id: changed-files
        uses: tj-actions/changed-files@v41
        with:
          separator: ','
          files: |
            **.tf
            **.hcl
            **.json
            **.yaml
            **.yml

      - name: Run Terragrunt
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}

          # Folder **configuration**
          folders: |
            core/infrastructure
          auto-detect: true
          changed-files: ${{ steps.changed-files.outputs.all_changed_files }}

          # Command configuration
          command: plan
          terragrunt-args: |
            --terragrunt-non-interactive
            --terragrunt-log-level info
            --terragrunt-include-external-dependencies
          parallel: false

          # Detection configuration
          file-patterns: '*.tf,*.hcl,*.json,*.yaml,*.yml'
          terragrunt-file: 'terragrunt.hcl'
          max-walk-up: 5

          # Limits and behavior
          max-runs: 20
          delete-old-comments: true

          # Version configuration
          terragrunt-version: '0.55.1'
          opentofu-version: '1.6.2'
```

## Inputs

| Input                 | Description                                                                                        | Required | Default                          | Example                              |
| --------------------- | -------------------------------------------------------------------------------------------------- | -------- | -------------------------------- | ------------------------------------ |
| `github-token`        | GitHub token for API access. Needs `pull-requests: write` permission                               | **Yes**  | -                                | `${{ secrets.GITHUB_TOKEN }}`        |
| `folders`             | Comma or newline separated list of folders to run Terragrunt in. Can be relative or absolute paths | No*      | -                                | `environments/dev,environments/prod` |
| `command`             | Terragrunt command to execute. Supports all Terragrunt commands including `run-all` variants       | No       | `plan`                           | `apply`, `init`, `run-all plan`      |
| `terragrunt-args`     | Additional arguments to pass to Terragrunt. Can be multi-line for multiple args                    | No       | `--terragrunt-non-interactive`   | See examples above                   |
| `parallel`            | Execute `run-all` commands in parallel. Only applies to `run-all` commands                         | No       | `false`                          | `true`                               |
| `delete-old-comments` | Delete previous bot comments before posting new ones. Keeps PR discussions clean                   | No       | `true`                           | `false`                              |
| `auto-detect`         | Auto-detect Terragrunt folders by walking up from changed files                                    | No       | `false`                          | `true`                               |
| `file-patterns`       | File patterns to track for auto-detection (comma-separated globs)                                  | No       | `*.tf,*.hcl,*.json,*.yaml,*.yml` | `*.json,policy-*.tf`                 |
| `terragrunt-file`     | Name of the Terragrunt file to look for when walking up directories                                | No       | `terragrunt.hcl`                 | `root.hcl`                           |
| `changed-files`       | List of changed files for auto-detection (comma-separated). If not provided, uses git diff         | No       | Auto-detected from git           | See examples                         |
| `max-walk-up`         | Maximum directory levels to walk up when searching for Terragrunt file                             | No       | `3`                              | `5`                                  |
| `max-runs`            | Maximum number of Terragrunt executions allowed. Set to 0 for unlimited                            | No       | `20`                             | `10`, `0`                            |
| `terragrunt-version`  | Terragrunt version to install (leave empty to use pre-installed)                                   | No       | -                                | `0.55.1`                             |
| `opentofu-version`    | OpenTofu version to install (leave empty to skip)                                                  | No       | -                                | `1.6.2`                              |
| `terraform-version`   | Terraform version to install (only if opentofu-version is empty, leave empty to skip)              | No       | -                                | `1.7.0`                              |
| `working-directory`   | Working directory for the action                                                                   | No       | `.`                              | `infrastructure`                     |

*Note: `folders` is required unless `auto-detect` is enabled

## Outputs

| Output                       | Description                                                     |
| ---------------------------- | --------------------------------------------------------------- |
| `success`                    | Whether all Terragrunt executions succeeded (`true` or `false`) |
| `total-resources-to-add`     | Total number of resources to be added across all executions     |
| `total-resources-to-change`  | Total number of resources to be changed across all executions   |
| `total-resources-to-destroy` | Total number of resources to be destroyed across all executions |

## Using Action Outputs

You can use the action outputs in subsequent steps:

```yaml
jobs:
  terragrunt:
    runs-on: ubuntu-latest
    outputs:
      success: ${{ steps.run-terragrunt.outputs.success }}
      resources-to-add: ${{ steps.run-terragrunt.outputs.total-resources-to-add }}
      resources-to-change: ${{ steps.run-terragrunt.outputs.total-resources-to-change }}
      resources-to-destroy: ${{ steps.run-terragrunt.outputs.total-resources-to-destroy }}
    steps:
      - uses: actions/checkout@v4

      - id: run-terragrunt
        uses: boogy/terragrunt-runner@v1
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          folders: environments/dev
          command: plan

      - name: Check for destructive changes
        if: steps.run-terragrunt.outputs.total-resources-to-destroy > 0
        run: |
          echo "⚠️ WARNING: This plan will destroy ${{ steps.run-terragrunt.outputs.total-resources-to-destroy }} resources!"
          exit 1

  notify:
    needs: terragrunt
    if: always()
    runs-on: ubuntu-latest
    steps:
      - name: Send Slack notification
        uses: slack/send@v1
        with:
          status: ${{ needs.terragrunt.outputs.success == 'true' && 'Success' || 'Failed' }}
          message: |
            Terragrunt run completed:
            - Resources to add: ${{ needs.terragrunt.outputs.resources-to-add }}
            - Resources to change: ${{ needs.terragrunt.outputs.resources-to-change }}
            - Resources to destroy: ${{ needs.terragrunt.outputs.resources-to-destroy }}
```

## Security Notes

### Best Practices

1. **GitHub Token**: Always use `${{ secrets.GITHUB_TOKEN }}` instead of hardcoding tokens
2. **Permissions**: Use least-privilege principle for token permissions
3. **Input Validation**: The action validates all inputs to prevent injection attacks
4. **Folder Restrictions**: Absolute paths outside `/workspace` are blocked for security
5. **Command Sanitization**: Only approved Terragrunt commands are allowed

### Required Permissions

```yaml
permissions:
  contents: read        # To checkout code
  pull-requests: write  # To post comments
  id-token: write       # For OIDC authentication (if using AWS/GCP/Azure)
```

### Security Features

- ✅ Input sanitization prevents command injection
- ✅ Path traversal protection blocks `../` patterns
- ✅ Argument validation blocks shell metacharacters
- ✅ Runs as non-root user in Docker container
- ✅ No secrets are logged or exposed in comments
- ✅ Maximum execution limits prevent resource exhaustion

## Comment Format

### Individual Folder Comments

Each folder gets its own comment with:
- Clear status indicator (✅ success, ❌ failure)
- Terragrunt command executed
- Resource changes summary (additions, changes, deletions)
- Clean Terraform/OpenTofu output (filtered from Terragrunt noise)
- Automatic pagination for large outputs (shows "1/2", "2/2" etc.)

Example successful execution:
```markdown
## ✅ Terragrunt Execution: `environments/dev`
**Status:** Success
**Command:** `terragrunt plan`
**Changes:** +3 to add, ~2 to change, -1 to destroy

<details>
<summary><b>View Output</b></summary>

```hcl
# aws_instance.example will be created
+ resource "aws_instance" "example" {
    + ami           = "ami-0c55b159cbfafe1f0"
    + instance_type = "t2.micro"
    ...
}

Plan: 3 to add, 2 to change, 1 to destroy.
```

</details>
```

Example failed execution:
```markdown
## ❌ Terragrunt Execution: `environments/prod`
**Status:** Failed
**Command:** `terragrunt plan`

<details>
<summary><b>View Error Details</b></summary>

```hcl
Error: Invalid provider configuration
  on main.tf line 10:
  Provider "aws" requires region to be configured.
```

</details>
```

### Summary Comment

A comprehensive summary is posted after all executions:
```
## 📊 Terragrunt Execution Summary

**Command:** `terragrunt plan`
**Total Folders:** 3

### Results by Folder
| Folder                 | Status    | Resources to Add | Resources to Change | Resources to Destroy | Resources to Import | Resources to Move |
| ---------------------- | --------- | ---------------- | ------------------- | -------------------- | ------------------- | ----------------- |
| `environments/dev`     | ✅ Success | +3               | ~2                  | -1                   | 0                   | 0                 |
| `environments/staging` | ✅ Success | 0                | 0                   | 0                    | 0                   | 0                 |
| `environments/prod`    | ❌ Failed  | -                | -                   | -                    | -                   | -                 |

### Overall Statistics
- **Successful Executions:** 2/3
- **Failed Executions:** 1/3
- **Folders with No Changes:** 1/3

### Total Resource Changes
- **Resources to Add:** +3
- **Resources to Change:** ~2
- **Resources to Destroy:** -1
```

## Auto-Detection Behavior

The auto-detection feature walks up directory trees to find Terragrunt files:

1. **File Change Detection**: When files matching `file-patterns` are changed
2. **Directory Walk-Up**: Starting from the changed file's directory, walks up looking for `terragrunt.hcl`
3. **Stop Conditions**:
   - Finds a `terragrunt.hcl` file
   - Reaches `max-walk-up` levels
   - Reaches the repository root
4. **Deduplication**: Multiple changes in the same Terragrunt folder are processed only once

### Example Scenarios

Given this directory structure:
```
infrastructure/
├── environments/
│   ├── dev/
│   │   ├── terragrunt.hcl
│   │   ├── main.tf
│   │   └── policies/
│   │       └── s3-policy.json
│   └── prod/
│       ├── terragrunt.hcl
│       └── config/
│           └── app-config.yaml
```

- Change to `infrastructure/environments/dev/policies/s3-policy.json` → Runs in `infrastructure/environments/dev/`
- Change to `infrastructure/environments/prod/config/app-config.yaml` → Runs in `infrastructure/environments/prod/`
- Change to both files → Runs in both `dev/` and `prod/` folders (deduplicated)

## Execution Limits

The `max-runs` parameter prevents excessive executions:

- **Default**: 20 executions maximum
- **Warning**: Logs warning when approaching 80% of limit
- **Error**: Fails with clear message when limit exceeded
- **Disable**: Set to `0` for unlimited executions
- **Use Cases**:
  - Prevent CI cost overruns
  - Avoid long-running jobs
  - Protect against misconfiguration

## Development

### Building Locally

```bash
# Install dependencies
go mod download

# Build the binary
go build -o terragrunt-runner main.go

# Build the Docker image
docker build -t terragrunt-runner:latest .
```

### Testing Locally

```bash
# Run with local folders
./terragrunt-runner \
  --github-token="$GITHUB_TOKEN" \
  --repository="owner/repo" \
  --pull-request=123 \
  --folders="./infrastructure/dev,./infrastructure/prod" \
  --command="plan"

# Test auto-detection
./terragrunt-runner \
  --github-token="$GITHUB_TOKEN" \
  --repository="owner/repo" \
  --pull-request=123 \
  --auto-detect \
  --changed-files="modules/vpc/policies/policy.json" \
  --max-walk-up=5
```

### Docker Development

```bash
# Build the image
docker build -t terragrunt-runner:dev .

# Run the container
docker run --rm \
  -v $(pwd):/workspace \
  -e GITHUB_TOKEN="$GITHUB_TOKEN" \
  -e GITHUB_REPOSITORY="owner/repo" \
  -e GITHUB_PR_NUMBER="123" \
  terragrunt-runner:dev \
  --folders="/workspace/infrastructure/dev" \
  --command="plan"
```

### Using the Makefile

```bash
# Show available commands
make help

# Build the binary
make build

# Run tests
make test

# Generate test coverage
make coverage

# Build Docker image
make docker-build

# Run linters
make lint

# Format code
make fmt
```

## Troubleshooting

### Common Issues

#### 1. "Too many Terragrunt folders to process"

**Problem**: More folders detected than `max-runs` allows.

**Solutions**:
- Increase `max-runs` to a higher value
- Set `max-runs: 0` to disable the limit
- Use more specific `file-patterns` to reduce matches
- Manually specify folders instead of using auto-detect

#### 2. Comments Not Being Posted

**Problem**: Comments don't appear on the PR.

**Checks**:
- Ensure `github-token` has `pull-requests: write` permission
- Verify PR number is correctly detected
- Check action logs for API errors
- Ensure repository format is correct (`owner/repo`)

#### 3. Auto-Detection Not Finding Folders

**Problem**: Changed files don't trigger Terragrunt runs.

**Solutions**:
- Verify `file-patterns` match your file types
- Check `max-walk-up` is sufficient for your directory depth
- Ensure `terragrunt-file` matches your actual filename
- Use `--changed-files` to explicitly provide file list

#### 4. Large Output Truncation

**Problem**: Terraform output is cut off.

**Notes**:
- Comments are automatically split when exceeding GitHub's 65KB limit
- Each split shows pagination (e.g., "1/3", "2/3", "3/3")
- Check all comment parts for complete output

#### 5. Permission Denied Errors

**Problem**: Action fails with permission errors.

**Solutions**:
- Ensure AWS/Azure/GCP credentials are properly configured
- Check Terragrunt backend permissions
- Verify state file access permissions
- Ensure git repository is properly checked out

### Debug Mode

Enable debug logging by setting environment variables:

```yaml
env:
  DEBUG: 'true'
  TF_LOG: 'DEBUG'
  TERRAGRUNT_LOG_LEVEL: 'debug'
```

## Security Considerations

### GitHub Token
- Use the default `GITHUB_TOKEN` when possible (automatically scoped)
- For cross-repository access, use a PAT with minimal permissions
- Never commit tokens directly in workflows

### Cloud Credentials
- Use OIDC authentication instead of long-lived credentials
- Implement role assumption with session names
- Use environment-specific roles with least privilege

### State Files
- Ensure backend configuration uses encryption
- Implement state locking to prevent concurrent modifications
- Use separate state files for different environments
- Enable versioning on state storage

### Sensitive Output
- The action filters Terraform output but review what's posted
- Use `sensitive = true` in Terraform for sensitive variables
- Consider using separate workflows for sensitive environments
- Implement PR approval requirements for production

### Best Practices
- Always use `--terragrunt-non-interactive` in CI/CD
- Implement branch protection rules
- Use environment protection rules for production
- Enable audit logging for compliance
- Regularly update Terragrunt and OpenTofu versions

## Requirements

- **GitHub**: Repository with Pull Requests enabled
- **Terragrunt**: Configuration files in specified folders
- **Provider Credentials**: AWS, Azure, GCP, or other provider credentials
- **Permissions**: GitHub token with `pull-requests: write` permission
- **Backend**: Configured Terraform/OpenTofu backend for state management

## License

MIT

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

### Contribution Guidelines

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Development Setup

1. Install Go 1.22 or later
2. Install Docker for testing
3. Run `make deps` to install dependencies
4. Run `make test` to ensure tests pass
5. Run `make lint` before committing

## Support

For issues and feature requests, please create an issue in the repository.

### Getting Help

- Check the [Troubleshooting](#troubleshooting) section
- Review existing [GitHub Issues](https://github.com/boogy/terragrunt-runner/issues)
- Create a new issue with:
  - Action version
  - Workflow configuration
  - Error messages
  - Expected vs actual behavior
