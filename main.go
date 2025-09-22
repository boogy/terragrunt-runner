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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v60/github"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

const (
	maxCommentSize = 65536
	headerSize     = 500
)

type Config struct {
	GithubToken       string
	Repository        string
	PullRequest       int
	Folders           []string
	Command           string
	TerragruntArgs    string
	ParallelExec      bool
	DeleteOldComments bool
	AutoDetect        bool
	FilePatterns      []string
	TerragruntFile    string
	ChangedFiles      []string
	MaxWalkUpLevels   int
	MaxRuns           int
}

type ExecutionResult struct {
	Folder          string
	Output          string
	Error           error
	ResourceChanges *ResourceChanges
	Success         bool
}

type ResourceChanges struct {
	ToAdd     int
	ToChange  int
	ToDestroy int
	ToImport  int
	ToMove    int
	NoChanges bool
}

var (
	logger *slog.Logger
	config = &Config{}
)

func main() {
	// Initialize structured logger
	logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	var rootCmd = &cobra.Command{
		Use:   "terragrunt-runner",
		Short: "Execute Terragrunt commands and post results to GitHub PR",
		Long:  `A professional tool to run Terragrunt in multiple folders and post formatted results to GitHub Pull Requests.`,
		RunE:  run,
	}

	rootCmd.Flags().StringVar(&config.GithubToken, "github-token", os.Getenv("GITHUB_TOKEN"), "GitHub token for API access")
	rootCmd.Flags().StringVar(&config.Repository, "repository", os.Getenv("GITHUB_REPOSITORY"), "GitHub repository (owner/repo)")
	rootCmd.Flags().IntVar(&config.PullRequest, "pull-request", getPRNumber(), "Pull request number")
	rootCmd.Flags().StringSliceVar(&config.Folders, "folders", []string{}, "Folders to run Terragrunt in")
	rootCmd.Flags().StringVar(&config.Command, "command", "plan", "Terragrunt command (plan, apply, init, run-all)")
	rootCmd.Flags().StringVar(&config.TerragruntArgs, "terragrunt-args", "--terragrunt-non-interactive", "Additional Terragrunt arguments")
	rootCmd.Flags().BoolVar(&config.ParallelExec, "parallel", false, "Execute in parallel for run-all commands")
	rootCmd.Flags().BoolVar(&config.DeleteOldComments, "delete-old-comments", true, "Delete previous bot comments")
	rootCmd.Flags().BoolVar(&config.AutoDetect, "auto-detect", false, "Auto-detect Terragrunt folders from changed files")
	rootCmd.Flags().StringSliceVar(&config.FilePatterns, "file-patterns", []string{"*.tf", "*.hcl", "*.json", "*.yaml", "*.yml"}, "File patterns to track for auto-detection")
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

func run(cmd *cobra.Command, args []string) error {
	setupLogging()

	// Mask the GitHub token in logs
	if config.GithubToken != "" {
		fmt.Printf("::add-mask::%s\n", config.GithubToken)
	}

	// Auto-detect folders if enabled
	if config.AutoDetect {
		detectedFolders := detectTerragruntFolders()
		if len(detectedFolders) > 0 {
			logger.Info("Auto-detected Terragrunt folders", "folders", detectedFolders)
			config.Folders = append(config.Folders, detectedFolders...)
		}
	}

	// Remove duplicates from folders
	config.Folders = uniqueFolders(config.Folders)

	// Check if we exceed max runs limit
	if config.MaxRuns > 0 && len(config.Folders) > config.MaxRuns {
		fmt.Printf("::error::Too many Terragrunt folders detected: %d folders exceed the limit of %d\n",
			len(config.Folders), config.MaxRuns)
		logger.Error("Too many Terragrunt folders to process",
			"totalFolders", len(config.Folders),
			"maxRuns", config.MaxRuns)

		return fmt.Errorf("number of Terragrunt folders (%d) exceeds maximum allowed runs (%d). "+
			"You can either increase --max-runs or set it to 0 to disable the limit. "+
			"Folders that would be processed: %v",
			len(config.Folders), config.MaxRuns, config.Folders)
	}

	// Warn if approaching the limit
	if config.MaxRuns > 0 && len(config.Folders) > int(float64(config.MaxRuns)*0.8) {
		fmt.Printf("::warning::Approaching maximum runs limit: %d of %d folders\n",
			len(config.Folders), config.MaxRuns)
		logger.Warn("Approaching maximum runs limit",
			"totalFolders", len(config.Folders),
			"maxRuns", config.MaxRuns)
	}

	if err := validateConfig(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
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
		return fmt.Errorf("failed to post comments: %w", err)
	}

	if err := postSummary(ctx, client, results); err != nil {
		return fmt.Errorf("failed to post summary: %w", err)
	}

	// Calculate totals for outputs
	totalAdd, totalChange, totalDestroy := 0, 0, 0
	hasErrors := false
	for _, result := range results {
		if !result.Success {
			hasErrors = true
		}
		if result.ResourceChanges != nil {
			totalAdd += result.ResourceChanges.ToAdd
			totalChange += result.ResourceChanges.ToChange
			totalDestroy += result.ResourceChanges.ToDestroy
		}
	}

	// Set GitHub Actions outputs
	if err := setActionOutputs(hasErrors, totalAdd, totalChange, totalDestroy); err != nil {
		logger.Warn("Failed to set action outputs", "error", err)
	}

	if hasErrors {
		return fmt.Errorf("some Terragrunt executions failed")
	}

	return nil
}

// setActionOutputs writes outputs for GitHub Actions
func setActionOutputs(hasErrors bool, totalAdd, totalChange, totalDestroy int) error {
	// GitHub Actions output format
	outputFile := os.Getenv("GITHUB_OUTPUT")
	if outputFile == "" {
		// Not running in GitHub Actions context
		logger.Debug("GITHUB_OUTPUT not set, skipping output generation")
		return nil
	}

	// Write to GITHUB_OUTPUT file
	f, err := os.OpenFile(outputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open GITHUB_OUTPUT file: %w", err)
	}
	defer f.Close()

	outputs := []string{
		fmt.Sprintf("success=%t", !hasErrors),
		fmt.Sprintf("total-resources-to-add=%d", totalAdd),
		fmt.Sprintf("total-resources-to-change=%d", totalChange),
		fmt.Sprintf("total-resources-to-destroy=%d", totalDestroy),
	}

	for _, output := range outputs {
		if _, err := fmt.Fprintln(f, output); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}
	}

	// Also emit summary annotations if there are significant changes
	if totalDestroy > 10 {
		fmt.Printf("::warning::⚠️ This plan will destroy %d resources. Please review carefully before applying.\n", totalDestroy)
	}
	if totalAdd+totalChange+totalDestroy > 50 {
		fmt.Printf("::warning::🚨 Large number of changes detected (%d total). Consider reviewing in smaller batches.\n",
			totalAdd+totalChange+totalDestroy)
	}

	return nil
}

