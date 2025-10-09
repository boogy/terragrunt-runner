package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/google/go-github/v75/github"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

const (
	maxCommentSize = 65536 // GitHub comment size limit
	headerSize     = 500   // Estimated size for headers and markdown
)

var botCommentHeaders = []string{
	"Terragrunt Execution",
	"Failed Terragrunt",
	"Terragrunt Summary",
	"Success Terragrunt",
	"✅ Success Terragrunt",
}

var (
	Reset   = "\033[0m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	Gray    = "\033[37m"
	White   = "\033[97m"
)

type Config struct {
	GithubToken       string   // GitHub token for API access
	Repository        string   // GitHub repository in "owner/repo" format
	Owner             string   // GitHub repository owner
	PullRequest       int      // Pull request number
	Folders           []string // List of folders to run Terragrunt in
	Command           string   // Terragrunt CLI command
	RunAllRootDir     string   // Run --all directory root
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
	rootCmd.Flags().StringVar(&config.Owner, "owner", os.Getenv("GITHUB_REPOSITORY_OWNER"), "GitHub repository owner (optional, extracted from repository if not set)")
	rootCmd.Flags().IntVar(&config.PullRequest, "pull-request", getPRNumber(), "Pull request number")
	rootCmd.Flags().StringVar(&foldersStr, "folders", "", "Folders to run Terragrunt in (comma, space, or newline separated)")
	rootCmd.Flags().StringVar(&config.Command, "command", "plan", "Terragrunt CLI command (e.g., 'plan', 'run --all plan')")
	rootCmd.Flags().StringVar(&config.RunAllRootDir, "root-dir", "live", "Run --all root directory from where to run terragrunt")
	rootCmd.Flags().StringVar(&config.TerragruntArgs, "args", "--non-interactive", "Additional Terragrunt arguments")
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
	pr, err := extractPullRequestNumber()
	if err == nil {
		return pr
	}
	return 0
}

func extractPullRequestNumber() (int, error) {
	github_event_file := "/github/workflow/event.json"
	file, err := os.ReadFile(github_event_file)
	if err != nil {
		fail(fmt.Sprintf("GitHub event payload not found in %s", github_event_file))
		return -1, err
	}

	var data any
	err = json.Unmarshal(file, &data)
	if err != nil {
		return -1, err
	}
	payload := data.(map[string]any)

	prNumber, err := strconv.Atoi(fmt.Sprintf("%v", payload["number"]))
	if err != nil {
		return 0, fmt.Errorf("not a valid PR")
	}
	return prNumber, nil
}

// Main execution function
func run(cmd *cobra.Command, args []string) error {
	setupLogging()
	fmt.Printf("\n\nTerragrunt Runner Version: %s, BuildTime: %s, Commit: %s\n", Version, BuildTime, Commit)

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

			fmt.Printf("Terragrunt execution failed for folder: %s\n", result.Folder)
			if result.Error != nil {
				fmt.Printf("Error: %v\n", result.Error)
			}
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
		fmt.Printf("::error::Missing required config: GithubToken=%t, Repository=%s, PullRequest=%d, Folders=%d\n",
			config.GithubToken == "", config.Repository, config.PullRequest, len(config.Folders))
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
			if comment.User == nil || !strings.Contains(*comment.User.Login, "[bot]") {
				continue
			}
			if comment.Body != nil && slices.ContainsFunc(botCommentHeaders, func(header string) bool {
				return strings.Contains(*comment.Body, header)
			}) {
				if _, err := client.Issues.DeleteComment(ctx, owner, repo, *comment.ID); err != nil {
					logger.Warn("Failed to delete comment", "id", *comment.ID, "error", err)
					// Continue; don't fail whole function on one delete error
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

// Execute Terragrunt commands based on configuration
func executeTerragrunt() []ExecutionResult {
	isRunAll := strings.Contains(config.Command, "--all") || strings.HasPrefix(config.Command, "run-all")

	if isRunAll {
		return executeTerragruntAll()
	} else {
		return executeTerragruntPerFolder()
	}
}

// getRepoRoot returns the absolute path of the current git repository root
func getRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}

	// Fallback: not a git repo or git not available
	fallback, ferr := os.Getwd()
	if ferr != nil {
		return "", fmt.Errorf("failed to get repo root and fallback: %v, %v", err, ferr)
	}

	fmt.Fprintf(os.Stderr, "Warning: could not determine git repo root, falling back to current dir: %s\n", fallback)
	return fallback, nil
}

// Execute Terragrunt with --all across multiple folders
func executeTerragruntAll() []ExecutionResult {
	// Set working directory to the repo root + specified root dir
	repoRoot, errF := getRepoRoot()
	if errF != nil {
		return []ExecutionResult{{Folder: ".", Error: fmt.Errorf("failed to determine run root: %w", errF), Success: false}}
	}
	absRunAllDir := filepath.Join(repoRoot, config.RunAllRootDir)

	cmdParts := strings.Fields(config.Command)
	// Replace old "run-all" with new "run --all"
	if cmdParts[0] == "run-all" {
		cmdParts = append([]string{"run", "--all"}, cmdParts[1:]...)
	}

	// Separate Terragrunt command parts and Terraform args if -- is present
	var terragruntBaseCmd, terragruntFlags, tfSubCmd, tfArgs []string
	foundSeparator := false

	// First, handle explicit -- separator
	for _, part := range cmdParts {
		if part == "--" {
			foundSeparator = true
			continue
		}
		if foundSeparator {
			tfArgs = append(tfArgs, part)
		} else {
			terragruntBaseCmd = append(terragruntBaseCmd, part)
		}
	}

	// If no separator and it's a multi-module command, extract the Terraform subcommand
	if !foundSeparator && len(terragruntBaseCmd) > 2 && terragruntBaseCmd[0] == "run" && terragruntBaseCmd[1] == "--all" {
		// Everything after "run --all" is the Terraform subcommand and args
		tfSubCmd = terragruntBaseCmd[2:]
		terragruntBaseCmd = terragruntBaseCmd[:2]
	}

	// Build Terragrunt-specific flags that go AFTER "run --all" but BEFORE the Terraform subcommand
	if config.MaxParallel > 0 {
		terragruntFlags = append(terragruntFlags, "--parallelism", strconv.Itoa(config.MaxParallel))
	}

	// Convert folder paths to be relative to absRunAllDir
	// This is critical because Terragrunt's --queue-include-dir expects paths relative
	// to the directory where terragrunt is executed (absRunAllDir).
	//
	// Example scenario:
	//   - absRunAllDir = /repo/live/accounts
	//   - folder = live/accounts/account1/baseline (from user input or auto-detect)
	//   - We need: account1/baseline (relative to absRunAllDir)
	//
	// Without this conversion, Terragrunt excludes all units because the paths don't match.
	for _, folder := range config.Folders {
		// Convert folder to absolute path first (if it's not already)
		absFolder := folder
		if !filepath.IsAbs(folder) {
			absFolder = filepath.Join(repoRoot, folder)
		}
		absFolder = filepath.Clean(absFolder)

		// Calculate relative path from absRunAllDir to the folder
		relPath, err := filepath.Rel(absRunAllDir, absFolder)
		if err != nil {
			// Fallback: try string manipulation if filepath.Rel fails
			relPath, _ = strings.CutPrefix(folder, config.RunAllRootDir+"/")
			relPath, _ = strings.CutPrefix(relPath, config.RunAllRootDir)
			relPath = strings.TrimPrefix(relPath, "/")
		}

		logger.Debug("Queue include dir", "original", folder, "absolute", absFolder, "relative", relPath, "runDir", absRunAllDir)
		terragruntFlags = append(terragruntFlags, "--queue-include-dir", relPath)
	}

	// Include external dependencies for all units
	terragruntFlags = append(terragruntFlags, "--queue-include-external")

	// Append additional Terragrunt args to terragruntFlags
	if config.TerragruntArgs != "" {
		sArgs, err := sanitizeArgs(config.TerragruntArgs)
		if err != nil {
			return []ExecutionResult{{Folder: ".", Error: err, Success: false}}
		}
		terragruntFlags = append(terragruntFlags, sArgs...)
	}

	// Note: We intentionally do NOT add -no-color flag to preserve color output
	// If users want to disable colors, they can add it via --args flag

	// Reassemble cmdParts in correct order:
	// terragrunt run --all [TERRAGRUNT_FLAGS] [TERRAFORM_SUBCOMMAND] -- [TERRAFORM_ARGS]
	cmdParts = terragruntBaseCmd                    // "run --all"
	cmdParts = append(cmdParts, terragruntFlags...) // "--parallelism 5 --queue-include-dir ..."
	cmdParts = append(cmdParts, tfSubCmd...)        // "plan"
	if len(tfArgs) > 0 {
		cmdParts = append(cmdParts, "--")      // separator
		cmdParts = append(cmdParts, tfArgs...) // terraform-specific args
	}

	// Debug: Print the command that will be executed
	logger.Info("Executing Terragrunt command", "args", cmdParts, "dir", absRunAllDir)

	cmd := exec.Command("terragrunt", cmdParts...)
	cmd.Dir = absRunAllDir
	cmd.Env = append(os.Environ(), "TF_IN_AUTOMATION=true", "TG_NON_INTERACTIVE=true")

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	output := stdout.String() + stderr.String()

	fmt.Println(Red + "#########################################################" + Reset)
	fmt.Printf("::group::Terragrunt run --all from %s\n", absRunAllDir)
	fmt.Print(output) // Print output with colors to console
	fmt.Println("::endgroup::")
	fmt.Println(Red + "#########################################################" + Reset)

	// Split output by module to get individual results per folder for summary table
	moduleOutputs := splitOutputByModule(output)
	results := []ExecutionResult{}
	var summaryOutput string

	// Create a map of parsed folder names to original folder names for cleaner display
	folderMap := make(map[string]string)
	for _, folder := range config.Folders {
		// Extract the part after root-dir for matching
		cleanName := strings.TrimPrefix(folder, config.RunAllRootDir+"/")
		cleanName = strings.TrimPrefix(cleanName, config.RunAllRootDir)
		cleanName = strings.TrimPrefix(cleanName, "/")
		folderMap[cleanName] = folder
	}

	// Track total changes across all modules
	totalChanges := &ResourceChanges{}

	for parsedFolder, modOutput := range moduleOutputs {
		// Handle special _summary entry separately
		if parsedFolder == "_summary" {
			summaryOutput = modOutput
			continue
		}

		// Use original folder name if we can find a match, otherwise use parsed name
		displayFolder := parsedFolder
		for clean, original := range folderMap {
			if strings.HasSuffix(parsedFolder, clean) || strings.HasSuffix(clean, parsedFolder) {
				displayFolder = original
				break
			}
		}

		// Strip ANSI codes only for PR comments (not for console)
		cleanOutput := stripAnsiCodes(modOutput)
		changes := parseResourceChanges(modOutput)
		success := err == nil && !strings.Contains(modOutput, "Error:")
		resultErr := err
		if success {
			resultErr = nil
		}

		// Accumulate total changes
		if changes != nil {
			totalChanges.ToAdd += changes.ToAdd
			totalChanges.ToChange += changes.ToChange
			totalChanges.ToDestroy += changes.ToDestroy
			totalChanges.ToReplace += changes.ToReplace
			if !changes.NoChanges {
				totalChanges.NoChanges = false
			}
		}

		results = append(results, ExecutionResult{
			Folder:          displayFolder,
			Output:          cleanOutput,
			Error:           resultErr,
			ResourceChanges: changes,
			Success:         success,
		})
	}

	// Append summary to the last result if available
	if summaryOutput != "" && len(results) > 0 {
		lastIdx := len(results) - 1
		results[lastIdx].Output = results[lastIdx].Output + "\n\n" + stripAnsiCodes(summaryOutput)
	}

	// Fallback if splitting failed - create results from full output
	if len(results) == 0 {
		cleanOutput := stripAnsiCodes(output)
		totalChanges = parseResourceChanges(output)
		success := err == nil

		// Create a result for each configured folder
		for _, folder := range config.Folders {
			results = append(results, ExecutionResult{
				Folder:          folder,
				Output:          cleanOutput,
				Error:           err,
				ResourceChanges: totalChanges,
				Success:         success,
			})
		}
	}

	// Prepend a summary result for the overall run --all operation
	// This shows the root-dir and total changes across all folders
	summaryResult := ExecutionResult{
		Folder:          config.RunAllRootDir,
		Output:          stripAnsiCodes(output),
		Error:           err,
		ResourceChanges: totalChanges,
		Success:         err == nil,
	}
	results = append([]ExecutionResult{summaryResult}, results...)

	return results
}

// Split Terragrunt output by module/folder
func splitOutputByModule(output string) map[string]string {
	moduleOutputs := make(map[string][]string)
	unmatchedLines := []string{} // Capture lines not associated with any module
	var currentModule string
	moduleEndMarkers := []string{
		"Releasing state lock",
		"❯❯ Run Summary",
		"Run Summary",
	}

	r := regexp.MustCompile(`^\[(.*?)\] (.*)$`)
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := scanner.Text()

		// Check if this line is a module end marker (like summary)
		isEndMarker := false
		for _, marker := range moduleEndMarkers {
			if strings.Contains(line, marker) {
				isEndMarker = true
				break
			}
		}

		// If we hit an end marker, clear current module so subsequent lines go to unmatched
		if isEndMarker {
			currentModule = ""
			unmatchedLines = append(unmatchedLines, line)
			continue
		}

		if match := r.FindStringSubmatch(line); match != nil {
			currentModule = match[1]
			moduleOutputs[currentModule] = append(moduleOutputs[currentModule], match[2])
		} else if currentModule != "" {
			moduleOutputs[currentModule] = append(moduleOutputs[currentModule], line)
		} else {
			// Capture lines that appear before any module or after all modules (like summary)
			unmatchedLines = append(unmatchedLines, line)
		}
	}

	result := make(map[string]string)
	for mod, lines := range moduleOutputs {
		result[mod] = strings.TrimSpace(strings.Join(lines, "\n"))
	}

	// Add unmatched output as a special entry if there's any meaningful content
	if len(unmatchedLines) > 0 {
		unmatchedText := strings.TrimSpace(strings.Join(unmatchedLines, "\n"))
		if unmatchedText != "" {
			result["_summary"] = unmatchedText
		}
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
	// Calculate absolute folder path correctly
	// If folder is already absolute, use it as-is
	// If folder is relative, join it with repo root (not current working directory)
	absFolder := folder
	if !filepath.IsAbs(folder) {
		repoRoot, err := getRepoRoot()
		if err != nil {
			return ExecutionResult{Folder: folder, Error: fmt.Errorf("failed to determine repo root: %w", err), Success: false}
		}
		absFolder = filepath.Join(repoRoot, folder)
	}
	absFolder = filepath.Clean(absFolder)

	logger.Debug("Execute in folder", "original", folder, "absolute", absFolder)

	cmdParts := strings.Fields(config.Command)
	if config.TerragruntArgs != "" {
		sArgs, err := sanitizeArgs(config.TerragruntArgs)
		if err != nil {
			return ExecutionResult{Folder: folder, Error: err, Success: false}
		}
		cmdParts = append(cmdParts, sArgs...)
	}

	// Note: We intentionally do NOT add -no-color flag to preserve color output
	// If users want to disable colors, they can add it via --args flag

	cmd := exec.Command("terragrunt", cmdParts...)
	cmd.Dir = absFolder
	cmd.Env = append(os.Environ(), "TF_IN_AUTOMATION=true", "TG_NON_INTERACTIVE=true")

	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	output := stdout.String() + stderr.String()
	fmt.Println() // empty line for easier read in the console log

	fmt.Println(Red + "#########################################################" + Reset)
	fmt.Printf("::group::Terragrunt in %s\n", folder)
	fmt.Print(output) // Print output with colors to console
	fmt.Println("::endgroup::")
	fmt.Println(Red + "#########################################################" + Reset)

	// Strip ANSI codes only for PR comments (not for console)
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

// stripAnsiCodes removes all ANSI escape sequences from a string
func stripAnsiCodes(s string) string {
	// Comprehensive ANSI escape sequence pattern that handles:
	// - Standard color codes: \x1b[...m or \033[...m
	// - CSI sequences: \x1b[...
	// - OSC sequences: \x1b]...
	// - Unicode replacement character followed by [: �[...m (corrupted ANSI)
	reAnsi := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[=>]|\033\[[0-9;]*[mGKHfABCDsuJSTlh]|�\[[0-9;]*[a-zA-Z]`)
	return reAnsi.ReplaceAllString(s, "")
}

// Extract relevant Terraform output, filtering noise
func extractTerraformOutput(raw string) string {
	// 1. Remove ANSI color codes but preserve all spacing
	cleaned := stripAnsiCodes(raw)

	// 2. Normalize line endings
	cleaned = strings.ReplaceAll(cleaned, "\r\n", "\n")

	lines := strings.Split(cleaned, "\n")
	var result []string
	capture := false
	includeOutputs := false
	planSeen := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		// Early detection: no changes
		if strings.Contains(lower, "no changes") {
			return "No changes detected."
		}

		// Start capturing when plan or apply section begins
		if strings.Contains(lower, "will perform the following actions") ||
			strings.Contains(lower, "used the selected providers to generate the following execution plan") {
			capture = true

			// append this line too instead of skipping it
			result = append(result, line)

			continue // don't append this line, start after
		}

		// Capture resource change lines before the plan summary
		if capture && !strings.HasPrefix(trimmed, "Plan:") {
			result = append(result, line)
		}

		// Capture plan summary only once
		if strings.HasPrefix(trimmed, "Plan:") && !planSeen {
			result = append(result, line)
			planSeen = true
			capture = false
			continue
		}

		// Keep capturing "Changes to Outputs" section after plan
		if strings.HasPrefix(trimmed, "Changes to Outputs:") {
			includeOutputs = true
			result = append(result, "") // blank line for spacing
			result = append(result, line)
			continue
		}

		// Capture lines inside Outputs section
		if includeOutputs {
			result = append(result, line)

			// Stop if state lock release or apply/destroy complete
			if strings.Contains(lower, "releasing state lock") ||
				strings.Contains(lower, "apply complete!") ||
				strings.Contains(lower, "destroy complete!") {
				break
			}
		}

		// Capture errors as well
		if strings.HasPrefix(trimmed, "Error:") {
			result = append(result, line)
			break
		}
	}

	// 3. Fallback — if nothing matched, take last 50 lines
	if len(result) == 0 {
		allLines := strings.Split(cleaned, "\n")
		n := len(allLines)
		if n > 50 {
			allLines = allLines[n-50:]
		}
		return strings.Join(allLines, "\n")
	}

	// 4. Return output exactly as formatted by Terraform/OpenTofu
	return strings.TrimRight(strings.Join(result, "\n"), "\n")
}

// Parse resource changes from Terragrunt output
func parseResourceChanges(output string) *ResourceChanges {
	output = stripAnsiCodes(output)

	changes := &ResourceChanges{}
	r := regexp.MustCompile(`Plan:\s+(\d+)\s+to\s+add,?\s+(\d+)\s+to\s+change,?\s+(\d+)\s+to\s+destroy`)
	m := r.FindStringSubmatch(output)
	if len(m) == 4 {
		changes.ToAdd, _ = strconv.Atoi(m[1])
		changes.ToChange, _ = strconv.Atoi(m[2])
		changes.ToDestroy, _ = strconv.Atoi(m[3])
	}

	if strings.Contains(output, "No changes") {
		changes.NoChanges = true
	}

	return changes
}

// Post individual comments for each execution result
func postComments(ctx context.Context, client *github.Client, results []ExecutionResult) error {
	parts := strings.Split(config.Repository, "/")
	owner, repo := parts[0], parts[1]

	// For run --all, only post the first result (overall summary)
	// Individual folder results are shown in the summary table only
	isRunAll := strings.Contains(config.Command, "--all") || strings.HasPrefix(config.Command, "run-all")
	commentsToPost := results
	if isRunAll && len(results) > 1 && results[0].Folder == config.RunAllRootDir {
		commentsToPost = results[:1] // Only post the first result (overall summary)
	}

	for _, result := range commentsToPost {
		header := formatCommentHeader(result)

		if result.ResourceChanges != nil && result.ResourceChanges.NoChanges {
			body := header + "\nNo Changes"
			if err := createComment(ctx, client, owner, repo, body); err != nil {
				return err
			}
			continue
		}

		content := result.Output

		detailsTitle := "View Output"
		if !result.Success {
			detailsTitle = "View Error Details"
			content = result.Error.Error()
		}

		if len(header)+len(content) <= maxCommentSize-headerSize {
			body := header + "\n\n<details><summary><b>" + detailsTitle + "</b></summary>\n\n```hcl\n" + content + "\n```\n</details>"
			if err := createComment(ctx, client, owner, repo, body); err != nil {
				return err
			}
		} else {
			chunks := splitContent(content, maxCommentSize-headerSize-300)
			for i, chunk := range chunks {
				partHeader := formatCommentHeaderWithPart(result, i+1, len(chunks))
				partTitle := fmt.Sprintf("%s (Part %d/%d)", detailsTitle, i+1, len(chunks))
				body := partHeader + "\n\n<details><summary><b>" + partTitle + "</b></summary>\n\n```hcl\n" + chunk + "\n```\n</details>"
				if err := createComment(ctx, client, owner, repo, body); err != nil {
					return err
				}
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

	// For run --all commands, show just the command instead of folder names
	isRunAll := strings.Contains(config.Command, "--all") || strings.HasPrefix(config.Command, "run-all")
	folderDisplay := result.Folder
	if isRunAll {
		folderDisplay = config.Command
	}

	header := fmt.Sprintf("## %s Terragrunt: %s\n", status, folderDisplay)
	if isRunAll {
		header += fmt.Sprintf("**Folder:** %s\n", result.Folder)
	}
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

	// For run --all, skip the first result (which is the overall summary)
	// and only show individual folder results in the table
	isRunAll := strings.Contains(config.Command, "--all") || strings.HasPrefix(config.Command, "run-all")
	tableResults := results
	if isRunAll && len(results) > 1 && results[0].Folder == config.RunAllRootDir {
		tableResults = results[1:]
	}

	b.WriteString("## Terragrunt Summary\n\n**Command:** " + config.Command + "\n**Folders:** " + fmt.Sprint(len(tableResults)) + "\n\n")

	b.WriteString("| Folder | Status | Add | Change | Destroy | Replace |\n|--------|--------|-----|--------|---------|---------|\n")
	success, noChange := 0, 0
	for _, r := range tableResults {
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

	b.WriteString(fmt.Sprintf("\n- Success: %d/%d\n- No Changes: %d\n", success, len(tableResults), noChange))
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

func fail(err string) {
	fmt.Printf("Error: %s\n", err)
	os.Exit(-1)
}
