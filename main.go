package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/google/go-github/v75/github"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

const (
	maxCommentSize = 65536 // GitHub comment size limit
	headerSize     = 500   // Estimated size for headers and markdown
)

type Config struct {
	GithubToken       string   // GitHub token for API access
	Repository        string   // GitHub repository in "owner/repo" format
	PullRequest       int      // Pull request number
	Folders           []string // List of folders to run Terragrunt in
	Command           string   // Terragrunt CLI command
	TerragruntArgs    string   // Additional Terragrunt arguments
	ParallelExec      bool     // Whether to execute in parallel
	MaxParallel       int      // Maximum parallel executions (0 = unlimited)
	DeleteOldComments bool     // Whether to delete old bot comments
	AutoDetect        bool     // Whether to auto-detect folders from changed files
	FilePatterns      []string // File patterns to track for auto-detection
	TerragruntFile    string   // Name of the Terragrunt file to look for
	ChangedFiles      []string // List of changed files (for auto-detection)
	MaxWalkUpLevels   int      // Maximum directory levels to walk up when searching for Terragrunt file
	MaxRuns           int      // Maximum number of Terragrunt executions allowed (0 = unlimited)
}

type ExecutionResult struct {
	Folder          string           // Folder where Terragrunt was executed
	Output          string           // Cleaned output from Terragrunt
	Error           error            // Error if execution failed
	ResourceChanges *ResourceChanges // Parsed resource changes
	Success         bool             // Whether the command was successful
}

type ResourceChanges struct {
	ToAdd     int
	ToChange  int
	ToDestroy int
	ToImport  int
	ToMove    int
	ToReplace int
	NoChanges bool
}

var (
	Version    = "dev"
	BuildTime  = "unknown"
	Commit     = "unknown"
	logger     *slog.Logger
	config     = &Config{}
	foldersStr string
)

func main() {
	logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	var rootCmd = &cobra.Command{
		Use:   "terragrunt-runner",
		Short: "Execute Terragrunt commands and post results to GitHub PR",
		Long:  `A tool to run Terragrunt CLI commands in multiple folders and post formatted results to GitHub Pull Requests.`,
		RunE:  run,
	}

	rootCmd.Flags().StringVar(&config.GithubToken, "github-token", os.Getenv("GITHUB_TOKEN"), "GitHub token for API access")
	rootCmd.Flags().StringVar(&config.Repository, "repository", os.Getenv("GITHUB_REPOSITORY"), "GitHub repository (owner/repo)")
	rootCmd.Flags().IntVar(&config.PullRequest, "pull-request", getPRNumber(), "Pull request number")
	rootCmd.Flags().StringVar(&foldersStr, "folders", "", "Folders to run Terragrunt in (comma, space, or newline separated)")
	rootCmd.Flags().StringVar(&config.Command, "command", "plan", "Terragrunt CLI command (e.g., 'plan', 'run --all plan')")
	rootCmd.Flags().StringVar(&config.TerragruntArgs, "args", "--terragrunt-non-interactive", "Additional Terragrunt arguments")
	rootCmd.Flags().BoolVar(&config.ParallelExec, "parallel", true, "Execute in parallel (for multi-folder runs)")
	rootCmd.Flags().IntVar(&config.MaxParallel, "max-parallel", 5, "Maximum parallel executions (0 = unlimited)")
	rootCmd.Flags().BoolVar(&config.DeleteOldComments, "delete-old-comments", true, "Delete previous bot comments")
	rootCmd.Flags().BoolVar(&config.AutoDetect, "auto-detect", false, "Auto-detect Terragrunt folders from changed files")
	rootCmd.Flags().StringSliceVar(&config.FilePatterns, "file-patterns", []string{"*.hcl", "*.json", "*.yaml", "*.yml"}, "File patterns to track for auto-detection")
	rootCmd.Flags().StringVar(&config.TerragruntFile, "terragrunt-file", "terragrunt.hcl", "Name of the Terragrunt file to look for")
	rootCmd.Flags().StringSliceVar(&config.ChangedFiles, "changed-files", []string{}, "List of changed files (for auto-detection)")
	rootCmd.Flags().IntVar(&config.MaxWalkUpLevels, "max-walk-up", 3, "Maximum directory levels to walk up when searching for Terragrunt file")
	rootCmd.Flags().IntVar(&config.MaxRuns, "max-runs", 20, "Maximum number of Terragrunt executions allowed (0 = unlimited)")

	if err := rootCmd.Execute(); err != nil {
		logger.Error("Failed to execute command", "error", err)
		os.Exit(1)
	}
}