func setupLogging() {
	// Logger is already initialized in main()
	// Adjust log level based on DEBUG environment variable
	if os.Getenv("DEBUG") == "true" {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}))
		slog.SetDefault(logger)
	}
}

func validateConfig() error {
	if config.GithubToken == "" {
		return fmt.Errorf("GitHub token is required")
	}
	if config.Repository == "" {
		return fmt.Errorf("repository is required")
	}

	// Validate repository format
	repoParts := strings.Split(config.Repository, "/")
	if len(repoParts) != 2 || repoParts[0] == "" || repoParts[1] == "" {
		return fmt.Errorf("repository must be in format 'owner/repo'")
	}

	// Validate repository name characters
	repoPattern := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-_.]*$`)
	if !repoPattern.MatchString(repoParts[0]) || !repoPattern.MatchString(repoParts[1]) {
		return fmt.Errorf("repository contains invalid characters")
	}

	if config.PullRequest <= 0 {
		return fmt.Errorf("pull request number must be positive")
	}

	if len(config.Folders) == 0 {
		return fmt.Errorf("at least one folder must be specified")
	}

	// Validate folder paths
	for _, folder := range config.Folders {
		// Check for path traversal attempts
		if strings.Contains(folder, "..") {
			return fmt.Errorf("folder path contains invalid traversal: %s", folder)
		}
		// Check for absolute paths (security risk in Docker)
		if filepath.IsAbs(folder) && !strings.HasPrefix(folder, "/workspace") {
			return fmt.Errorf("absolute paths outside /workspace are not allowed: %s", folder)
		}
	}

	// Validate numeric bounds
	if config.MaxWalkUpLevels < 0 || config.MaxWalkUpLevels > 20 {
		return fmt.Errorf("max-walk-up must be between 0 and 20")
	}

	if config.MaxRuns < 0 || config.MaxRuns > 100 {
		return fmt.Errorf("max-runs must be between 0 and 100")
	}

	// Validate command
	validCommands := []string{"plan", "apply", "init", "validate", "run-all plan", "run-all apply", "run-all init", "run-all validate"}
	validCommand := slices.Contains(validCommands, config.Command)
	if !validCommand {
		return fmt.Errorf("invalid command: %s", config.Command)
	}

	return nil
}

func createGitHubClient() *github.Client {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: config.GithubToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

func deleteOldComments(ctx context.Context, client *github.Client) error {
	parts := strings.Split(config.Repository, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repository format")
	}
	owner, repo := parts[0], parts[1]

	opts := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		comments, resp, err := client.Issues.ListComments(ctx, owner, repo, config.PullRequest, opts)
		if err != nil {
			return err
		}

		for _, comment := range comments {
			if comment.User != nil && strings.Contains(*comment.User.Login, "[bot]") &&
				(strings.Contains(*comment.Body, "Terragrunt Execution") ||
					strings.Contains(*comment.Body, "Terragrunt Execution Summary")) {
				_, err := client.Issues.DeleteComment(ctx, owner, repo, *comment.ID)
				if err != nil {
					logger.Warn("Failed to delete comment", "error", err)
				}
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return nil
}

func executeTerragrunt() []ExecutionResult {
	var results []ExecutionResult
	var wg sync.WaitGroup
	resultsChan := make(chan ExecutionResult, len(config.Folders))

	for _, folder := range config.Folders {
		if strings.HasPrefix(config.Command, "run-all") && config.ParallelExec {
			wg.Add(1)
			go func(f string) {
				defer wg.Done()
				result := executeTerragruntInFolder(f)
				resultsChan <- result
			}(folder)
		} else {
			result := executeTerragruntInFolder(folder)
			results = append(results, result)
		}
	}

	if strings.HasPrefix(config.Command, "run-all") && config.ParallelExec {
		wg.Wait()
		close(resultsChan)
		for result := range resultsChan {
			results = append(results, result)
		}
	}

	return results
}

// sanitizeArgs sanitizes and validates command arguments
func sanitizeArgs(args string) ([]string, error) {
	// Split arguments properly, respecting quotes
	fields := strings.Fields(args)
	sanitized := make([]string, 0, len(fields))

	// List of forbidden patterns that could be dangerous
	forbiddenPatterns := []string{
		";", "&&", "||", "|", ">", "<", "`", "$(", "${",
	}

	for _, field := range fields {
		// Check for forbidden patterns
		for _, pattern := range forbiddenPatterns {
			if strings.Contains(field, pattern) {
				return nil, fmt.Errorf("forbidden pattern '%s' in argument: %s", pattern, field)
			}
		}

		// Only allow specific safe argument patterns
		if strings.HasPrefix(field, "--") || strings.HasPrefix(field, "-") ||
			!strings.HasPrefix(field, "-") {
			sanitized = append(sanitized, field)
		}
	}

	return sanitized, nil
}

