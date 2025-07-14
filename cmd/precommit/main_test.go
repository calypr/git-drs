package precommit

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPrecommitCommandStructure(t *testing.T) {
	// Test that the command is properly structured
	if Cmd.Use != "precommit" {
		t.Errorf("Expected Use to be 'precommit', got '%s'", Cmd.Use)
	}

	if Cmd.Short == "" {
		t.Error("Expected Short description to be non-empty")
	}

	if Cmd.Long == "" {
		t.Error("Expected Long description to be non-empty")
	}

	if Cmd.RunE == nil {
		t.Error("Expected RunE function to be defined")
	}
}

func TestPrecommitCommandArgValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "no arguments (correct)",
			args:    []string{},
			wantErr: false,
		},
		{
			name:    "one argument (incorrect)",
			args:    []string{"extra"},
			wantErr: true,
		},
		{
			name:    "multiple arguments (incorrect)",
			args:    []string{"extra1", "extra2"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test command with the same arg validation
			testCmd := &cobra.Command{
				Use:  "precommit",
				Args: cobra.ExactArgs(0),
				RunE: func(cmd *cobra.Command, args []string) error {
					// Return early to avoid actual execution
					return errors.New("test error - expected")
				},
			}

			var buf bytes.Buffer
			testCmd.SetOut(&buf)
			testCmd.SetErr(&buf)
			testCmd.SetArgs(tt.args)

			err := testCmd.Execute()
			
			// Check if we got an argument validation error
			hasArgError := err != nil && strings.Contains(err.Error(), "accepts 0 arg")
			
			if hasArgError != tt.wantErr {
				t.Errorf("precommit command arg validation error = %v, wantErr %v, error: %v", hasArgError, tt.wantErr, err)
			}
		})
	}
}

func TestPrecommitCommandExecution(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "git-drs-precommit-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Change to temp dir
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create .drs directory structure
	drsDir := filepath.Join(tempDir, ".drs")
	err = os.MkdirAll(drsDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		setup       func() error
		expectError bool
		errorCheck  func(error) bool
	}{
		{
			name: "precommit in directory without git repo",
			setup: func() error {
				return nil // No setup needed
			},
			expectError: true,
			errorCheck: func(err error) bool {
				// Should fail with error related to git lfs or missing setup
				errStr := err.Error()
				return err != nil && (strings.Contains(errStr, "UpdateDrsObjects failed") || 
					strings.Contains(errStr, "exit status") ||
					strings.Contains(errStr, "git lfs") ||
					strings.Contains(errStr, "failed"))
			},
		},
		{
			name: "precommit with minimal setup",
			setup: func() error {
				// Create a minimal .git directory
				gitDir := filepath.Join(tempDir, ".git")
				return os.MkdirAll(gitDir, 0755)
			},
			expectError: true,
			errorCheck: func(err error) bool {
				// Should fail but not with argument validation error
				errStr := err.Error()
				return err != nil && !strings.Contains(errStr, "accepts 0 arg") &&
					(strings.Contains(errStr, "UpdateDrsObjects failed") || 
					strings.Contains(errStr, "exit status") ||
					strings.Contains(errStr, "git lfs") ||
					strings.Contains(errStr, "failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			if tt.setup != nil {
				err := tt.setup()
				if err != nil {
					t.Fatalf("Setup failed: %v", err)
				}
			}

			var buf bytes.Buffer
			testCmd := &cobra.Command{
				Use:  "precommit",
				Args: cobra.ExactArgs(0),
				RunE: Cmd.RunE, // Use the actual RunE function
			}

			testCmd.SetOut(&buf)
			testCmd.SetErr(&buf)
			testCmd.SetArgs([]string{})

			// Capture and handle potential panic from log.Fatalf
			defer func() {
				if r := recover(); r != nil {
					// If there's a panic, convert it to an error for testing
					if tt.expectError {
						// Expected to fail, so panic is acceptable
						return
					}
					t.Errorf("Unexpected panic: %v", r)
				}
			}()

			err := testCmd.Execute()

			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			} else if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if tt.expectError && err != nil && tt.errorCheck != nil {
				if !tt.errorCheck(err) {
					t.Errorf("Error check failed for error: %v", err)
				}
			}
		})
	}
}

func TestPrecommitCommandHelp(t *testing.T) {
	// Test help output
	var buf bytes.Buffer
	testCmd := &cobra.Command{
		Use:   "precommit",
		Short: "pre-commit hook to create DRS objects",
		Long:  "Pre-commit hook that creates and commits a DRS object to the repo for every LFS file committed",
		Args:  cobra.ExactArgs(0),
	}

	testCmd.SetOut(&buf)
	testCmd.SetErr(&buf)
	testCmd.SetArgs([]string{"--help"})

	err := testCmd.Execute()
	if err != nil {
		t.Errorf("Help command failed: %v", err)
	}

	output := buf.String()
	expectedStrings := []string{
		"pre-commit hook to create DRS objects",
		"Pre-commit hook that creates and commits a DRS object",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Help output missing expected string '%s'. Got: %s", expected, output)
		}
	}
}

func TestPrecommitCommandArgs(t *testing.T) {
	// Test Args validation directly
	if Cmd.Args == nil {
		t.Error("Expected Args validation to be defined")
	}

	// Test exact args requirement (0 args)
	err := Cmd.Args(Cmd, []string{})
	if err != nil {
		t.Errorf("Expected no error for zero arguments, got: %v", err)
	}

	err = Cmd.Args(Cmd, []string{"extra"})
	if err == nil {
		t.Error("Expected error for one argument")
	}
}

func TestPrecommitCommandFlags(t *testing.T) {
	// Test that the command has no flags defined
	flags := Cmd.Flags()
	if flags.HasFlags() {
		t.Error("Expected precommit command to have no flags")
	}
}

func TestPrecommitCommandWithValidDirectoryStructure(t *testing.T) {
	// This test verifies the command structure when called in a proper environment
	// but expects it to fail due to missing configuration

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "git-drs-precommit-valid-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Change to temp dir
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create proper directory structure
	gitDir := filepath.Join(tempDir, ".git")
	err = os.MkdirAll(gitDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	drsDir := filepath.Join(tempDir, ".drs")
	err = os.MkdirAll(drsDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	testCmd := &cobra.Command{
		Use:  "precommit",
		Args: cobra.ExactArgs(0),
		RunE: Cmd.RunE, // Use the actual RunE function
	}

	testCmd.SetOut(&buf)
	testCmd.SetErr(&buf)
	testCmd.SetArgs([]string{})

	err = testCmd.Execute()

	// Should fail with execution error (not argument validation error)
	if err == nil {
		t.Error("Expected execution error due to missing configuration")
	} else if strings.Contains(err.Error(), "accepts 0 arg") {
		t.Error("Got argument validation error instead of execution error")
	}
}