func getPRNumber() int {
	if prStr := os.Getenv("GITHUB_PR_NUMBER"); prStr != "" {
		if pr, err := strconv.Atoi(prStr); err == nil {
			return pr
		}
	}
	if ref := os.Getenv("GITHUB_REF"); strings.Contains(ref, "pull/") {
		parts := strings.Split(ref, "/")
		for i, part := range parts {
			if part == "pull" && i+1 < len(parts) {
				if pr, err := strconv.Atoi(parts[i+1]); err == nil {
					return pr
				}
			}
		}
	}
	return 0
}

// Main execution function
func run(cmd *cobra.Command, args []string) error {
	setupLogging()
	fmt.Printf("Terragrunt Runner Version: %s, BuildTime: %s, Commit: %s\n", Version, BuildTime, Commit)

	// Parse folders from input string (comma, space, newline separated)
	config.Folders = parseFolders(foldersStr)

	if config.GithubToken != "" {
		fmt.Printf("::add-mask::%s\n", config.GithubToken)
	}

	// Auto-detect folders if enabled and no folders provided
	if config.AutoDetect {
		detectedFolders := detectTerragruntFolders()
		if len(detectedFolders) > 0 {
			logger.Info("Auto-detected Terragrunt folders", "folders", detectedFolders)
			config.Folders = append(config.Folders, detectedFolders...)
		}
	}

	// Ensure unique folders
	config.Folders = uniqueFolders(config.Folders)

	// Validate max runs
	if config.MaxRuns > 0 && len(config.Folders) > config.MaxRuns {
		fmt.Printf("::error::Too many Terragrunt folders: %d > %d\n", len(config.Folders), config.MaxRuns)
		return fmt.Errorf("exceeds max runs: %d folders vs %d limit", len(config.Folders), config.MaxRuns)
	}

	if err := validateConfig(); err != nil {
		return err
	}

	ctx := context.Background()
	client := createGitHubClient()

	if config.DeleteOldComments {
		if err := deleteOldComments(ctx, client); err != nil {
			logger.Warn("Failed to delete old comments", "error", err)
		}
	}

	results := executeTerragrunt()

	if err := postComments(ctx, client, results); err != nil {
		return err
	}

	if err := postSummary(ctx, client, results); err != nil {
		return err
	}

	totalAdd, totalChange, totalDestroy, totalReplace := 0, 0, 0, 0
	hasErrors := false
	for _, result := range results {
		if !result.Success {
			hasErrors = true
		}
		if result.ResourceChanges != nil {
			totalAdd += result.ResourceChanges.ToAdd
			totalChange += result.ResourceChanges.ToChange
			totalDestroy += result.ResourceChanges.ToDestroy
			totalReplace += result.ResourceChanges.ToReplace
		}
	}

	setActionOutputs(hasErrors, totalAdd, totalChange, totalDestroy, totalReplace)

	if hasErrors {
		return fmt.Errorf("some executions failed")
	}
	return nil
}

// Parse folders from input string
func parseFolders(input string) []string {
	// Replace commas with spaces, then use strings.Fields to split on spaces
	input = strings.ReplaceAll(input, ",", " ")
	input = strings.ReplaceAll(input, "\n", " ")
	return strings.Fields(input)
}