func executeTerragruntInFolder(folder string) ExecutionResult {
	// Start group for this folder's execution
	fmt.Printf("::group::Executing Terragrunt in %s\n", folder)
	defer fmt.Println("::endgroup::")

	logger.Info("Executing Terragrunt", "folder", folder)

	absFolder, err := filepath.Abs(folder)
	if err != nil {
		fmt.Printf("::error::Failed to resolve folder path: %s\n", err.Error())
		return ExecutionResult{
			Folder:  folder,
			Error:   err,
			Success: false,
		}
	}

	// Ensure folder is within workspace
	if !strings.HasPrefix(absFolder, "/workspace") && !strings.HasPrefix(absFolder, ".") {
		err := fmt.Errorf("folder must be within workspace: %s", folder)
		fmt.Printf("::error::Security violation - %s\n", err.Error())
		return ExecutionResult{
			Folder:  folder,
			Error:   err,
			Success: false,
		}
	}

	cmdParts := []string{config.Command}
	if config.TerragruntArgs != "" {
		sanitizedArgs, err := sanitizeArgs(config.TerragruntArgs)
		if err != nil {
			fmt.Printf("::error::Invalid Terragrunt arguments - %s\n", err.Error())
			return ExecutionResult{
				Folder:  folder,
				Error:   fmt.Errorf("invalid terragrunt arguments: %w", err),
				Success: false,
			}
		}
		cmdParts = append(cmdParts, sanitizedArgs...)
	}

	if config.Command == "plan" || strings.HasSuffix(config.Command, "plan") {
		cmdParts = append(cmdParts, "-no-color")
	}

	cmd := exec.Command("terragrunt", cmdParts...)
	cmd.Dir = absFolder
	cmd.Env = append(os.Environ(),
		"TF_IN_AUTOMATION=true",
		"TERRAGRUNT_NON_INTERACTIVE=true",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err = cmd.Run()
	duration := time.Since(startTime)

	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\n" + stderr.String()
	}

	logger.Info("Terragrunt execution completed",
		"folder", folder,
		"duration", duration,
		"success", err == nil)

	// Add error annotation if execution failed
	if err != nil {
		fmt.Printf("::error file=%s::Terragrunt execution failed in %s: %s\n",
			filepath.Join(folder, "terragrunt.hcl"), folder, err.Error())
	}

	cleanOutput := extractTerraformOutput(output)
	resourceChanges := parseResourceChanges(output)

	return ExecutionResult{
		Folder:          folder,
		Output:          cleanOutput,
		Error:           err,
		ResourceChanges: resourceChanges,
		Success:         err == nil,
	}
}

