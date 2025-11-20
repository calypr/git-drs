//go:build ignore
// +build ignore

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/config"
)

const VERSION = "1.0.0"

type MigrationConfig struct {
	ProjectID  string
	RepoPath   string
	DryRun     bool
	OutputFile string
	Verbose    bool
}

type UUIDMapping struct {
	FilePath   string    `json:"file_path"`
	SHA256     string    `json:"sha256"`
	Size       int64     `json:"size"`
	LegacyUUID string    `json:"legacy_uuid"`
	NewUUID    string    `json:"new_uuid"`
	MigratedAt time.Time `json:"migrated_at"`
	Status     string    `json:"status"` // "created", "exists", "error"
	Error      string    `json:"error,omitempty"`
}

type MigrationReport struct {
	ProjectID     string        `json:"project_id"`
	StartTime     time.Time     `json:"start_time"`
	EndTime       time.Time     `json:"end_time"`
	DryRun        bool          `json:"dry_run"`
	TotalFiles    int           `json:"total_files"`
	Migrated      int           `json:"migrated"`
	Skipped       int           `json:"skipped"`
	Errors        int           `json:"errors"`
	UUIDMappings  []UUIDMapping `json:"uuid_mappings"`
	ExistingUUIDs int           `json:"existing_uuids"`
}

func main() {
	cfg := parseFlags()

	if err := validateConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Git-DRS UUID Migration Tool v%s\n", VERSION)
	fmt.Printf("Project: %s\n", cfg.ProjectID)
	fmt.Printf("Repository: %s\n", cfg.RepoPath)
	if cfg.DryRun {
		fmt.Println("DRY RUN MODE - No changes will be made to indexd")
	}
	fmt.Println()

	// Change to repo directory
	if err := os.Chdir(cfg.RepoPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error changing to repo directory: %v\n", err)
		os.Exit(1)
	}

	// Initialize indexd client
	var logger client.LoggerInterface = &client.NoOpLogger{}
	if cfg.Verbose {
		logger, _ = client.NewLogger("", false)
	}

	indexdClient, err := client.NewIndexDClient(logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing indexd client: %v\n", err)
		os.Exit(1)
	}

	// Run migration
	report, err := runMigration(cfg, indexdClient)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Migration failed: %v\n", err)
		os.Exit(1)
	}

	// Print summary
	printSummary(report)

	// Export report
	if cfg.OutputFile != "" {
		if err := exportReport(report, cfg.OutputFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error exporting report: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\nMigration report exported to: %s\n", cfg.OutputFile)
	}

	if report.Errors > 0 {
		os.Exit(1)
	}
}

func parseFlags() *MigrationConfig {
	cfg := &MigrationConfig{}

	flag.StringVar(&cfg.ProjectID, "project", "", "Project ID (format: program-project)")
	flag.StringVar(&cfg.RepoPath, "repo", ".", "Path to Git repository")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "Perform dry run without making changes")
	flag.StringVar(&cfg.OutputFile, "output", "", "Output file for migration report (JSON)")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Enable verbose logging")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Migrates Git-DRS projects from v1 UUIDs (project-based) to v2 UUIDs (path-based)\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  # Dry run\n")
		fmt.Fprintf(os.Stderr, "  %s --project gdc-mirror --repo /path/to/repo --dry-run --output report.json\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  # Actual migration\n")
		fmt.Fprintf(os.Stderr, "  %s --project gdc-mirror --repo /path/to/repo --output migration.json\n", os.Args[0])
	}

	flag.Parse()
	return cfg
}

func validateConfig(cfg *MigrationConfig) error {
	if cfg.ProjectID == "" {
		return fmt.Errorf("project ID is required (use --project)")
	}

	if !strings.Contains(cfg.ProjectID, "-") {
		return fmt.Errorf("project ID must be in format 'program-project', got: %s", cfg.ProjectID)
	}

	if _, err := os.Stat(cfg.RepoPath); os.IsNotExist(err) {
		return fmt.Errorf("repository path does not exist: %s", cfg.RepoPath)
	}

	// Check if it's a git repo
	gitDir := filepath.Join(cfg.RepoPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return fmt.Errorf("not a git repository: %s", cfg.RepoPath)
	}

	return nil
}

