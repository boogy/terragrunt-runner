# Terragrunt Runner GitHub Action

[![Go](https://img.shields.io/badge/Go-1.25-blue.svg)](https://golang.org/) [![Terragrunt](https://img.shields.io/badge/Terragrunt-0.88%2B-purple.svg)](https://terragrunt.gruntwork.io/) [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A GitHub Action to execute Terragrunt commands across multiple folders, optionally auto-detecting changed modules, and posting **formatted results** (including resource change summaries and plan diffs) as comments on Pull Requests. Supports both single-module and multi-module (`run --all`) executions, with parallel processing for efficiency.

This action is ideal for Infrastructure as Code (IaC) workflows in monorepos, ensuring secure plan previews, automated reviews, and compliance checks in PRs.

---

## Features

- **Tools Installation**: Install `Terraform`/`OpenTofu` and `Terragrunt`.
- **Auto-Detection of Changed Modules**: Walks up directories from changed files to find `terragrunt.hcl` files, limiting runs to impacted modules.
- **Multi-Module Support**: Uses `run --all -- <terraform command>` for Terragrunt's built-in parallelism.
- **Per-Folder Execution**: Run commands independently per folder, with optional Go-based parallelism.
- **PR Comment Posting**: Posts detailed outputs with collapsible sections for large plans. Supports **Terraform and OpenTofu** outputs. Splits comments if exceeding GitHub limits (65k chars).
- **Resource Change Parsing**: Extracts add/change/destroy/replace counts from plan outputs for summaries and warnings.
- **Preserves Color in Console, Sanitizes for Comments**: CLI output keeps colors; comments remove ANSI codes but preserve spacing and empty lines.
- **Cleanup Old Comments**: Deletes previous bot comments to keep PRs tidy.
- **Output Variables**: Sets GitHub Action outputs for success and total resource changes, usable in downstream steps.
- **Security-Focused**: Sanitizes arguments, validates folders/inputs, and runs in non-interactive mode by default.
- **Limits and Safeguards**: Configurable max runs/parallelism to prevent abuse or unexpected costs.

---

## Prerequisites

- Terragrunt v0.88+ installed in your workflow runner.
- `Terraform`/`OpenTofu` and `Terragrunt` installed.
- GitHub Token with `issues:write` and `pull-requests:write` scopes for commenting.

---

## Inputs

All inputs correspond to CLI flags. Defaults are shown below.

| Input                 | Description                                                                                       | Required | Default                             |
| --------------------- | ------------------------------------------------------------------------------------------------- | -------- | ----------------------------------- |
| `github-token`        | GitHub token for API access (uses `${{ github.token }}` by default).                              | No       | `${{ github.token }}`               |
| `repository`          | GitHub repository (owner/repo). Uses `${{ github.repository }}`.                                  | No       | `${{ github.repository }}`          |
| `pull-request`        | Pull request number. Auto-detected from `${{ github.ref }}`.                                      | No       | Auto-detected                       |
| `folders`             | Comma, space, or newline separated folders to run Terragrunt in.                                  | No       | [] (requires auto-detect or manual) |
| `command`             | Terragrunt command (e.g., `plan`, `apply`, `run --all plan`).                                     | No       | `plan`                              |
| `root-dir`            | Root directory for `run --all` commands. Used as working directory and shown in PR comments.      | No       | `live`                              |
| `args`                | Additional Terragrunt args (e.g., `--terragrunt-config custom.hcl`). Sanitized for security.      | No       | `--non-interactive`                 |
| `parallel`            | Enable parallel execution for per-folder runs.                                                    | No       | `true`                              |
| `max-parallel`        | Max concurrent executions (0 = unlimited). Applies to per-folder or Terragrunt's `--parallelism`. | No       | `5`                                 |
| `delete-old-comments` | Delete previous bot comments on the PR.                                                           | No       | `true`                              |
| `auto-detect`         | Auto-detect folders from changed files.                                                           | No       | `false`                             |
| `file-patterns`       | File patterns for auto-detection (comma-separated, e.g., `*.hcl,*.json`).                         | No       | `*.hcl,*.json,*.yaml,*.yml`         |
| `terragrunt-file`     | Terragrunt config file to search for (e.g., `terragrunt.hcl`).                                    | No       | `terragrunt.hcl`                    |
| `changed-files`       | Comma-separated changed files (for auto-detect; auto-fetches from git if empty).                  | No       | [] (fetches from `git diff HEAD~1`) |
| `max-walk-up`         | Max directory levels to walk up for Terragrunt file.                                              | No       | `3`                                 |
| `max-runs`            | Max Terragrunt executions allowed (0 = unlimited). Prevents excessive runs.                       | No       | `20`                                |
| `terragrunt-version`  | Version of Terragrunt to install                                                                  | No       |
| `opentofu-version`    | Version of OpenTofu to install                                                                    | No       |
| `terraform-version`   | Version of Terraform to install                                                                   | No       |

> [!NOTE]
> `Terraform`/`OpenTofu` and `Terragrunt` can be installed separately or by the action itself.

---

## Outputs

| Output                       | Description                                       |
| ---------------------------- | ------------------------------------------------- |
| `success`                    | `true` if all executions succeeded, else `false`. |
| `total-resources-to-add`     | Total resources to add across all runs.           |
| `total-resources-to-change`  | Total resources to change.                        |
| `total-resources-to-destroy` | Total resources to destroy.                       |
| `total-resources-to-replace` | Total resources to replace.                       |

> **Warnings are emitted for high destruction (>10) or large changes (>50 total).**

---

## Usage

### Basic Workflow Example

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
        uses: actions/checkout@v5
        with:
          fetch-depth: 2 # Needed for git diff

      - name: Setup Terragrunt
        uses: gruntwork-io/terragrunt-action@v3

      - name: Run Terragrunt Runner
        uses: boogy/terragrunt-runner@v1
        with:
          command: plan
          auto-detect: true
          max-walk-up: 3
          github-token: ${{ github.token }}
```

- Auto-detects changed Terragrunt modules.
- Runs terragrunt plan per folder in parallel (up to 5).
- Posts formatted outputs, summaries, and plan changes to the PR.

## Multi-Module with run --all plan

```yaml
- name: Run Terragrunt Runner
  uses: boogy/terragrunt-runner@v1
  with:
    command: run --all plan
    root-dir: live/accounts
    folders: |
      live/accounts/account1/baseline
      live/accounts/account2/baseline
    max-parallel: 10
    args: --non-interactive
```

- Runs a single Terragrunt command across all specified modules.
- Posts one PR comment with overall summary (root-dir and total changes).
- Summary table shows individual folder breakdown.
- Preserves color in console; removes ANSI codes in PR comments.
- Individual folder results shown only in summary table, not as separate comments.

## Specifying Folders Manually

```yaml
- name: Run Terragrunt Runner
  uses: boogy/terragrunt-runner@v1
  with:
    folders: |
      module/a
      module/b
    command: apply
    parallel: true
    max-parallel: 0
```

- Executes commands independently per folder.
- Uses Go goroutines for parallelism.


## Auto-Detection Explanation

- Fetches changed files via git diff --name-only HEAD~1 (or via changed-files input).
- Filters files matching file-patterns (e.g., `*.hcl`,`*.tf`).
- Walks up directories (up to `max-walk-up`) to find nearest terragrunt-file.
- Deduplicates folder paths.
- Limits total runs to max-runs to prevent excessive executions.
- Example:
  - `module/a/main.tf` changes → runs in `module/a` if `module/a/terragrunt.hcl` exists.
  - `module/b/resource/policy/base.json` changes → runs in `module/b/resource/` if `module/b/resource/terragrunt.hcl` exists.

## Security Considerations

- **Argument Sanitization**: Blocks shell injection patterns; only safe Terragrunt/Terraform flags allowed.
- **Folder Validation**: Prevents path traversal (.., absolute paths restricted).
- **Best Practices**: Use least-privilege tokens; add manual confirmations for `apply`.
- **Output Safety**: ANSI codes removed from PR comments; spacing preserved for readability.