func extractTerraformOutput(output string) string {
	lines := strings.Split(output, "\n")
	var cleanLines []string
	var captureOutput bool
	var inResourceBlock bool
	var resourceBlockLines []string

	// Patterns that indicate the start of important output
	planStartPatterns := []string{
		"Terraform will perform the following actions:",
		"OpenTofu will perform the following actions:",
		"Terraform used the selected providers",
		"OpenTofu used the selected providers",
		"No changes. Your infrastructure matches the configuration",
		"No changes. Infrastructure is up-to-date",
		"Your infrastructure matches the configuration",
	}

	// Patterns that indicate the end of the plan output
	planEndPatterns := []string{
		"Plan:",
		"Apply complete!",
		"Destroy complete!",
	}

	// Patterns to skip entirely
	skipPatterns := []string{
		"[terragrunt]",
		"Refreshing state...",
		"Reading...",
		"Read complete after",
		"Acquiring state lock",
		"Releasing state lock",
		"Initializing the backend",
		"Initializing provider plugins",
		"Downloading",
		"Installing",
		"- Reusing previous version",
		"Partner and community providers",
		"Finding",
		"Using previously-installed",
	}

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Check if we should skip this line entirely
		shouldSkip := false
		for _, skipPattern := range skipPatterns {
			if strings.Contains(line, skipPattern) {
				shouldSkip = true
				break
			}
		}
		if shouldSkip {
			continue
		}

		// Check for plan start patterns
		if !captureOutput {
			for _, pattern := range planStartPatterns {
				if strings.Contains(line, pattern) {
					captureOutput = true
					cleanLines = append(cleanLines, line)
					break
				}
			}
			// Also capture error messages
			if strings.HasPrefix(trimmedLine, "Error:") || strings.HasPrefix(trimmedLine, "│ Error:") {
				captureOutput = true
				cleanLines = append(cleanLines, line)
			}
			continue
		}

		// Check for plan end patterns (summary line)
		for _, pattern := range planEndPatterns {
			if strings.Contains(line, pattern) {
				// Add the summary line and stop capturing
				cleanLines = append(cleanLines, line)
				captureOutput = false
				break
			}
		}

		if captureOutput {
			// Handle resource blocks (# resource.name will be created/updated/destroyed)
			if strings.HasPrefix(trimmedLine, "#") && (strings.Contains(line, "will be") ||
				strings.Contains(line, "must be") || strings.Contains(line, "has been")) {
				// Start of a resource block
				if inResourceBlock && len(resourceBlockLines) > 0 {
					// Add previous resource block
					cleanLines = append(cleanLines, resourceBlockLines...)
				}
				inResourceBlock = true
				resourceBlockLines = []string{line}
			} else if inResourceBlock {
				// Continue collecting resource block lines
				if trimmedLine == "" {
					// Empty line might indicate end of resource block
					if len(resourceBlockLines) > 0 {
						cleanLines = append(cleanLines, resourceBlockLines...)
						cleanLines = append(cleanLines, "")
					}
					inResourceBlock = false
					resourceBlockLines = nil
				} else if strings.HasPrefix(trimmedLine, "+") || strings.HasPrefix(trimmedLine, "-") ||
					strings.HasPrefix(trimmedLine, "~") || strings.HasPrefix(trimmedLine, "}") ||
					strings.HasPrefix(trimmedLine, "{") || strings.Contains(line, "->") ||
					strings.Contains(line, "=") || strings.HasPrefix(trimmedLine, "#") {
					// This is part of the resource changes
					resourceBlockLines = append(resourceBlockLines, line)
				}
			} else if trimmedLine != "" {
				// Capture other important lines like warnings or summaries
				if !strings.Contains(line, "Refreshing") && !strings.Contains(line, "Reading") {
					cleanLines = append(cleanLines, line)
				}
			}
		}
	}

	// Add any remaining resource block
	if inResourceBlock && len(resourceBlockLines) > 0 {
		cleanLines = append(cleanLines, resourceBlockLines...)
	}

	// If we didn't capture anything meaningful, check for "No changes" message
	if len(cleanLines) == 0 {
		for _, line := range lines {
			if strings.Contains(line, "No changes") || strings.Contains(line, "Infrastructure is up-to-date") {
				return strings.TrimSpace(line)
			}
			if strings.Contains(line, "Error:") {
				// Return error messages if that's all we have
				errorLines := []string{}
				capturing := false
				for _, l := range lines {
					if strings.Contains(l, "Error:") {
						capturing = true
					}
					if capturing && !strings.Contains(l, "[terragrunt]") {
						errorLines = append(errorLines, l)
					}
				}
				return strings.Join(errorLines, "\n")
			}
		}
		// If still nothing, return a minimal message
		return "No meaningful changes detected in the output."
	}

	return strings.TrimSpace(strings.Join(cleanLines, "\n"))
}

