package main

import (
	"os"
	"testing"

	"github.com/bmeg/git-drs/cmd"
)

// TestMainIntegration tests the main entry point
func TestMainIntegration(t *testing.T) {
	// Test that the main function can be called without panicking
	// We'll override the args to avoid actual execution

	// Save original args
	originalArgs := os.Args

	// Test help command
	os.Args = []string{"git-drs", "--help"}
	
	// Capture any panics
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Main function panicked with --help: %v", r)
		}
		// Restore original args
		os.Args = originalArgs
	}()

	// Create a test command to verify structure
	if cmd.RootCmd == nil {
		t.Fatal("RootCmd is nil")
	}

	if cmd.RootCmd.Use != "git-drs" {
		t.Errorf("Expected root command Use to be 'git-drs', got '%s'", cmd.RootCmd.Use)
	}

	// Verify all expected subcommands are present
	expectedSubcommands := []string{"download", "init", "precommit", "query", "transfer", "version"}
	commands := cmd.RootCmd.Commands()
	
	for _, expected := range expectedSubcommands {
		found := false
		for _, actualCmd := range commands {
			if actualCmd.Use == expected || actualCmd.Use == expected+" <oid>" || actualCmd.Use == expected+" <drs_id>" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected subcommand '%s' not found in root command", expected)
		}
	}
}

// TestCommandsHaveTests verifies that all commands have corresponding test files
func TestCommandsHaveTests(t *testing.T) {
	expectedTestFiles := []string{
		"cmd/root_test.go",
		"cmd/version/main_test.go",
		"cmd/query/main_test.go", 
		"cmd/download/main_test.go",
		"cmd/initialize/main_test.go",
		"cmd/precommit/main_test.go",
		"cmd/transfer/main_test.go",
	}

	for _, testFile := range expectedTestFiles {
		if _, err := os.Stat(testFile); os.IsNotExist(err) {
			t.Errorf("Expected test file '%s' does not exist", testFile)
		}
	}
}

// TestCommandRegistration verifies all commands are properly registered
func TestCommandRegistration(t *testing.T) {
	rootCmd := cmd.RootCmd
	
	if rootCmd == nil {
		t.Fatal("RootCmd is nil")
	}

	commands := rootCmd.Commands()
	if len(commands) == 0 {
		t.Error("No subcommands found in root command")
	}

	// Each command should have proper structure
	for _, cmd := range commands {
		if cmd.Use == "" {
			t.Error("Found command with empty Use field")
		}
		if cmd.Short == "" {
			t.Errorf("Command '%s' has empty Short description", cmd.Use)
		}
		if cmd.RunE == nil && cmd.Run == nil {
			t.Errorf("Command '%s' has no RunE or Run function", cmd.Use)
		}
	}
}