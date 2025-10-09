package main

import (
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestParseFolders(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "comma separated",
			input:    "folder1,folder2,folder3",
			expected: []string{"folder1", "folder2", "folder3"},
		},
		{
			name:     "space separated",
			input:    "folder1 folder2 folder3",
			expected: []string{"folder1", "folder2", "folder3"},
		},
		{
			name:     "mixed comma and space",
			input:    "folder1, folder2 ,folder3",
			expected: []string{"folder1", "folder2", "folder3"},
		},
		{
			name:     "newlines",
			input:    "folder1\nfolder2\nfolder3",
			expected: []string{"folder1", "folder2", "folder3"},
		},
		{
			name:     "empty",
			input:    "",
			expected: []string{},
		},
		{
			name:     "only whitespace",
			input:    "  , \n ",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFolders(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("parseFolders() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestUniqueFolders(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "duplicates",
			input:    []string{"folder1", "folder2", "folder1", "folder3"},
			expected: []string{"folder1", "folder2", "folder3"},
		},
		{
			name:     "with unclean paths",
			input:    []string{"folder1/", "folder1", "folder2/..//folder2"},
			expected: []string{"folder1", "folder2"},
		},
		{
			name:     "empty",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "single",
			input:    []string{"folder1"},
			expected: []string{"folder1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uniqueFolders(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("uniqueFolders() length = %d, want %d", len(got), len(tt.expected))
			}
			seen := make(map[string]bool)
			for _, g := range got {
				seen[g] = true
			}
			for _, exp := range tt.expected {
				if !seen[exp] {
					t.Errorf("uniqueFolders() missing %s", exp)
				}
			}
		})
	}
}

func TestUniqueStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "duplicates",
			input:    []string{"a", "b", "a", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "empty",
			input:    []string{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uniqueStrings(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("uniqueStrings() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSanitizeArgs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:  "valid args",
			input: "--non-interactive -var=foo -lock=false",
			want:  []string{"--non-interactive", "-var=foo", "-lock=false"},
		},
		{
			name:    "forbidden patterns",
			input:   "--non-interactive ; rm -rf /",
			wantErr: true,
		},
		{
			name:    "mixed",
			input:   "safe-arg $(danger)",
			wantErr: true,
		},
		{
			name:  "empty",
			input: "",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeArgs(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("sanitizeArgs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("sanitizeArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseResourceChanges(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *ResourceChanges
	}{
		{
			name:     "basic plan - add only",
			input:    `Plan: 1 to add, 0 to change, 0 to destroy.`,
			expected: &ResourceChanges{ToAdd: 1},
		},
		{
			name:     "basic plan - change only",
			input:    `Plan: 0 to add, 1 to change, 0 to destroy.`,
			expected: &ResourceChanges{ToChange: 1},
		},
		{
			name:     "basic plan - destroy only",
			input:    `Plan: 0 to add, 0 to change, 1 to destroy.`,
			expected: &ResourceChanges{ToDestroy: 1},
		},
		{
			name:     "no changes",
			input:    `No changes`,
			expected: &ResourceChanges{NoChanges: true},
		},
		{
			name:     "complex plan",
			input:    `Plan: 2 to add, 3 to change, 1 to destroy.`,
			expected: &ResourceChanges{ToAdd: 2, ToChange: 3, ToDestroy: 1},
		},
		{
			name:     "plan with commas",
			input:    `Plan: 5 to add, 2 to change, 1 to destroy.`,
			expected: &ResourceChanges{ToAdd: 5, ToChange: 2, ToDestroy: 1},
		},
		{
			name:     "no plan line",
			input:    `Some other output without plan`,
			expected: &ResourceChanges{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseResourceChanges(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("parseResourceChanges() = %+v, want %+v", got, tt.expected)
			}
		})
	}
}

func TestExtractTerraformOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "basic plan",
			input: `
[terragrunt] Initializing
Terraform used the selected providers to generate the following execution plan. Resource actions are indicated with the following symbols:
  + create

Terraform will perform the following actions:

  # resource.example will be created
  + resource "resource" "example" {
      + id = (known after apply)
    }

Plan: 1 to add, 0 to change, 0 to destroy.
`,
			expected: `Terraform used the selected providers to generate the following execution plan. Resource actions are indicated with the following symbols:
  + create

Terraform will perform the following actions:

  # resource.example will be created
  + resource "resource" "example" {
      + id = (known after apply)
    }

Plan: 1 to add, 0 to change, 0 to destroy.`,
		},
		{
			name: "with no changes",
			input: `
Refreshing state...
Acquiring state lock...
Terraform used the selected providers to generate the following execution plan.
No changes. Infrastructure is up-to-date.
`,
			expected: `No changes detected.`,
		},
		{
			name:     "error",
			input:    `Error: Invalid configuration`,
			expected: `Error: Invalid configuration`,
		},
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
		{
			name: "will perform actions trigger",
			input: `
Acquiring state lock...
Terraform will perform the following actions:

  # resource.example will be created
  + resource "resource" "example" {
      + id = (known after apply)
    }

Plan: 1 to add, 0 to change, 0 to destroy.
`,
			expected: `Terraform will perform the following actions:

  # resource.example will be created
  + resource "resource" "example" {
      + id = (known after apply)
    }

Plan: 1 to add, 0 to change, 0 to destroy.`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTerraformOutput(tt.input)
			if strings.TrimSpace(got) != strings.TrimSpace(tt.expected) {
				t.Errorf("extractTerraformOutput() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFormatResourceChanges(t *testing.T) {
	tests := []struct {
		name     string
		changes  *ResourceChanges
		expected string
	}{
		{
			name:     "all types",
			changes:  &ResourceChanges{ToAdd: 1, ToChange: 2, ToDestroy: 3, ToReplace: 4},
			expected: "**Changes:** +1 add, ~2 change, -3 destroy, /4 replace\n",
		},
		{
			name:     "zero",
			changes:  &ResourceChanges{},
			expected: "**Changes:** \n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatResourceChanges(tt.changes)
			if got != tt.expected {
				t.Errorf("formatResourceChanges() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSplitContent(t *testing.T) {
	content := strings.Repeat("line\n", 10)
	maxSize := 20
	got := splitContent(content, maxSize)
	if len(got) < 2 {
		t.Errorf("splitContent should split into multiple chunks")
	}
}

func TestValidateConfig(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	config = &Config{
		GithubToken: "token",
		Repository:  "owner/repo",
		PullRequest: 1,
		Folders:     []string{"folder"},
		Command:     "plan",
		MaxParallel: 5,
	}
	if err := validateConfig(); err != nil {
		t.Errorf("validateConfig() error = %v, want nil", err)
	}

	config.Repository = "invalid"
	if err := validateConfig(); err == nil {
		t.Error("validateConfig() expected error for invalid repo")
	}
}

func TestSplitOutputByModule(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name: "with module output and summary",
			input: `[account1/baseline] Initializing the backend...
[account1/baseline] Successfully configured the backend "s3"!
[account2/baseline] Initializing the backend...
[account2/baseline] Successfully configured the backend "s3"!

❯❯ Run Summary  2 units  24s
   ────────────────────────────────
   Succeeded    2`,
			expected: map[string]string{
				"account1/baseline": "Initializing the backend...\nSuccessfully configured the backend \"s3\"!",
				"account2/baseline": "Initializing the backend...\nSuccessfully configured the backend \"s3\"!",
				"_summary": "❯❯ Run Summary  2 units  24s\n   ────────────────────────────────\n   Succeeded    2",
			},
		},
		{
			name: "only module output",
			input: `[account1/baseline] Plan: 2 to add, 0 to change, 2 to destroy.
[account2/baseline] Plan: 2 to add, 0 to change, 2 to destroy.`,
			expected: map[string]string{
				"account1/baseline": "Plan: 2 to add, 0 to change, 2 to destroy.",
				"account2/baseline": "Plan: 2 to add, 0 to change, 2 to destroy.",
			},
		},
		{
			name: "only summary (no modules)",
			input: `❯❯ Run Summary  0 units  5s
   ────────────────────────────────
   Succeeded    0`,
			expected: map[string]string{
				"_summary": "❯❯ Run Summary  0 units  5s\n   ────────────────────────────────\n   Succeeded    0",
			},
		},
		{
			name:     "empty input",
			input:    "",
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitOutputByModule(tt.input)

			// Check that we have the expected number of entries
			if len(got) != len(tt.expected) {
				t.Errorf("splitOutputByModule() returned %d entries, want %d", len(got), len(tt.expected))
				t.Errorf("Got keys: %v", getKeys(got))
				t.Errorf("Expected keys: %v", getKeys(tt.expected))
			}

			// Check each expected entry
			for key, expectedVal := range tt.expected {
				gotVal, exists := got[key]
				if !exists {
					t.Errorf("splitOutputByModule() missing key %q", key)
					continue
				}
				if strings.TrimSpace(gotVal) != strings.TrimSpace(expectedVal) {
					t.Errorf("splitOutputByModule()[%q] = %q, want %q", key, gotVal, expectedVal)
				}
			}
		})
	}
}

func getKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestStripAnsiCodes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "standard ANSI escape codes",
			input:    "\x1b[0mHello\x1b[31m World\x1b[0m",
			expected: "Hello World",
		},
		{
			name:     "ANSI codes with multiple parameters",
			input:    "\x1b[1;32mSuccess\x1b[0m",
			expected: "Success",
		},
		{
			name:     "unicode replacement character ANSI",
			input:    "�[0mOpenTofu�[1m will perform�[0m",
			expected: "OpenTofu will perform",
		},
		{
			name:     "mixed ANSI codes",
			input:    "\x1b[32m+\x1b[0m add\x1b[31m-\x1b[0m destroy",
			expected: "+ add- destroy",
		},
		{
			name:     "octal ANSI codes",
			input:    "\033[0mPlan:\033[32m 2\033[0m to add",
			expected: "Plan: 2 to add",
		},
		{
			name:     "no ANSI codes",
			input:    "Plain text without any codes",
			expected: "Plain text without any codes",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name: "complex terraform output with ANSI",
			input: `�[0m�[1mPlan:�[0m 2 to add, 0 to change, 2 to destroy.
�[0m
Changes to Outputs:
  �[33m~�[0m�[0m bucket_name = "old-value" �[33m->�[0m�[0m "new-value"`,
			expected: `Plan: 2 to add, 0 to change, 2 to destroy.

Changes to Outputs:
  ~ bucket_name = "old-value" -> "new-value"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripAnsiCodes(tt.input)
			if got != tt.expected {
				t.Errorf("stripAnsiCodes() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExecuteTerragruntInFolder_PathResolution(t *testing.T) {
	// This test verifies that path resolution works correctly and doesn't create
	// duplicate path components like /repo/live/live/accounts/...
	oldConfig := config
	oldLogger := logger
	defer func() {
		config = oldConfig
		logger = oldLogger
	}()

	// Initialize logger for the test
	logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	config = &Config{
		Command:         "plan",
		TerragruntArgs:  "--non-interactive",
		Folders:         []string{"live/accounts/account1"},
		ParallelExec:    false,
		MaxParallel:     1,
	}

	// Test that relative paths are joined with repo root correctly
	result := executeTerragruntInFolder("live/accounts/test")

	// We expect an error because the folder doesn't exist, but we can verify
	// the folder path in the result doesn't have duplicated components
	if result.Folder != "live/accounts/test" {
		t.Errorf("Expected folder path to be preserved as 'live/accounts/test', got %q", result.Folder)
	}

	// The error should be about the directory not existing (or terragrunt not being installed),
	// NOT about path duplication issues
	if result.Error != nil && strings.Contains(result.Error.Error(), "live/live") {
		t.Errorf("Path duplication detected in error: %v", result.Error)
	}
}

func TestFormatCommentHeader(t *testing.T) {
	oldConfig := config
	defer func() { config = oldConfig }()

	config = &Config{Command: "plan"}

	tests := []struct {
		name     string
		result   ExecutionResult
		expected string
	}{
		{
			name: "success with changes",
			result: ExecutionResult{
				Folder:  "live/accounts/account1",
				Success: true,
				ResourceChanges: &ResourceChanges{
					ToAdd:     2,
					ToChange:  1,
					ToDestroy: 0,
				},
			},
			expected: "## ✅ Success Terragrunt: live/accounts/account1\n**Command:** plan\n**Changes:** +2 add, ~1 change\n",
		},
		{
			name: "failed",
			result: ExecutionResult{
				Folder:  "live/accounts/account2",
				Success: false,
			},
			expected: "## ❌ Failed Terragrunt: live/accounts/account2\n**Command:** plan\n",
		},
		{
			name: "success no changes",
			result: ExecutionResult{
				Folder:          "live/accounts/account3",
				Success:         true,
				ResourceChanges: &ResourceChanges{NoChanges: true},
			},
			expected: "## ✅ Success Terragrunt: live/accounts/account3\n**Command:** plan\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCommentHeader(tt.result)
			if got != tt.expected {
				t.Errorf("formatCommentHeader() = %q, want %q", got, tt.expected)
			}
		})
	}
}