func parseResourceChanges(output string) *ResourceChanges {
	changes := &ResourceChanges{}

	// Standard Plan summary format (both Terraform and OpenTofu)
	// First check if imports are mentioned
	planWithImportRegex := regexp.MustCompile(`Plan: (\d+) to import, (\d+) to add, (\d+) to change, (\d+) to destroy`)
	if matches := planWithImportRegex.FindStringSubmatch(output); len(matches) > 0 {
		changes.ToImport, _ = strconv.Atoi(matches[1])
		changes.ToAdd, _ = strconv.Atoi(matches[2])
		changes.ToChange, _ = strconv.Atoi(matches[3])
		changes.ToDestroy, _ = strconv.Atoi(matches[4])
	} else {
		// Standard format without imports
		planSummaryRegex := regexp.MustCompile(`Plan: (\d+) to add, (\d+) to change, (\d+) to destroy`)
		if matches := planSummaryRegex.FindStringSubmatch(output); len(matches) > 0 {
			changes.ToAdd, _ = strconv.Atoi(matches[1])
			changes.ToChange, _ = strconv.Atoi(matches[2])
			changes.ToDestroy, _ = strconv.Atoi(matches[3])
		}
	}

	// Alternative format with "will be"
	addRegex := regexp.MustCompile(`(\d+) (?:resource|resources) will be (?:created|added)`)
	if matches := addRegex.FindStringSubmatch(output); len(matches) > 0 {
		changes.ToAdd, _ = strconv.Atoi(matches[1])
	}

	changeRegex := regexp.MustCompile(`(\d+) (?:resource|resources) will be (?:updated|changed|modified)`)
	if matches := changeRegex.FindStringSubmatch(output); len(matches) > 0 {
		changes.ToChange, _ = strconv.Atoi(matches[1])
	}

	destroyRegex := regexp.MustCompile(`(\d+) (?:resource|resources) will be (?:destroyed|deleted)`)
	if matches := destroyRegex.FindStringSubmatch(output); len(matches) > 0 {
		changes.ToDestroy, _ = strconv.Atoi(matches[1])
	}

	// Import and move operations
	importRegex := regexp.MustCompile(`(\d+) to import`)
	if matches := importRegex.FindStringSubmatch(output); len(matches) > 0 {
		changes.ToImport, _ = strconv.Atoi(matches[1])
	}

	moveRegex := regexp.MustCompile(`(\d+) to move`)
	if matches := moveRegex.FindStringSubmatch(output); len(matches) > 0 {
		changes.ToMove, _ = strconv.Atoi(matches[1])
	}

	// Count resources by looking for specific markers in the output
	if changes.ToAdd == 0 && changes.ToChange == 0 && changes.ToDestroy == 0 {
		// Count by looking for resource markers
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.Contains(line, "will be created") || strings.Contains(line, "will be added") {
				changes.ToAdd++
			} else if strings.Contains(line, "will be updated") || strings.Contains(line, "will be changed") ||
				strings.Contains(line, "will be modified") {
				changes.ToChange++
			} else if strings.Contains(line, "will be destroyed") || strings.Contains(line, "will be deleted") {
				changes.ToDestroy++
			}
		}
	}

	// Check for no changes
	noChangePatterns := []string{
		"No changes",
		"Infrastructure is up-to-date",
		"Your infrastructure matches",
		"0 to add, 0 to change, 0 to destroy",
	}

	for _, pattern := range noChangePatterns {
		if strings.Contains(output, pattern) {
			changes.NoChanges = true
			break
		}
	}

	// If we have no counted changes and didn't explicitly find "no changes", check totals
	if changes.ToAdd == 0 && changes.ToChange == 0 && changes.ToDestroy == 0 &&
		changes.ToImport == 0 && changes.ToMove == 0 && !changes.NoChanges {
		// Only set NoChanges if output is not empty and doesn't contain change indicators
		if output != "" && !strings.Contains(output, "will be") && !strings.Contains(output, "must be") {
			changes.NoChanges = true
		}
	}

	return changes
}