// Set GitHub Action outputs and warnings
func setActionOutputs(hasErrors bool, totalAdd, totalChange, totalDestroy, totalReplace int) error {
	outputFile := os.Getenv("GITHUB_OUTPUT")
	if outputFile == "" {
		return nil
	}
	f, err := os.OpenFile(outputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	outputs := []string{
		fmt.Sprintf("success=%t", !hasErrors),
		fmt.Sprintf("total-resources-to-add=%d", totalAdd),
		fmt.Sprintf("total-resources-to-change=%d", totalChange),
		fmt.Sprintf("total-resources-to-destroy=%d", totalDestroy),
		fmt.Sprintf("total-resources-to-replace=%d", totalReplace),
	}
	for _, output := range outputs {
		fmt.Fprintln(f, output)
	}

	if totalDestroy > 10 {
		fmt.Printf("::warning::High destruction risk: %d resources\n", totalDestroy)
	}
	if totalAdd+totalChange+totalDestroy+totalReplace > 50 {
		fmt.Printf("::warning::Large changes: %d total resources\n", totalAdd+totalChange+totalDestroy+totalReplace)
	}
	return nil
}

// Setup logging based on DEBUG env var
func setupLogging() {
	if os.Getenv("DEBUG") == "true" {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
		slog.SetDefault(logger)
	}
}

// Validate configuration parameters
func validateConfig() error {
	if config.GithubToken == "" || config.Repository == "" || config.PullRequest <= 0 || len(config.Folders) == 0 {
		return fmt.Errorf("missing required config")
	}

	repoParts := strings.Split(config.Repository, "/")
	if len(repoParts) != 2 || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-_.]*$`).MatchString(repoParts[0]) || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-_.]*$`).MatchString(repoParts[1]) {
		return fmt.Errorf("invalid repository format")
	}

	for _, folder := range config.Folders {
		if strings.Contains(folder, "..") || (filepath.IsAbs(folder) && !strings.HasPrefix(folder, "/workspace")) {
			return fmt.Errorf("invalid folder: %s", folder)
		}
	}

	if config.MaxParallel < 0 || config.MaxParallel > 50 {
		return fmt.Errorf("invalid max-parallel")
	}

	// Validate CLI command format
	cmdParts := strings.Fields(config.Command)
	if len(cmdParts) < 1 {
		return fmt.Errorf("invalid command")
	}

	return nil
}

// Create GitHub client with authentication
func createGitHubClient() *github.Client {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: config.GithubToken})
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

