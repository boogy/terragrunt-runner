# Terragrunt Runner GitHub Action

[![Go](https://img.shields.io/badge/Go-1.25-blue.svg)](https://golang.org/) [![Terragrunt](https://img.shields.io/badge/Terragrunt-0.88%2B-purple.svg)](https://terragrunt.gruntwork.io/) [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A GitHub Action to execute Terragrunt commands across multiple folders, optionally auto-detecting changed modules, and posting formatted results (including resource change summaries) as comments on Pull Requests. Supports both single-module and multi-module (`run --all`) executions, with parallel processing for efficiency.

This action is ideal for Infrastructure as Code (IaC) workflows in monorepos, ensuring security reviews, plan previews, and compliance checks are automated in PRs.

## Features

- **Auto-Detection of Changed Modules**: Walks up directories from changed files to find `terragrunt.hcl` files, limiting runs to impacted modules.
- **Multi-Module Support**: Use `run --all -- <terraform command>` for Terragrunt's built-in parallelism across specified folders.
- **Per-Folder Execution**: Run commands independently per folder, with optional Go-based parallelism.
- **PR Comment Posting**: Posts detailed outputs (with collapsible sections for large plans) and a summary table to the PR. Splits large comments to respect GitHub limits (65k chars).
- **Resource Change Parsing**: Extracts add/change/destroy/replace counts from Terraform/OpenTofu outputs for summaries and warnings (e.g., high destruction risk).
- **Cleanup Old Comments**: Deletes previous bot comments to keep PRs clean.
- **Output Variables**: Sets GitHub Action outputs for success status and total resource changes, usable in downstream steps.
- **Security-Focused**: Sanitizes arguments to prevent injection, validates folders/inputs, and supports non-interactive mode by default.
- **Limits and Safeguards**: Configurable max runs/parallelism to prevent abuse or high costs.

## Prerequisites

- Terragrunt v0.88+ installed in your workflow runner (e.g., via `gruntwork-io/terragrunt-action`).
- Terraform/OpenTofu installed if running plans/applies.
- GitHub Token with `issues:write` and `pull-requests:write` scopes for commenting.
- When used with AWS authentication the `id-token:write` scope must be set.

## Inputs

All inputs correspond to CLI flags. Defaults are shown below.

| Input                 | Description                                                                                       | Required | Default                             |
| --------------------- | ------------------------------------------------------------------------------------------------- | -------- | ----------------------------------- |
| `github-token`        | GitHub token for API access (uses `${{ github.token }}` by default).                              | No       | `${{ github.token }}`               |
| `repository`          | GitHub repository (owner/repo). Uses `${{ github.repository }}`.                                  | No       | `${{ github.repository }}`          |
| `pull-request`        | Pull request number. Auto-detected from `${{ github.ref }}`.                                      | No       | Auto-detected                       |
| `folders`             | Comma, space, or newline separated folders to run Terragrunt in (e.g., `module1,module2`).        | No       | [] (requires auto-detect or manual) |
| `command`             | Terragrunt command (e.g., `plan`, `run --all -- plan -no-color -var=foo`).                        | No       | `plan`                              |
| `args`                | Additional Terragrunt args (e.g., `--terragrunt-config custom.hcl`). Sanitized for security.      | No       | `--non-interactive`                 |
| `parallel`            | Enable parallel execution for per-folder runs.                                                    | No       | `false`                             |
| `max-parallel`        | Max concurrent executions (0 = unlimited). Applies to per-folder or Terragrunt's `--parallelism`. | No       | `5`                                 |
| `delete-old-comments` | Delete previous bot comments on the PR.                                                           | No       | `true`                              |
| `auto-detect`         | Auto-detect folders from changed files.                                                           | No       | `false`                             |
| `file-patterns`       | File patterns for auto-detection (comma-separated, e.g., `*.hcl,*.json`).                         | No       | `*.hcl,*.json,*.yaml,*.yml`         |
| `terragrunt-file`     | Terragrunt config file to search for (e.g., `terragrunt.hcl`).                                    | No       | `terragrunt.hcl`                    |
| `changed-files`       | Comma-separated changed files (for auto-detect; auto-fetches from git if empty).                  | No       | [] (fetches from `git diff HEAD~1`) |
| `max-walk-up`         | Max directory levels to walk up for Terragrunt file.                                              | No       | `3`                                 |
| `max-runs`            | Max Terragrunt executions allowed (0 = unlimited). Prevents excessive runs.                       | No       | `20`                                |

## Outputs

| Output                       | Description                                       |
| ---------------------------- | ------------------------------------------------- |
| `success`                    | `true` if all executions succeeded, else `false`. |
| `total-resources-to-add`     | Total resources to add across all runs.           |
| `total-resources-to-change`  | Total resources to change.                        |
| `total-resources-to-destroy` | Total resources to destroy.                       |
| `total-resources-to-replace` | Total resources to replace.                       |

Warnings are emitted for high destruction (>10) or large changes (>50 total).

## Usage

### Basic Workflow Example

Add this to your `.github/workflows/terragrunt-plan.yml`:

```yaml
name: Terragrunt Plan on PR

on:
  pull_request:
    branches: [main]

permissions:
  contents: read
  pull-requests: write

jobs:
  terragrunt-plan:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
        with:
          fetch-depth: 2  # Needed for git diff

      - name: Setup Terragrunt
        uses: gruntwork-io/terragrunt-action@v2  # Or your preferred setup

      - name: Run Terragrunt Runner
        uses: boogy/terragrunt-runner@v1  # Replace with your action repo/tag
        with:
          command: plan
          auto-detect: true
          max-walk-up: 3
          github-token: ${{ github.token }}
```

This auto-detects changed Terragrunt modules, runs `terragrunt plan` per folder in parallel (up to 5), and posts outputs/summary to the PR.

### Multi-Module with `run --all plan`

For Terragrunt's built-in parallelism across modules:

```yaml
- name: Run Terragrunt Runner
  uses: boogy/terragrunt-runner@v1
  with:
    command: run --all -- plan -no-color -var=foo
    auto-detect: true
    max-parallel: 10  # Uses Terragrunt's --parallelism
    args: --non-interactive -lock=false
```

- Runs a single `terragrunt run --all --non-interactive -lock=false --parallelism 10 --queue-include-dir <folder1> --queue-include-dir <folder2> ... -- plan -no-color -var=foo`.
- Parses combined output into per-module comments.
- If output splitting fails (rare), falls back to a single "." folder result.

### Specifying Folders Manually

```yaml
- name: Run Terragrunt Runner
  uses: boogy/terragrunt-runner@v1
  with:
    folders: |
      module/a
      module/b
    command: apply
    parallel: true
    max-parallel: 0  # Unlimited parallelism in Go
```

- Executes `terragrunt apply` independently in each folder.
- Uses Go goroutines for parallelism.

### Auto-Detection Explanation

When `auto-detect: true`:
- Fetches changed files via `git diff --name-only HEAD~1` (or the provided `changed-files` input for reliability in shallow checkouts).
- Filters files matching `file-patterns` (e.g., `*.tf,*.hcl`).
- For each matching file, walks up to `max-walk-up` levels to find the nearest directory containing `terragrunt-file` (e.g., `terragrunt.hcl`).
- Deduplicates and cleans folder paths.
- Combines detected folders with any manual `folders` input.
- Errors if the total exceeds `max-runs` (prevents excessive executions).

**Security Note**: This limits runs to impacted modules, minimizing credential exposure and attack surface. Always validate `changed-files` sources (e.g., from trusted actions like `tj-actions/changed-files`) to avoid path injection. For high-security workflows, set low `max-runs` and monitor for anomalous PR changes.

Example: If `module/a/main.tf` changes, and `module/a/terragrunt.hcl` exists, runs in `module/a`.

### Security Considerations

- **Argument Sanitization**: Blocks shell injection patterns (e.g., `;`, `&&`); allows only safe Terragrunt/Terraform flags.
- **Folder Validation**: Prevents path traversal (no `..`, absolute paths restricted).
- **Best Practices**: Use least-privilege tokens. For applies, add confirmation steps. Integrate with security scanners (e.g., tfsec) in workflows.
- **Potential Improvements**: Monitor for large plans that could expose sensitive data; use encrypted variables for secrets.

### Troubleshooting

- **No Folders Detected**: Enable `auto-detect` or specify `folders`. Check git diff works (needs `fetch-depth: 2`).
- **Command Failures**: Ensure Terragrunt/Terraform installed. Use `DEBUG=true` env for verbose logs.
- **Large Outputs**: Automatically splits into multi-part comments.
- **Old Syntax**: Supports legacy `run-all` by rewriting to `run --all`.

For issues, open a GitHub issue with logs.