func postComments(ctx context.Context, client *github.Client, results []ExecutionResult) error {
	parts := strings.Split(config.Repository, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repository format")
	}
	owner, repo := parts[0], parts[1]

	for _, result := range results {
		if err := postResultComments(ctx, client, owner, repo, result); err != nil {
			logger.Error("Failed to post comment", "folder", result.Folder, "error", err)
		}
	}

	return nil
}

func postResultComments(ctx context.Context, client *github.Client, owner, repo string, result ExecutionResult) error {
	header := formatCommentHeader(result)
	content := result.Output

	// Format content in a collapsible details section
	detailsTitle := "View Output"
	if !result.Success {
		detailsTitle = "View Error Details"
	}

	if len(header)+len(content) <= maxCommentSize-headerSize {
		body := header + "\n\n<details>\n<summary><b>" + detailsTitle + "</b></summary>\n\n```hcl\n" + content + "\n```\n\n</details>"
		return createComment(ctx, client, owner, repo, body)
	}

	// For large outputs that need splitting
	chunks := splitContent(content, maxCommentSize-headerSize-300)
	for i, chunk := range chunks {
		partHeader := formatCommentHeaderWithPart(result, i+1, len(chunks))
		partTitle := fmt.Sprintf("%s (Part %d/%d)", detailsTitle, i+1, len(chunks))
		body := partHeader + "\n\n<details>\n<summary><b>" + partTitle + "</b></summary>\n\n```hcl\n" + chunk + "\n```\n\n</details>"
		if err := createComment(ctx, client, owner, repo, body); err != nil {
			return err
		}
	}

	return nil
}

func formatCommentHeader(result ExecutionResult) string {
	status := "✅"
	statusText := "Success"
	if !result.Success {
		status = "❌"
		statusText = "Failed"
	}

	header := fmt.Sprintf("## %s Terragrunt Execution: `%s`\n", status, result.Folder)
	header += fmt.Sprintf("**Status:** %s\n", statusText)
	header += fmt.Sprintf("**Command:** `terragrunt %s`\n", config.Command)

	if result.ResourceChanges != nil && !result.ResourceChanges.NoChanges {
		header += formatResourceChanges(result.ResourceChanges)
	}

	return header
}

func formatCommentHeaderWithPart(result ExecutionResult, part, total int) string {
	status := "✅"
	statusText := "Success"
	if !result.Success {
		status = "❌"
		statusText = "Failed"
	}

	header := fmt.Sprintf("## %s Terragrunt Execution: `%s` (%d/%d)\n", status, result.Folder, part, total)
	header += fmt.Sprintf("**Status:** %s\n", statusText)
	header += fmt.Sprintf("**Command:** `terragrunt %s`\n", config.Command)

	if part == 1 && result.ResourceChanges != nil && !result.ResourceChanges.NoChanges {
		header += formatResourceChanges(result.ResourceChanges)
	}

	return header
}

func formatResourceChanges(changes *ResourceChanges) string {
	if changes.NoChanges {
		return "**Changes:** No changes required\n"
	}

	parts := []string{}
	if changes.ToAdd > 0 {
		parts = append(parts, fmt.Sprintf("+%d to add", changes.ToAdd))
	}
	if changes.ToChange > 0 {
		parts = append(parts, fmt.Sprintf("~%d to change", changes.ToChange))
	}
	if changes.ToDestroy > 0 {
		parts = append(parts, fmt.Sprintf("-%d to destroy", changes.ToDestroy))
	}
	if changes.ToImport > 0 {
		parts = append(parts, fmt.Sprintf("↓%d to import", changes.ToImport))
	}
	if changes.ToMove > 0 {
		parts = append(parts, fmt.Sprintf("→%d to move", changes.ToMove))
	}

	if len(parts) > 0 {
		return fmt.Sprintf("**Changes:** %s\n", strings.Join(parts, ", "))
	}
	return ""
}