func runMigration(cfg *MigrationConfig, indexdClient client.ObjectStoreClient) (*MigrationReport, error) {
	report := &MigrationReport{
		ProjectID:    cfg.ProjectID,
		StartTime:    time.Now(),
		DryRun:       cfg.DryRun,
		UUIDMappings: make([]UUIDMapping, 0),
	}

	fmt.Println("Step 1: Discovering LFS files...")
	lfsFiles, err := getLFSFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to get LFS files: %w", err)
	}
	report.TotalFiles = len(lfsFiles)
	fmt.Printf("Found %d LFS files\n\n", len(lfsFiles))

	fmt.Println("Step 2: Querying existing indexd records...")
	existingRecords, err := getExistingRecords(indexdClient, cfg.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to get existing records: %w", err)
	}
	report.ExistingUUIDs = len(existingRecords)
	fmt.Printf("Found %d existing indexd records\n\n", len(existingRecords))

	// Create lookup map for existing records by SHA256
	existingBySHA := make(map[string][]client.OutputInfo)
	for _, record := range existingRecords {
		sha := record.Hashes.SHA256
		existingBySHA[sha] = append(existingBySHA[sha], record)
	}

	fmt.Println("Step 3: Creating v2 UUIDs and migration plan...")
	for i, file := range lfsFiles {
		if cfg.Verbose || (i+1)%100 == 0 {
			fmt.Printf("Processing file %d/%d: %s\n", i+1, len(lfsFiles), file.Name)
		}

		mapping := createUUIDMapping(file, existingBySHA, cfg.ProjectID)

		if !cfg.DryRun && mapping.Status == "created" {
			// Actually create the indexd record
			if err := createIndexdRecord(indexdClient, file, mapping, cfg.ProjectID); err != nil {
				mapping.Status = "error"
				mapping.Error = err.Error()
				report.Errors++
			} else {
				report.Migrated++
			}
		} else if mapping.Status == "created" {
			report.Migrated++ // Would be created if not dry-run
		} else if mapping.Status == "exists" {
			report.Skipped++
		} else {
			report.Errors++
		}

		report.UUIDMappings = append(report.UUIDMappings, mapping)
	}

	report.EndTime = time.Now()
	fmt.Println()
	return report, nil
}

func createUUIDMapping(file LFSFile, existingBySHA map[string][]client.OutputInfo, projectID string) UUIDMapping {
	mapping := UUIDMapping{
		FilePath:   file.Name,
		SHA256:     file.Oid,
		Size:       file.Size,
		NewUUID:    client.ComputeDeterministicUUID(file.Name, file.Oid, file.Size),
		MigratedAt: time.Now(),
		Status:     "created",
	}

	// Find legacy UUID if exists
	existing := existingBySHA[file.Oid]
	for _, record := range existing {
		// Check if this record belongs to our project
		expectedAuthz := fmt.Sprintf("/programs/%s", strings.ReplaceAll(projectID, "-", "/projects/"))
		for _, authz := range record.Authz {
			if authz == expectedAuthz {
				mapping.LegacyUUID = record.Did
				break
			}
		}
		if mapping.LegacyUUID != "" {
			break
		}
	}

	// Check if new UUID already exists
	for _, record := range existing {
		if record.Did == mapping.NewUUID {
			mapping.Status = "exists"
			return mapping
		}
	}

	return mapping
}

