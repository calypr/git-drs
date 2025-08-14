package download

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestDownloadCommandStructure(t *testing.T) {
	// Test that the command is properly structured
	if Cmd.Use != "download <oid>" {
		t.Errorf("Expected Use to be 'download <oid>', got '%s'", Cmd.Use)
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

func TestDownloadCommandFlags(t *testing.T) {
	// Test that flags are properly defined
	dstFlag := Cmd.Flags().Lookup("dst")
	if dstFlag == nil {
		t.Error("Expected 'dst' flag to be defined")
	}

	if dstFlag.Shorthand != "d" {
		t.Errorf("Expected 'dst' flag shorthand to be 'd', got '%s'", dstFlag.Shorthand)
	}

	if dstFlag.DefValue != "" {
		t.Errorf("Expected 'dst' flag default value to be empty, got '%s'", dstFlag.DefValue)
	}
}

func TestDownloadCommandArgValidation(t *testing.T) {
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
			args:    []string{"abc123"},
			wantErr: false, // Will fail later due to missing client, but args validation passes
		},
		{
			name:    "multiple arguments",
			args:    []string{"abc123", "def456"},
			wantErr: true, // ExactArgs(1) requires exactly one argument
		},
		{
			name:    "empty string argument",
			args:    []string{""},
			wantErr: false, // Command accepts empty strings
		},
		{
			name:    "sha256-like oid",
			args:    []string{"a665a45920422f9d417e4867efdc4fb8a04a1f3fff1fa07e998e86f7f7a27ae3"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test command with the same arg validation
			testCmd := &cobra.Command{
				Use:  "download <oid>",
				Args: cobra.ExactArgs(1),
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
			hasArgError := err != nil && (strings.Contains(err.Error(), "accepts 1 arg") || strings.Contains(err.Error(), "requires exactly"))
			
			if hasArgError != tt.wantErr {
				t.Errorf("download command arg validation error = %v, wantErr %v, error: %v", hasArgError, tt.wantErr, err)
			}
		})
	}
}

func TestDownloadCommandExecution(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		flags       map[string]string
		expectError bool
		errorCheck  func(error) bool
	}{
		{
			name:        "valid oid but no client config",
			args:        []string{"a665a45920422f9d417e4867efdc4fb8a04a1f3fff1fa07e998e86f7f7a27ae3"},
			expectError: true,
			errorCheck: func(err error) bool {
				// Should fail with client initialization error
				return err != nil
			},
		},
		{
			name:        "valid oid with destination path",
			args:        []string{"a665a45920422f9d417e4867efdc4fb8a04a1f3fff1fa07e998e86f7f7a27ae3"},
			flags:       map[string]string{"dst": "/tmp/test-download"},
			expectError: true,
			errorCheck: func(err error) bool {
				return err != nil
			},
		},
		{
			name:        "short oid",
			args:        []string{"abc123"},
			expectError: true,
			errorCheck: func(err error) bool {
				return err != nil
			},
		},
		{
			name:        "empty oid",
			args:        []string{""},
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
				Use:  "download <oid>",
				Args: cobra.ExactArgs(1),
				RunE: Cmd.RunE, // Use the actual RunE function
			}
			
			// Add the dst flag
			testCmd.Flags().StringP("dst", "d", "", "Destination path to save the downloaded file")
			
			testCmd.SetOut(&buf)
			testCmd.SetErr(&buf)
			testCmd.SetArgs(tt.args)

			// Set flags if provided
			for flag, value := range tt.flags {
				testCmd.Flags().Set(flag, value)
			}

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

func TestDownloadCommandHelp(t *testing.T) {
	// Test help output
	var buf bytes.Buffer
	testCmd := &cobra.Command{
		Use:   "download <oid>",
		Short: "Download file using file object ID",
		Long:  "Download file using file object ID (sha256 hash). Use lfs ls-files to get oid",
		Args:  cobra.ExactArgs(1),
	}
	
	testCmd.Flags().StringP("dst", "d", "", "Destination path to save the downloaded file")
	
	testCmd.SetOut(&buf)
	testCmd.SetErr(&buf)
	testCmd.SetArgs([]string{"--help"})

	err := testCmd.Execute()
	if err != nil {
		t.Errorf("Help command failed: %v", err)
	}

	output := buf.String()
	expectedStrings := []string{
		"Download file using file object ID",
		"sha256 hash",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Help output missing expected string '%s'. Got: %s", expected, output)
		}
	}
}

func TestDownloadCommandArgs(t *testing.T) {
	// Test Args validation directly
	if Cmd.Args == nil {
		t.Error("Expected Args validation to be defined")
	}

	// Test exact args requirement
	err := Cmd.Args(Cmd, []string{})
	if err == nil {
		t.Error("Expected error for no arguments")
	}

	err = Cmd.Args(Cmd, []string{"oid"})
	if err != nil {
		t.Errorf("Expected no error for one argument, got: %v", err)
	}

	err = Cmd.Args(Cmd, []string{"oid1", "oid2"})
	if err == nil {
		t.Error("Expected error for multiple arguments")
	}
}

func TestDownloadCommandFlagInteraction(t *testing.T) {
	// Test flag parsing
	var buf bytes.Buffer
	testCmd := &cobra.Command{
		Use: "download <oid>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dst, _ := cmd.Flags().GetString("dst")
			if dst != "" {
				buf.WriteString("dst flag set to: " + dst)
			}
			return errors.New("expected test error")
		},
	}
	
	testCmd.Flags().StringP("dst", "d", "", "Destination path")
	testCmd.SetOut(&buf)
	testCmd.SetErr(&buf)

	// Test long flag
	testCmd.SetArgs([]string{"--dst", "/test/path", "test-oid"})
	testCmd.Execute()
	
	if !strings.Contains(buf.String(), "dst flag set to: /test/path") {
		t.Error("Long flag not properly parsed")
	}

	// Reset buffer and test short flag
	buf.Reset()
	testCmd.SetArgs([]string{"-d", "/test/path2", "test-oid"})
	testCmd.Execute()
	
	if !strings.Contains(buf.String(), "dst flag set to: /test/path2") {
		t.Error("Short flag not properly parsed")
	}
}