func splitContent(content string, maxSize int) []string {
	var chunks []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, maxSize*2), maxSize*2)

	var currentChunk strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		lineLen := len(line) + 1

		if currentChunk.Len()+lineLen > maxSize && currentChunk.Len() > 0 {
			chunks = append(chunks, currentChunk.String())
			currentChunk.Reset()
		}

		currentChunk.WriteString(line)
		currentChunk.WriteString("\n")
	}

	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	if len(chunks) == 0 && content != "" {
		for i := 0; i < len(content); i += maxSize {
			end := min(i+maxSize, len(content))
			chunks = append(chunks, content[i:end])
		}
	}

	return chunks
}

func postSummary(ctx context.Context, client *github.Client, results []ExecutionResult) error {
	parts := strings.Split(config.Repository, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repository format")
	}
	owner, repo := parts[0], parts[1]

	summary := formatSummary(results)
	return createComment(ctx, client, owner, repo, summary)
}

func formatSummary(results []ExecutionResult) string {
	var summary strings.Builder

	summary.WriteString("## 📊 Terragrunt Execution Summary\n\n")
	summary.WriteString(fmt.Sprintf("**Command:** `terragrunt %s`\n", config.Command))
	summary.WriteString(fmt.Sprintf("**Total Folders:** %d\n\n", len(results)))

	successCount := 0
	failureCount := 0
	totalAdd := 0
	totalChange := 0
	totalDestroy := 0
	totalImport := 0
	totalMove := 0
	noChangeCount := 0

	summary.WriteString("### Results by Folder\n\n")
	summary.WriteString("| Folder | Status | Resources to Add | Resources to Change | Resources to Destroy | Resources to Import | Resources to Move |\n")
	summary.WriteString("|--------|--------|-----------------|---------------------|---------------------|---------------------|-------------------|\n")

	for _, result := range results {
		status := "✅ Success"
		if !result.Success {
			status = "❌ Failed"
			failureCount++
		} else {
			successCount++
		}

		addStr := "-"
		changeStr := "-"
		destroyStr := "-"
		importStr := "-"
		moveStr := "-"

		if result.ResourceChanges != nil {
			if result.ResourceChanges.NoChanges {
				noChangeCount++
				addStr = "0"
				changeStr = "0"
				destroyStr = "0"
			} else {
				if result.ResourceChanges.ToAdd > 0 {
					addStr = fmt.Sprintf("+%d", result.ResourceChanges.ToAdd)
					totalAdd += result.ResourceChanges.ToAdd
				} else {
					addStr = "0"
				}
				if result.ResourceChanges.ToChange > 0 {
					changeStr = fmt.Sprintf("~%d", result.ResourceChanges.ToChange)
					totalChange += result.ResourceChanges.ToChange
				} else {
					changeStr = "0"
				}
				if result.ResourceChanges.ToDestroy > 0 {
					destroyStr = fmt.Sprintf("-%d", result.ResourceChanges.ToDestroy)
					totalDestroy += result.ResourceChanges.ToDestroy
				} else {
					destroyStr = "0"
				}
				if result.ResourceChanges.ToImport > 0 {
					importStr = fmt.Sprintf("↓%d", result.ResourceChanges.ToImport)
					totalImport += result.ResourceChanges.ToImport
				}
				if result.ResourceChanges.ToMove > 0 {
					moveStr = fmt.Sprintf("→%d", result.ResourceChanges.ToMove)
					totalMove += result.ResourceChanges.ToMove
				}
			}
		}

		summary.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | %s | %s | %s |\n",
			result.Folder, status, addStr, changeStr, destroyStr, importStr, moveStr))
	}

	summary.WriteString("\n### Overall Statistics\n\n")
	summary.WriteString(fmt.Sprintf("- **Successful Executions:** %d/%d\n", successCount, len(results)))
	summary.WriteString(fmt.Sprintf("- **Failed Executions:** %d/%d\n", failureCount, len(results)))
	summary.WriteString(fmt.Sprintf("- **Folders with No Changes:** %d/%d\n", noChangeCount, len(results)))

	if totalAdd > 0 || totalChange > 0 || totalDestroy > 0 || totalImport > 0 || totalMove > 0 {
		summary.WriteString("\n### Total Resource Changes\n\n")
		if totalAdd > 0 {
			summary.WriteString(fmt.Sprintf("- **Resources to Add:** +%d\n", totalAdd))
		}
		if totalChange > 0 {
			summary.WriteString(fmt.Sprintf("- **Resources to Change:** ~%d\n", totalChange))
		}
		if totalDestroy > 0 {
			summary.WriteString(fmt.Sprintf("- **Resources to Destroy:** -%d\n", totalDestroy))
		}
		if totalImport > 0 {
			summary.WriteString(fmt.Sprintf("- **Resources to Import:** ↓%d\n", totalImport))
		}
		if totalMove > 0 {
			summary.WriteString(fmt.Sprintf("- **Resources to Move:** →%d\n", totalMove))
		}
	}

	summary.WriteString("\n---\n")
	summary.WriteString("*Generated by Terragrunt Runner Action*\n")

	return summary.String()
}

