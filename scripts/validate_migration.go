//go:build ignore
// +build ignore

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/calypr/git-drs/client"
)

const VERSION = "1.0.0"

type ValidationConfig struct {
	ProjectID   string
	MappingFile string
	Verbose     bool
}

type ValidationResult struct {
	FilePath     string `json:"file_path"`
	NewUUID      string `json:"new_uuid"`
	LegacyUUID   string `json:"legacy_uuid"`
	NewExists    bool   `json:"new_exists"`
	LegacyExists bool   `json:"legacy_exists"`
	BothDownload bool   `json:"both_downloadable"`
	Status       string `json:"status"` // "pass", "warn", "fail"
	Error        string `json:"error,omitempty"`
}

type ValidationReport struct {
	ProjectID      string             `json:"project_id"`
	ValidationTime time.Time          `json:"validation_time"`
	TotalFiles     int                `json:"total_files"`
	Passed         int                `json:"passed"`
	Warnings       int                `json:"warnings"`
	Failed         int                `json:"failed"`
	Results        []ValidationResult `json:"results"`
}

func main() {
	cfg := parseFlags()

	if err := validateConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Git-DRS Migration Validation Tool v%s\n", VERSION)
	fmt.Printf("Project: %s\n", cfg.ProjectID)
	fmt.Printf("Mapping file: %s\n", cfg.MappingFile)
	fmt.Println()

	// Load migration report
	mappings, err := loadMigrationReport(cfg.MappingFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading migration report: %v\n", err)
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

	// Run validation
	report, err := runValidation(cfg, indexdClient, mappings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Validation failed: %v\n", err)
		os.Exit(1)
	}

	// Print results
	printResults(report)

	if report.Failed > 0 {
		os.Exit(1)
	}
}

func parseFlags() *ValidationConfig {
	cfg := &ValidationConfig{}

	flag.StringVar(&cfg.ProjectID, "project", "", "Project ID (format: program-project)")
	flag.StringVar(&cfg.MappingFile, "mapping", "", "Path to migration report JSON file")
	flag.BoolVar(&cfg.Verbose, "verbose", false, "Enable verbose logging")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Validates a Git-DRS UUID migration by checking record existence and downloadability\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  %s --project gdc-mirror --mapping migration.json\n", os.Args[0])
	}

	flag.Parse()
	return cfg
}

func validateConfig(cfg *ValidationConfig) error {
	if cfg.ProjectID == "" {
		return fmt.Errorf("project ID is required (use --project)")
	}

	if cfg.MappingFile == "" {
		return fmt.Errorf("mapping file is required (use --mapping)")
	}

	if _, err := os.Stat(cfg.MappingFile); os.IsNotExist(err) {
		return fmt.Errorf("mapping file does not exist: %s", cfg.MappingFile)
	}

	return nil
}

func loadMigrationReport(filename string) ([]UUIDMapping, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var report MigrationReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}

	return report.UUIDMappings, nil
}

func runValidation(cfg *ValidationConfig, indexdClient client.ObjectStoreClient, mappings []UUIDMapping) (*ValidationReport, error) {
	report := &ValidationReport{
		ProjectID:      cfg.ProjectID,
		ValidationTime: time.Now(),
		TotalFiles:     len(mappings),
		Results:        make([]ValidationResult, 0),
	}

	fmt.Println("Validating migration...")
	for i, mapping := range mappings {
		if cfg.Verbose || (i+1)%100 == 0 {
			fmt.Printf("Validating %d/%d: %s\n", i+1, len(mappings), mapping.FilePath)
		}

		result := validateRecord(indexdClient, mapping)
		report.Results = append(report.Results, result)

		switch result.Status {
		case "pass":
			report.Passed++
		case "warn":
			report.Warnings++
		case "fail":
			report.Failed++
		}
	}

	return report, nil
}

