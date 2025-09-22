package main

import (
	"testing"
)

func TestSanitizeArgs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:    "Valid arguments",
			input:   "--terragrunt-non-interactive --terragrunt-log-level info",
			want:    []string{"--terragrunt-non-interactive", "--terragrunt-log-level", "info"},
			wantErr: false,
		},
		{
			name:    "Command injection attempt with semicolon",
			input:   "--arg1 value; rm -rf /",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Command injection attempt with pipe",
			input:   "--arg1 | cat /etc/passwd",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Command injection attempt with backticks",
			input:   "--arg1 `whoami`",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Command injection attempt with $(...)",
			input:   "--arg1 $(dangerous command)",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Valid args with equals sign",
			input:   "--var=value --another-var=value2",
			want:    []string{"--var=value", "--another-var=value2"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeArgs(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("sanitizeArgs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !equalSlices(got, tt.want) {
				t.Errorf("sanitizeArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateConfig(t *testing.T) {
	// Save original config
	originalConfig := config

	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "Valid config",
			config: &Config{
				GithubToken:     "ghp_test123",
				Repository:      "owner/repo",
				PullRequest:     123,
				Folders:         []string{"folder1", "folder2"},
				Command:         "plan",
				MaxWalkUpLevels: 3,
				MaxRuns:         20,
			},
			wantErr: false,
		},
		{
			name: "Invalid repository format",
			config: &Config{
				GithubToken: "ghp_test123",
				Repository:  "invalid-repo-format",
				PullRequest: 123,
				Folders:     []string{"folder1"},
				Command:     "plan",
			},
			wantErr: true,
		},
		{
			name: "Invalid command",
			config: &Config{
				GithubToken: "ghp_test123",
				Repository:  "owner/repo",
				PullRequest: 123,
				Folders:     []string{"folder1"},
				Command:     "destroy", // Not in allowed list
			},
			wantErr: true,
		},
		{
			name: "Path traversal in folder",
			config: &Config{
				GithubToken: "ghp_test123",
				Repository:  "owner/repo",
				PullRequest: 123,
				Folders:     []string{"../../../etc"},
				Command:     "plan",
			},
			wantErr: true,
		},
		{
			name: "Max runs out of bounds",
			config: &Config{
				GithubToken: "ghp_test123",
				Repository:  "owner/repo",
				PullRequest: 123,
				Folders:     []string{"folder1"},
				Command:     "plan",
				MaxRuns:     150, // Over 100
			},
			wantErr: true,
		},
		{
			name: "Valid run-all command",
			config: &Config{
				GithubToken: "ghp_test123",
				Repository:  "owner/repo",
				PullRequest: 123,
				Folders:     []string{"folder1"},
				Command:     "run-all plan",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config = tt.config
			err := validateConfig()
			if (err != nil) != tt.wantErr {
				t.Errorf("validateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}

	// Restore original config
	config = originalConfig
}

func TestParseResourceChanges(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   *ResourceChanges
	}{
		{
			name: "Standard Terraform plan output",
			output: `Plan: 3 to add, 2 to change, 1 to destroy.`,
			want: &ResourceChanges{
				ToAdd:     3,
				ToChange:  2,
				ToDestroy: 1,
			},
		},
		{
			name: "OpenTofu plan output",
			output: `Plan: 5 to add, 0 to change, 0 to destroy.`,
			want: &ResourceChanges{
				ToAdd:     5,
				ToChange:  0,
				ToDestroy: 0,
			},
		},
		{
			name: "No changes output",
			output: `No changes. Your infrastructure matches the configuration.`,
			want: &ResourceChanges{
				NoChanges: true,
			},
		},
		{
			name: "With import and move",
			output: `Plan: 2 to import, 1 to add, 1 to change, 0 to destroy.
Changes to Outputs:`,
			want: &ResourceChanges{
				ToImport: 2,
				ToAdd:    1,
				ToChange: 1,
			},
		},
		{
			name: "Empty output",
			output: ``,
			want: &ResourceChanges{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseResourceChanges(tt.output)
			if got.ToAdd != tt.want.ToAdd || got.ToChange != tt.want.ToChange ||
				got.ToDestroy != tt.want.ToDestroy || got.ToImport != tt.want.ToImport ||
				got.NoChanges != tt.want.NoChanges {
				t.Errorf("parseResourceChanges() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}