func createComment(ctx context.Context, client *github.Client, owner, repo, body string) error {
	comment := &github.IssueComment{Body: &body}
	_, _, err := client.Issues.CreateComment(ctx, owner, repo, config.PullRequest, comment)
	return err
}

func detectTerragruntFolders() []string {
	foundFolders := make(map[string]bool)

	// If no changed files provided, try to get them from git
	if len(config.ChangedFiles) == 0 && config.AutoDetect {
		changedFiles := getChangedFilesFromGit()
		if len(changedFiles) > 0 {
			config.ChangedFiles = changedFiles
		}
	}

	for _, file := range config.ChangedFiles {
		// Check if file matches any of the patterns
		if !matchesPatterns(file, config.FilePatterns) {
			continue
		}

		// Find the nearest terragrunt.hcl by walking up
		terragruntDir := findTerragruntDirectory(file)
		if terragruntDir != "" {
			foundFolders[terragruntDir] = true
		}
	}

	// Convert map to slice
	var result []string
	for folder := range foundFolders {
		result = append(result, folder)
	}

	return result
}

func getChangedFilesFromGit() []string {
	var changedFiles []string

	// Helper function to safely execute git commands
	executeGitCommand := func(args ...string) ([]string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = "."
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		output, err := cmd.Output()
		if err != nil {
			logger.Debug("Git command failed",
				"command", strings.Join(args, " "),
				"error", err,
				"stderr", stderr.String())
			return nil, err
		}

		var files []string
		for _, file := range strings.Split(string(output), "\n") {
			if file = strings.TrimSpace(file); file != "" {
				files = append(files, file)
			}
		}
		return files, nil
	}

	// Try different git diff strategies
	strategies := [][]string{
		// For PR context: compare against base branch
		{"diff", "--name-only", "origin/main...HEAD"},
		{"diff", "--name-only", "origin/master...HEAD"},
		// For local development: compare against previous commit
		{"diff", "--name-only", "HEAD~1"},
		// Uncommitted changes
		{"diff", "--name-only"},
		// Staged changes
		{"diff", "--cached", "--name-only"},
	}

	for _, args := range strategies {
		files, err := executeGitCommand(args...)
		if err == nil && len(files) > 0 {
			changedFiles = append(changedFiles, files...)
		}
	}

	if len(changedFiles) == 0 {
		logger.Warn("No changed files detected from git")
	}

	return uniqueStrings(changedFiles)
}

func matchesPatterns(file string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, filepath.Base(file))
		if err == nil && matched {
			return true
		}
	}
	return false
}

func findTerragruntDirectory(filePath string) string {
	// Start from the file's directory
	dir := filepath.Dir(filePath)
	levelsWalked := 0

	for {
		// Check if terragrunt.hcl exists in this directory
		terragruntPath := filepath.Join(dir, config.TerragruntFile)
		if _, err := os.Stat(terragruntPath); err == nil {
			return dir
		}

		// Check if we've reached the maximum walk-up levels
		levelsWalked++
		if levelsWalked >= config.MaxWalkUpLevels {
			logger.Debug("Maximum walk-up levels reached",
				"file", filePath,
				"maxLevels", config.MaxWalkUpLevels)
			break
		}

		// Move up one directory
		parentDir := filepath.Dir(dir)
		if parentDir == dir || parentDir == "/" || parentDir == "." {
			// Reached the root
			break
		}
		dir = parentDir
	}

	return ""
}

func uniqueFolders(folders []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, folder := range folders {
		// Normalize the path
		normalizedFolder := filepath.Clean(folder)
		if !seen[normalizedFolder] {
			seen[normalizedFolder] = true
			result = append(result, normalizedFolder)
		}
	}

	return result
}

func uniqueStrings(strings []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, str := range strings {
		if !seen[str] {
			seen[str] = true
			result = append(result, str)
		}
	}

	return result
}