func validateRecord(indexdClient client.ObjectStoreClient, mapping UUIDMapping) ValidationResult {
	result := ValidationResult{
		FilePath:   mapping.FilePath,
		NewUUID:    mapping.NewUUID,
		LegacyUUID: mapping.LegacyUUID,
		Status:     "pass",
	}

	// Check if new UUID exists in indexd
	if mapping.NewUUID != "" {
		_, err := indexdClient.GetObject(mapping.NewUUID)
		if err == nil {
			result.NewExists = true
		}
	}

	// Check if legacy UUID exists in indexd
	if mapping.LegacyUUID != "" {
		_, err := indexdClient.GetObject(mapping.LegacyUUID)
		if err == nil {
			result.LegacyExists = true
		}
	}

	// Test downloadability via SHA256 (this works for both UUIDs)
	downloadable := false
	if mapping.SHA256 != "" {
		_, err := indexdClient.GetDownloadURL(mapping.SHA256)
		if err == nil {
			downloadable = true
			result.BothDownload = true
		}
	}

	// Determine status
	if !result.NewExists {
		result.Status = "fail"
		result.Error = "New UUID not found in indexd"
	} else if mapping.LegacyUUID != "" && !result.LegacyExists {
		result.Status = "warn"
		result.Error = "Legacy UUID not found (may have been cleaned up)"
	} else if !downloadable {
		result.Status = "fail"
		result.Error = "File not downloadable"
	}

	return result
}

func printResults(report *ValidationReport) {
	fmt.Println()
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Println("VALIDATION RESULTS")
	fmt.Println("=" + strings.Repeat("=", 79))
	fmt.Printf("Project: %s\n", report.ProjectID)
	fmt.Printf("Validation time: %s\n", report.ValidationTime.Format(time.RFC3339))
	fmt.Println()
	fmt.Printf("Total files validated:     %d\n", report.TotalFiles)
	fmt.Printf("Passed:                    %d\n", report.Passed)
	fmt.Printf("Warnings:                  %d\n", report.Warnings)
	fmt.Printf("Failed:                    %d\n", report.Failed)
	fmt.Println()

	if report.Failed > 0 {
		fmt.Println("FAILURES:")
		for _, result := range report.Results {
			if result.Status == "fail" {
				fmt.Printf("  ✗ %s\n", result.FilePath)
				fmt.Printf("    Error: %s\n", result.Error)
				fmt.Printf("    New UUID: %s (exists: %v)\n", result.NewUUID, result.NewExists)
				if result.LegacyUUID != "" {
					fmt.Printf("    Legacy UUID: %s (exists: %v)\n", result.LegacyUUID, result.LegacyExists)
				}
			}
		}
		fmt.Println()
	}

	if report.Warnings > 0 {
		fmt.Println("WARNINGS:")
		for _, result := range report.Results {
			if result.Status == "warn" {
				fmt.Printf("  ⚠ %s\n", result.FilePath)
				fmt.Printf("    Warning: %s\n", result.Error)
			}
		}
		fmt.Println()
	}

	successRate := 0.0
	if report.TotalFiles > 0 {
		successRate = float64(report.Passed) / float64(report.TotalFiles) * 100
	}

	fmt.Printf("Success rate: %.1f%%\n", successRate)

	if report.Failed == 0 && report.Warnings == 0 {
		fmt.Println()
		fmt.Println("✓ Migration validation PASSED - All files accessible")
	} else if report.Failed == 0 {
		fmt.Println()
		fmt.Println("✓ Migration validation PASSED with warnings")
	} else {
		fmt.Println()
		fmt.Println("✗ Migration validation FAILED - See errors above")
	}
	fmt.Println("=" + strings.Repeat("=", 79))
}

// Reuse types from migration script
type UUIDMapping struct {
	FilePath   string    `json:"file_path"`
	SHA256     string    `json:"sha256"`
	Size       int64     `json:"size"`
	LegacyUUID string    `json:"legacy_uuid"`
	NewUUID    string    `json:"new_uuid"`
	MigratedAt time.Time `json:"migrated_at"`
	Status     string    `json:"status"`
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
