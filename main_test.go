package main

import (
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
			name:     "add",
			input:    `Plan: 1 to import, 0 to add, 0 to change, 0 to destroy.`,
			expected: &ResourceChanges{ToAdd: 1},
		},
		{
			name:     "change",
			input:    `Plan: 0 to import, 1 to add, 0 to change, 0 to destroy.`,
			expected: &ResourceChanges{ToChange: 1},
		},
		{
			name:     "destroy",
			input:    `Plan: 0 to import, 0 to add, 1 to change, 0 to destroy.`,
			expected: &ResourceChanges{ToDestroy: 1},
		},
		{
			name:     "replace",
			input:    `Plan: 0 to import, 0 to add, 0 to change, 0 to destroy, 1 to replace.`,
			expected: &ResourceChanges{ToReplace: 1},
		},
		{
			name:     "no changes",
			input:    `No changes`,
			expected: &ResourceChanges{NoChanges: true},
		},
		{
			name:     "fallback add",
			input:    `1 resource will be created.`,
			expected: &ResourceChanges{ToAdd: 1},
		},
		{
			name:     "fallback change",
			input:    `1 resource will be updated.`,
			expected: &ResourceChanges{ToChange: 1},
		},
		{
			name:     "fallback destroy",
			input:    `1 resource will be destroyed.`,
			expected: &ResourceChanges{ToDestroy: 1},
		},
		{
			name:     "fallback replace",
			input:    `1 resource will be replaced.`,
			expected: &ResourceChanges{ToReplace: 1},
		},
		{
			name:     "complex",
			input:    `Plan: 2 to import, 3 to add, 1 to change, 0 to destroy, 4 to replace.`,
			expected: &ResourceChanges{ToAdd: 2, ToChange: 3, ToDestroy: 1, ToReplace: 4},
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
			name: "with skip lines",
			input: `
Refreshing state...
Acquiring state lock...
Terraform used the selected providers to generate the following execution plan.
No changes.
`,
			expected: `Terraform used the selected providers to generate the following execution plan.
No changes.`,
		},
		{
			name:     "error",
			input:    `Error: Invalid configuration`,
			expected: `Error: Invalid configuration`,
		},
		{
			name:     "empty",
			input:    "",
			expected: "No changes detected.",
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
