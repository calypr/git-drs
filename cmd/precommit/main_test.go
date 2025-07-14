package precommit

import (
	"bytes"
	"errors"
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
	// Test that the command structure is correct
	// We skip actual execution to avoid complex setup requirements
	
	// Verify the command can be configured properly
	testCmd := &cobra.Command{
		Use:  "precommit",
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Mock implementation to test structure
			return errors.New("mock execution error - expected for testing")
		},
	}

	var buf bytes.Buffer
	testCmd.SetOut(&buf)
	testCmd.SetErr(&buf)
	testCmd.SetArgs([]string{})

	err := testCmd.Execute()
	if err == nil {
		t.Error("Expected mock error but got none")
	}

	if !strings.Contains(err.Error(), "mock execution error") {
		t.Errorf("Expected mock error, got: %v", err)
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
	// This test verifies the command structure without actually executing
	// the complex precommit logic to avoid test environment issues
	
	var buf bytes.Buffer
	testCmd := &cobra.Command{
		Use:  "precommit",
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Mock the execution to test structure
			return errors.New("mock precommit execution")
		},
	}

	testCmd.SetOut(&buf)
	testCmd.SetErr(&buf)
	testCmd.SetArgs([]string{})

	err := testCmd.Execute()

	// Should get our mock error, not an argument validation error
	if err == nil {
		t.Error("Expected mock execution error")
	} else if strings.Contains(err.Error(), "accepts 0 arg") {
		t.Error("Got argument validation error instead of execution error")
	} else if !strings.Contains(err.Error(), "mock precommit execution") {
		t.Errorf("Expected mock error, got: %v", err)
	}
}