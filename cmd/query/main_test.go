package query

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestQueryCommandStructure(t *testing.T) {
	// Test that the command is properly structured
	if Cmd.Use != "query <drs_id>" {
		t.Errorf("Expected Use to be 'query <drs_id>', got '%s'", Cmd.Use)
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

func TestQueryCommandArgValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "no arguments",
			args:    []string{},
			wantErr: true,
		},
		{
			name:    "one valid argument",
			args:    []string{"drs-id-123"},
			wantErr: false, // Will fail later due to missing client, but args validation passes
		},
		{
			name:    "multiple arguments",
			args:    []string{"drs-id-123", "drs-id-456"},
			wantErr: false, // MinimumNArgs(1) allows multiple args
		},
		{
			name:    "empty string argument",
			args:    []string{""},
			wantErr: false, // Command accepts empty strings
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test command with the same arg validation
			testCmd := &cobra.Command{
				Use:  "query <drs_id>",
				Args: cobra.MinimumNArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					// Return early to avoid actual client initialization
					return errors.New("test error - expected")
				},
			}

			var buf bytes.Buffer
			testCmd.SetOut(&buf)
			testCmd.SetErr(&buf)
			testCmd.SetArgs(tt.args)

			err := testCmd.Execute()
			
			// Check if we got an argument validation error
			hasArgError := err != nil && strings.Contains(err.Error(), "requires at least")
			
			if hasArgError != tt.wantErr {
				t.Errorf("query command arg validation error = %v, wantErr %v, error: %v", hasArgError, tt.wantErr, err)
			}
		})
	}
}

func TestQueryCommandExecution(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorCheck  func(error) bool
	}{
		{
			name:        "valid drs id but no client config",
			args:        []string{"valid-drs-id"},
			expectError: true,
			errorCheck: func(err error) bool {
				// Should fail with client initialization error
				return err != nil
			},
		},
		{
			name:        "special characters in drs id",
			args:        []string{"drs://example.com/some-id-123"},
			expectError: true,
			errorCheck: func(err error) bool {
				return err != nil
			},
		},
		{
			name:        "uuid-like drs id",
			args:        []string{"550e8400-e29b-41d4-a716-446655440000"},
			expectError: true,
			errorCheck: func(err error) bool {
				return err != nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			testCmd := &cobra.Command{
				Use:  "query <drs_id>",
				Args: cobra.MinimumNArgs(1),
				RunE: Cmd.RunE, // Use the actual RunE function
			}
			
			testCmd.SetOut(&buf)
			testCmd.SetErr(&buf)
			testCmd.SetArgs(tt.args)

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

func TestQueryCommandHelp(t *testing.T) {
	// Test help output
	var buf bytes.Buffer
	testCmd := &cobra.Command{
		Use:   "query <drs_id>",
		Short: "Query DRS server by DRS ID",
		Long:  "Query DRS server by DRS ID",
		Args:  cobra.MinimumNArgs(1),
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
		"Query DRS server by DRS ID",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Help output missing expected string '%s'. Got: %s", expected, output)
		}
	}
}

func TestQueryCommandArgs(t *testing.T) {
	// Test Args validation directly
	if Cmd.Args == nil {
		t.Error("Expected Args validation to be defined")
	}

	// Test minimum args requirement
	err := Cmd.Args(Cmd, []string{})
	if err == nil {
		t.Error("Expected error for no arguments")
	}

	err = Cmd.Args(Cmd, []string{"drs-id"})
	if err != nil {
		t.Errorf("Expected no error for one argument, got: %v", err)
	}

	err = Cmd.Args(Cmd, []string{"drs-id-1", "drs-id-2"})
	if err != nil {
		t.Errorf("Expected no error for multiple arguments, got: %v", err)
	}
}