func createIndexdRecord(indexdClient client.ObjectStoreClient, file LFSFile, mapping UUIDMapping, projectID string) error {
	// Get config for bucket info
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	bucket := cfg.Servers.Gen3.Auth.Bucket
	if bucket == "" {
		return fmt.Errorf("bucket name not configured")
	}

	// Build authz string
	parts := strings.Split(projectID, "-")
	if len(parts) != 2 {
		return fmt.Errorf("invalid project ID format: %s", projectID)
	}
	authzStr := fmt.Sprintf("/programs/%s/projects/%s", parts[0], parts[1])

	// Construct file URL
	fileURL := fmt.Sprintf("s3://%s/%s/%s", bucket, mapping.NewUUID, file.Oid)

	// Create indexd record with migration metadata
	metadata := map[string]string{
		"uuid_version":   "v2",
		"migration_date": mapping.MigratedAt.Format(time.RFC3339),
		"canonical_path": file.Name,
	}
	if mapping.LegacyUUID != "" {
		metadata["legacy_uuid"] = mapping.LegacyUUID
	}

	indexdRecord := &client.IndexdRecord{
		Did:      mapping.NewUUID,
		FileName: file.Name,
		URLs:     []string{fileURL},
		Hashes:   client.HashInfo{SHA256: file.Oid},
		Size:     file.Size,
		Authz:    []string{authzStr},
		Metadata: metadata,
	}

	_, err = indexdClient.RegisterIndexdRecord(indexdRecord)
	if err != nil {
		return fmt.Errorf("failed to register indexd record: %w", err)
	}

	return nil
}

func printSummary(report *MigrationReport) {
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Println("MIGRATION SUMMARY")
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Printf("Project: %s\n", report.ProjectID)
	fmt.Printf("Duration: %s\n", report.EndTime.Sub(report.StartTime).Round(time.Second))
	if report.DryRun {
		fmt.Println("Mode: DRY RUN (no changes made)")
	} else {
		fmt.Println("Mode: LIVE MIGRATION")
	}
	fmt.Println()
	fmt.Printf("Total LFS files:           %d\n", report.TotalFiles)
	fmt.Printf("Existing indexd records:   %d\n", report.ExistingUUIDs)
	fmt.Println()
	fmt.Printf("Records to create:         %d\n", report.Migrated)
	fmt.Printf("Records already exist:     %d\n", report.Skipped)
	fmt.Printf("Errors:                    %d\n", report.Errors)
	fmt.Println()

	if report.Errors > 0 {
		fmt.Println("Files with errors:")
		for _, mapping := range report.UUIDMappings {
			if mapping.Status == "error" {
				fmt.Printf("  - %s: %s\n", mapping.FilePath, mapping.Error)
			}
		}
		fmt.Println()
	}

	successRate := 100.0
	if report.TotalFiles > 0 {
		successRate = float64(report.Migrated+report.Skipped) / float64(report.TotalFiles) * 100
	}
	fmt.Printf("Success rate: %.1f%%\n", successRate)
	fmt.Println("=" + strings.Repeat("=", 79))
}

func exportReport(report *MigrationReport, filename string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

// LFS file info from git lfs ls-files
type LFSFile struct {
	Name string
	Size int64
	Oid  string
}

type LFSLsOutput struct {
	Files []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		Oid  string `json:"oid"`
	} `json:"files"`
}

func getLFSFiles() ([]LFSFile, error) {
	cmd := exec.Command("git", "lfs", "ls-files", "--json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git lfs ls-files failed: %w", err)
	}

	var lfsOutput LFSLsOutput
	if err := json.Unmarshal(output, &lfsOutput); err != nil {
		return nil, fmt.Errorf("failed to parse LFS output: %w", err)
	}

	files := make([]LFSFile, len(lfsOutput.Files))
	for i, f := range lfsOutput.Files {
		files[i] = LFSFile{
			Name: f.Name,
			Size: f.Size,
			Oid:  f.Oid,
		}
	}

	return files, nil
}

func getExistingRecords(indexdClient client.ObjectStoreClient, projectID string) ([]client.OutputInfo, error) {
	records := make([]client.OutputInfo, 0)

	// Query indexd for all records in this project
	resultChan, err := indexdClient.ListObjectsByProject(projectID)
	if err != nil {
		return nil, err
	}

	for result := range resultChan {
		if result.Error != nil {
			return nil, result.Error
		}
		if result.Record != nil {
			records = append(records, *result.Record)
		}
	}

	return records, nil
}