// Delete old bot comments from the PR
func deleteOldComments(ctx context.Context, client *github.Client) error {
	parts := strings.Split(config.Repository, "/")
	owner, repo := parts[0], parts[1]
	opts := &github.IssueListCommentsOptions{ListOptions: github.ListOptions{PerPage: 100}}

	for {
		comments, resp, err := client.Issues.ListComments(ctx, owner, repo, config.PullRequest, opts)
		if err != nil {
			return err
		}
		for _, comment := range comments {
			if comment.User != nil && strings.Contains(*comment.User.Login, "[bot]") && (strings.Contains(*comment.Body, "Terragrunt Execution") || strings.Contains(*comment.Body, "Terragrunt Execution Summary")) {
				client.Issues.DeleteComment(ctx, owner, repo, *comment.ID)
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return nil
}

// Execute Terragrunt commands based on configuration
func executeTerragrunt() []ExecutionResult {
	isRunAll := strings.Contains(config.Command, "--all") || strings.HasPrefix(config.Command, "run-all")

	if isRunAll {
		return executeTerragruntAll()
	} else {
		return executeTerragruntPerFolder()
	}
}

// Execute Terragrunt with --all across multiple folders
func executeTerragruntAll() []ExecutionResult {
	fmt.Printf("::group::Terragrunt multi-module across folders\n")
	defer fmt.Println("::endgroup::")

	cmdParts := strings.Fields(config.Command)
	// Replace old "run-all" with new "run --all"
	if cmdParts[0] == "run-all" {
		cmdParts = append([]string{"run", "--all"}, cmdParts[1:]...)
	}

	// Separate Terragrunt command parts and Terraform args if -- is present
	var terragruntParts, tfParts []string
	foundSeparator := false
	for _, part := range cmdParts {
		if part == "--" {
			foundSeparator = true
			continue
		}
		if foundSeparator {
			tfParts = append(tfParts, part)
		} else {
			terragruntParts = append(terragruntParts, part)
		}
	}

	// If no separator and it's a multi-module command, assume the subcommand starts after "run --all"
	if !foundSeparator && len(terragruntParts) > 2 && terragruntParts[0] == "run" && terragruntParts[1] == "--all" {
		tfParts = terragruntParts[2:]
		terragruntParts = terragruntParts[:2]
	}

	// Append additional Terragrunt args to terragruntParts
	if config.TerragruntArgs != "" {
		sArgs, err := sanitizeArgs(config.TerragruntArgs)
		if err != nil {
			return []ExecutionResult{{Folder: ".", Error: err, Success: false}}
		}
		terragruntParts = append(terragruntParts, sArgs...)
	}

	// Reassemble cmdParts: terragruntParts + -- + tfParts if tfParts exist
	cmdParts = terragruntParts
	if len(tfParts) > 0 {
		cmdParts = append(cmdParts, "--")
		cmdParts = append(cmdParts, tfParts...)
	}

	if strings.Contains(config.Command, "plan") && !strings.Contains(config.TerragruntArgs, "-no-color") && !strings.Contains(strings.Join(cmdParts, " "), "-no-color") {
		cmdParts = append(cmdParts, "-no-color")
	}

	if config.MaxParallel > 0 {
		cmdParts = append(cmdParts, "--parallelism", strconv.Itoa(config.MaxParallel))
	}

	for _, folder := range config.Folders {
		cmdParts = append(cmdParts, "--queue-include-dir", folder)
	}

	cmd := exec.Command("terragrunt", cmdParts...)
	cmd.Dir = "."
	cmd.Env = append(os.Environ(), "TF_IN_AUTOMATION=true", "TG_NON_INTERACTIVE=true")

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	output := stdout.String() + stderr.String()

	moduleOutputs := splitOutputByModule(output)
	results := []ExecutionResult{}

	for folder, modOutput := range moduleOutputs {
		cleanOutput := extractTerraformOutput(modOutput)
		changes := parseResourceChanges(modOutput)
		success := err == nil && !strings.Contains(modOutput, "Error:")
		resultErr := err
		if success {
			resultErr = nil
		}
		results = append(results, ExecutionResult{
			Folder:          folder,
			Output:          cleanOutput,
			Error:           resultErr,
			ResourceChanges: changes,
			Success:         success,
		})
	}

	if len(results) == 0 {
		// Fallback if splitting failed
		cleanOutput := extractTerraformOutput(output)
		changes := parseResourceChanges(output)
		success := err == nil
		results = append(results, ExecutionResult{
			Folder:          ".",
			Output:          cleanOutput,
			Error:           err,
			ResourceChanges: changes,
			Success:         success,
		})
	}

	return results
}

// Split Terragrunt output by module/folder
func splitOutputByModule(output string) map[string]string {
	moduleOutputs := make(map[string][]string)
	var currentModule string
	r := regexp.MustCompile(`^\[(.*?)\] (.*)$`)
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := scanner.Text()
		if match := r.FindStringSubmatch(line); match != nil {
			currentModule = match[1]
			moduleOutputs[currentModule] = append(moduleOutputs[currentModule], match[2])
		} else if currentModule != "" {
			moduleOutputs[currentModule] = append(moduleOutputs[currentModule], line)
		}
	}

	result := make(map[string]string)
	for mod, lines := range moduleOutputs {
		result[mod] = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return result
}

// Execute Terragrunt in each folder separately
func executeTerragruntPerFolder() []ExecutionResult {
	var results []ExecutionResult
	var wg sync.WaitGroup
	resultsChan := make(chan ExecutionResult, len(config.Folders))
	sem := make(chan struct{}, getMaxParallel())

	useParallel := config.ParallelExec && getMaxParallel() > 0

	for _, folder := range config.Folders {
		if useParallel {
			wg.Add(1)
			go func(f string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				resultsChan <- executeTerragruntInFolder(f)
			}(folder)
		} else {
			results = append(results, executeTerragruntInFolder(folder))
		}
	}

	if useParallel {
		wg.Wait()
		close(resultsChan)
		for result := range resultsChan {
			results = append(results, result)
		}
	}
	return results
}

// Get maximum parallel executions
func getMaxParallel() int {
	if config.MaxParallel == 0 {
		return len(config.Folders)
	}
	return config.MaxParallel
}

// Sanitize additional Terragrunt arguments
func sanitizeArgs(args string) ([]string, error) {
	fields := strings.Fields(args)
	sanitized := []string{}

	forbidden := []string{";", "&&", "||", "|", ">", "<", "`", "$(", "${"}

	for _, field := range fields {
		for _, pat := range forbidden {
			if strings.Contains(field, pat) {
				return nil, fmt.Errorf("forbidden pattern in arg: %s", field)
			}
		}
		sanitized = append(sanitized, field)
	}
	return sanitized, nil
}

// Execute Terragrunt in a specific folder
func executeTerragruntInFolder(folder string) ExecutionResult {
	fmt.Printf("::group::Terragrunt in %s\n", folder)
	defer fmt.Println("::endgroup::")

	absFolder, _ := filepath.Abs(folder) // Ignore err for simplicity; validate earlier

	cmdParts := strings.Fields(config.Command)
	if config.TerragruntArgs != "" {
		sArgs, err := sanitizeArgs(config.TerragruntArgs)
		if err != nil {
			return ExecutionResult{Folder: folder, Error: err, Success: false}
		}
		cmdParts = append(cmdParts, sArgs...)
	}

	if strings.Contains(config.Command, "plan") && !strings.Contains(config.TerragruntArgs, "-no-color") && !strings.Contains(strings.Join(cmdParts, " "), "-no-color") {
		cmdParts = append(cmdParts, "-no-color")
	}

	cmd := exec.Command("terragrunt", cmdParts...)
	cmd.Dir = absFolder
	cmd.Env = append(os.Environ(), "TF_IN_AUTOMATION=true", "TERRAGRUNT_NON_INTERACTIVE=true")

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	output := stdout.String() + stderr.String()

	cleanOutput := extractTerraformOutput(output)
	changes := parseResourceChanges(output)

	return ExecutionResult{
		Folder:          folder,
		Output:          cleanOutput,
		Error:           err,
		ResourceChanges: changes,
		Success:         err == nil,
	}
}

// Extract relevant Terraform output, filtering noise
func extractTerraformOutput(output string) string {
	lines := strings.Split(output, "\n")
	var clean []string
	capture := false
	var resourceLines []string
	inResource := false

	startPatterns := []string{"Terraform will perform", "OpenTofu will perform", "No changes", "Terraform used the selected providers"}
	endPatterns := []string{"Plan:", "Apply complete!", "Destroy complete!"}
	skipPatterns := []string{"[terragrunt]", "Refreshing state", "Acquiring state lock", "Initializing", "Downloading"}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		skip := false
		for _, pat := range skipPatterns {
			if strings.Contains(line, pat) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		if !capture {
			for _, pat := range startPatterns {
				if strings.Contains(line, pat) {
					capture = true
					clean = append(clean, line)
					break
				}
			}
			if strings.HasPrefix(trimmed, "Error:") {
				capture = true
				clean = append(clean, line)
			}
			continue
		}

		for _, pat := range endPatterns {
			if strings.Contains(line, pat) {
				clean = append(clean, line)
				capture = false
				break
			}
		}

		if capture {
			if strings.HasPrefix(trimmed, "#") && strings.ContainsAny(line, "will be must be has been") {
				if inResource && len(resourceLines) > 0 {
					clean = append(clean, resourceLines...)
				}
				inResource = true
				resourceLines = []string{line}
			} else if inResource {
				if trimmed == "" || strings.HasPrefix(trimmed, "+~-}/{") || strings.Contains(line, "->=") {
					resourceLines = append(resourceLines, line)
				} else {
					if len(resourceLines) > 0 {
						clean = append(clean, resourceLines...)
					}
					inResource = false
					resourceLines = nil
					if trimmed != "" {
						clean = append(clean, line)
					}
				}
			} else if trimmed != "" {
				clean = append(clean, line)
			}
		}
	}
	if inResource && len(resourceLines) > 0 {
		clean = append(clean, resourceLines...)
	}
	if len(clean) == 0 {
		return "No changes detected."
	}
	return strings.TrimSpace(strings.Join(clean, "\n"))
}

// Parse resource changes from Terragrunt output
func parseResourceChanges(output string) *ResourceChanges {
	changes := &ResourceChanges{}

	// Regex for plan summary (with replace/import)
	fullRegex := regexp.MustCompile(`Plan: (\d+) to (?:import|add), (\d+) to (?:add|change), (\d+) to change, (\d+) to destroy(?:, (\d+) to replace)?`)
	matches := fullRegex.FindStringSubmatch(output)
	if len(matches) >= 4 {
		changes.ToAdd, _ = strconv.Atoi(matches[1])
		changes.ToChange, _ = strconv.Atoi(matches[2])
		changes.ToDestroy, _ = strconv.Atoi(matches[3])
		if len(matches) > 4 && matches[5] != "" {
			changes.ToReplace, _ = strconv.Atoi(matches[5])
		}
	}

	// Fallbacks for "will be" phrases
	if changes.ToAdd == 0 {
		if m := regexp.MustCompile(`(\d+) .* will be (created|added)`).FindStringSubmatch(output); len(m) > 0 {
			changes.ToAdd, _ = strconv.Atoi(m[1])
		}
	}
	if changes.ToChange == 0 {
		if m := regexp.MustCompile(`(\d+) .* will be (updated|changed)`).FindStringSubmatch(output); len(m) > 0 {
			changes.ToChange, _ = strconv.Atoi(m[1])
		}
	}
	if changes.ToDestroy == 0 {
		if m := regexp.MustCompile(`(\d+) .* will be (destroyed|deleted)`).FindStringSubmatch(output); len(m) > 0 {
			changes.ToDestroy, _ = strconv.Atoi(m[1])
		}
	}
	if changes.ToReplace == 0 {
		if m := regexp.MustCompile(`(\d+) .* will be replaced`).FindStringSubmatch(output); len(m) > 0 {
			changes.ToReplace, _ = strconv.Atoi(m[1])
		}
	}

	// No changes
	if strings.Contains(output, "No changes") || (changes.ToAdd == 0 && changes.ToChange == 0 && changes.ToDestroy == 0 && changes.ToReplace == 0) {
		changes.NoChanges = true
	}

	return changes
}

// Post individual comments for each execution result
func postComments(ctx context.Context, client *github.Client, results []ExecutionResult) error {
	parts := strings.Split(config.Repository, "/")
	owner, repo := parts[0], parts[1]

	for _, result := range results {
		header := formatCommentHeader(result)
		content := result.Output
		detailsTitle := "View Output"
		if !result.Success {
			detailsTitle = "View Error Details"
		}

		if len(header)+len(content) <= maxCommentSize-headerSize {
			body := header + "\n\n<details><summary><b>" + detailsTitle + "</b></summary>\n\n```hcl\n" + content + "\n```\n</details>"
			createComment(ctx, client, owner, repo, body)
		} else {
			chunks := splitContent(content, maxCommentSize-headerSize-300)
			for i, chunk := range chunks {
				partHeader := formatCommentHeaderWithPart(result, i+1, len(chunks))
				partTitle := fmt.Sprintf("%s (Part %d/%d)", detailsTitle, i+1, len(chunks))
				body := partHeader + "\n\n<details><summary><b>" + partTitle + "</b></summary>\n\n```hcl\n" + chunk + "\n```\n</details>"
				createComment(ctx, client, owner, repo, body)
			}
		}
	}
	return nil
}

// Format comment header with status and changes
func formatCommentHeader(result ExecutionResult) string {
	status := "✅ Success"
	if !result.Success {
		status = "❌ Failed"
	}
	header := fmt.Sprintf("## %s Terragrunt: %s\n", status, result.Folder)
	header += fmt.Sprintf("**Command:** %s\n", config.Command)
	if result.ResourceChanges != nil && !result.ResourceChanges.NoChanges {
		header += formatResourceChanges(result.ResourceChanges)
	}
	return header
}

// Format comment header with part information
func formatCommentHeaderWithPart(result ExecutionResult, part, total int) string {
	header := formatCommentHeader(result)
	return strings.Replace(header, result.Folder, fmt.Sprintf("%s (%d/%d)", result.Folder, part, total), 1)
}

// Format resource changes summary
func formatResourceChanges(changes *ResourceChanges) string {
	parts := []string{}
	if changes.ToAdd > 0 {
		parts = append(parts, fmt.Sprintf("+%d add", changes.ToAdd))
	}
	if changes.ToChange > 0 {
		parts = append(parts, fmt.Sprintf("~%d change", changes.ToChange))
	}
	if changes.ToDestroy > 0 {
		parts = append(parts, fmt.Sprintf("-%d destroy", changes.ToDestroy))
	}
	if changes.ToReplace > 0 {
		parts = append(parts, fmt.Sprintf("/%d replace", changes.ToReplace))
	}
	return "**Changes:** " + strings.Join(parts, ", ") + "\n"
}

// Split content into manageable chunks for comments
func splitContent(content string, maxSize int) []string {
	var chunks []string
	var builder strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := scanner.Text() + "\n"
		if builder.Len()+len(line) > maxSize && builder.Len() > 0 {
			chunks = append(chunks, builder.String())
			builder.Reset()
		}
		builder.WriteString(line)
	}
	if builder.Len() > 0 {
		chunks = append(chunks, builder.String())
	}
	return chunks
}

// Post a summary comment with overall results
func postSummary(ctx context.Context, client *github.Client, results []ExecutionResult) error {
	parts := strings.Split(config.Repository, "/")
	owner, repo := parts[0], parts[1]
	summary := formatSummary(results)
	return createComment(ctx, client, owner, repo, summary)
}

// Format summary of all execution results
func formatSummary(results []ExecutionResult) string {
	var b strings.Builder
	b.WriteString("## Terragrunt Summary\n\n**Command:** " + config.Command + "\n**Folders:** " + fmt.Sprint(len(results)) + "\n\n")

	b.WriteString("| Folder | Status | Add | Change | Destroy | Replace |\n|--------|--------|-----|--------|---------|---------|\n")
	success, noChange := 0, 0
	for _, r := range results {
		status := "✅"
		if !r.Success {
			status = "❌"
		} else {
			success++
		}
		add, change, destroy, replace := "0", "0", "0", "0"
		if r.ResourceChanges != nil {
			if !r.ResourceChanges.NoChanges {
				if r.ResourceChanges.ToAdd > 0 {
					add = fmt.Sprintf("+%d", r.ResourceChanges.ToAdd)
				}
				if r.ResourceChanges.ToChange > 0 {
					change = fmt.Sprintf("~%d", r.ResourceChanges.ToChange)
				}
				if r.ResourceChanges.ToDestroy > 0 {
					destroy = fmt.Sprintf("-%d", r.ResourceChanges.ToDestroy)
				}
				if r.ResourceChanges.ToReplace > 0 {
					replace = fmt.Sprintf("/%d", r.ResourceChanges.ToReplace)
				}
			} else {
				noChange++
			}
		}
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s |\n", r.Folder, status, add, change, destroy, replace))
	}

	b.WriteString(fmt.Sprintf("\n- Success: %d/%d\n- No Changes: %d\n", success, len(results), noChange))
	return b.String()
}

// Create a comment on the GitHub PR
func createComment(ctx context.Context, client *github.Client, owner, repo, body string) error {
	comment := &github.IssueComment{Body: &body}
	_, _, err := client.Issues.CreateComment(ctx, owner, repo, config.PullRequest, comment)
	return err
}

// Detect Terragrunt folders based on changed files
func detectTerragruntFolders() []string {
	found := make(map[string]bool)
	if len(config.ChangedFiles) == 0 {
		config.ChangedFiles = getChangedFilesFromGit()
	}
	for _, file := range config.ChangedFiles {
		if matchesPatterns(file, config.FilePatterns) {
			dir := findTerragruntDirectory(file)
			if dir != "" {
				found[dir] = true
			}
		}
	}
	var res []string
	for k := range found {
		res = append(res, k)
	}
	return res
}

// Get changed files from the last git commit
func getChangedFilesFromGit() []string {
	cmd := exec.Command("git", "diff", "--name-only", "HEAD~1")
	out, _ := cmd.Output()
	files := strings.Split(string(out), "\n")
	var clean []string
	for _, f := range files {
		if f = strings.TrimSpace(f); f != "" {
			clean = append(clean, f)
		}
	}
	return uniqueStrings(clean)
}

// Check if file matches any of the specified patterns
func matchesPatterns(file string, patterns []string) bool {
	for _, pat := range patterns {
		if matched, _ := filepath.Match(pat, filepath.Base(file)); matched {
			return true
		}
	}
	return false
}

// Find the nearest Terragrunt directory by walking up the path
func findTerragruntDirectory(filePath string) string {
	dir := filepath.Dir(filePath)
	for i := 0; i < config.MaxWalkUpLevels; i++ {
		tgPath := filepath.Join(dir, config.TerragruntFile)
		if _, err := os.Stat(tgPath); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// Ensure folders are unique and clean paths
func uniqueFolders(folders []string) []string {
	seen := make(map[string]bool)
	var res []string
	for _, f := range folders {
		nf := filepath.Clean(f)
		if !seen[nf] {
			seen[nf] = true
			res = append(res, nf)
		}
	}
	return res
}

// Ensure strings are unique
func uniqueStrings(strs []string) []string {
	seen := make(map[string]bool)
	var res []string
	for _, s := range strs {
		if !seen[s] {
			seen[s] = true
			res = append(res, s)
		}
	}
	return res
}
