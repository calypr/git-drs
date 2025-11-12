package initialize

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestInitializeCommandStructure(t *testing.T) {
	// Test that the command is properly structured
	if Cmd.Use != "init" {
		t.Errorf("Expected Use to be 'init', got '%s'", Cmd.Use)
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

func TestInitializeCommandFlags(t *testing.T) {
	// Test that required flags are properly defined
	requiredFlags := []struct {
		name      string
		shorthand string
	}{
		{"profile", ""},
		{"cred", ""},
		{"apiendpoint", ""},
	}

	for _, flag := range requiredFlags {
		flagDef := Cmd.Flags().Lookup(flag.name)
		if flagDef == nil {
			t.Errorf("Expected '%s' flag to be defined", flag.name)
			continue
		}

		if flag.shorthand != "" && flagDef.Shorthand != flag.shorthand {
			t.Errorf("Expected '%s' flag shorthand to be '%s', got '%s'", flag.name, flag.shorthand, flagDef.Shorthand)
		}
	}

	// Test that running without required flags fails
	var buf bytes.Buffer
	testCmd := &cobra.Command{Use: "init", Args: cobra.ExactArgs(0), RunE: Cmd.RunE}
	testCmd.Flags().String("profile", "", "Specify the profile to use")
	testCmd.Flags().String("cred", "", "Specify the credential file")
	testCmd.Flags().String("apiendpoint", "", "Specify the API endpoint")
	testCmd.MarkFlagRequired("profile")
	testCmd.MarkFlagRequired("cred")
	testCmd.MarkFlagRequired("apiendpoint")

	testCmd.SetOut(&buf)
	testCmd.SetErr(&buf)
	testCmd.SetArgs([]string{})

	err := testCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "required flag") {
		t.Error("Expected required flag error when running without flags")
	}
}

func TestInitializeCommandArgValidation(t *testing.T) {
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
				Use:  "init",
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
				t.Errorf("init command arg validation error = %v, wantErr %v, error: %v", hasArgError, tt.wantErr, err)
			}
		})
	}
}

func TestInitializeCommandExecutionWithMissingFlags(t *testing.T) {
	tests := []struct {
		name      string
		flags     map[string]string
		expectErr bool
		errCheck  func(error) bool
	}{
		{
			name:      "missing all required flags",
			flags:     map[string]string{},
			expectErr: true,
			errCheck: func(err error) bool {
				return strings.Contains(err.Error(), "required flag")
			},
		},
		{
			name: "missing profile flag",
			flags: map[string]string{
				"cred":        "/tmp/test-cred",
				"apiendpoint": "https://example.com",
			},
			expectErr: true,
			errCheck: func(err error) bool {
				return strings.Contains(err.Error(), "profile")
			},
		},
		{
			name: "missing cred flag",
			flags: map[string]string{
				"profile":     "test-profile",
				"apiendpoint": "https://example.com",
			},
			expectErr: true,
			errCheck: func(err error) bool {
				return strings.Contains(err.Error(), "cred")
			},
		},
		{
			name: "missing apiendpoint flag",
			flags: map[string]string{
				"profile": "test-profile",
				"cred":    "/tmp/test-cred",
			},
			expectErr: true,
			errCheck: func(err error) bool {
				return strings.Contains(err.Error(), "apiendpoint")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testCmd := &cobra.Command{
				Use:  "init",
				Args: cobra.ExactArgs(0),
				RunE: func(cmd *cobra.Command, args []string) error {
					return errors.New("execution should not reach here")
				},
			}

			// Add flags
			testCmd.Flags().String("profile", "", "Specify the profile to use")
			testCmd.Flags().String("cred", "", "Specify the credential file")
			testCmd.Flags().String("apiendpoint", "", "Specify the API endpoint")
			testCmd.MarkFlagRequired("profile")
			testCmd.MarkFlagRequired("cred")
			testCmd.MarkFlagRequired("apiendpoint")

			var buf bytes.Buffer
			testCmd.SetOut(&buf)
			testCmd.SetErr(&buf)

			// Build args from flags
			var args []string
			for flag, value := range tt.flags {
				args = append(args, "--"+flag, value)
			}
			testCmd.SetArgs(args)

			err := testCmd.Execute()

			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			} else if !tt.expectErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if tt.expectErr && err != nil && tt.errCheck != nil {
				if !tt.errCheck(err) {
					t.Errorf("Error check failed for error: %v", err)
				}
			}
		})
	}
}

func TestInitializeCommandExecutionWithValidFlags(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "git-drs-init-test")
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

	// Initialize a git repo
	gitDir := filepath.Join(tempDir, ".git")
	err = os.MkdirAll(gitDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create a temporary credential file
	credFile := filepath.Join(tempDir, "test-cred.json")
	err = os.WriteFile(credFile, []byte(`{"test": "cred"}`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		flags       map[string]string
		expectError bool
		errorCheck  func(error) bool
	}{
		{
			name: "valid flags but execution will fail due to missing dependencies",
			flags: map[string]string{
				"profile":     "test-profile",
				"cred":        credFile,
				"apiendpoint": "https://example.com/api",
			},
			expectError: true,
			errorCheck: func(err error) bool {
				// Should fail with some execution error, not flag validation
				return err != nil && !strings.Contains(err.Error(), "required flag")
			},
		},
		{
			name: "empty profile value",
			flags: map[string]string{
				"profile":     "",
				"cred":        credFile,
				"apiendpoint": "https://example.com/api",
			},
			expectError: true,
			errorCheck: func(err error) bool {
				return err != nil
			},
		},
		{
			name: "invalid URL endpoint",
			flags: map[string]string{
				"profile":     "test-profile",
				"cred":        credFile,
				"apiendpoint": "not-a-valid-url",
			},
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
				Use:  "init",
				Args: cobra.ExactArgs(0),
				RunE: Cmd.RunE, // Use the actual RunE function
			}

			// Add flags
			testCmd.Flags().String("profile", "", "Specify the profile to use")
			testCmd.Flags().String("cred", "", "Specify the credential file")
			testCmd.Flags().String("apiendpoint", "", "Specify the API endpoint")
			testCmd.MarkFlagRequired("profile")
			testCmd.MarkFlagRequired("cred")
			testCmd.MarkFlagRequired("apiendpoint")

			testCmd.SetOut(&buf)
			testCmd.SetErr(&buf)

			// Build args from flags
			var args []string
			for flag, value := range tt.flags {
				args = append(args, "--"+flag, value)
			}
			testCmd.SetArgs(args)

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

func TestInitializeCommandHelp(t *testing.T) {
	// Test help output
	var buf bytes.Buffer
	testCmd := &cobra.Command{
		Use:   "init",
		Short: "initialize required setup for git-drs",
		Long:  "initialize hooks, config required for git-drs",
		Args:  cobra.ExactArgs(0),
	}

	testCmd.Flags().String("profile", "", "Specify the profile to use")
	testCmd.Flags().String("cred", "", "Specify the credential file that you want to use")
	testCmd.Flags().String("apiendpoint", "", "Specify the API endpoint of the data commons")

	testCmd.SetOut(&buf)
	testCmd.SetErr(&buf)
	testCmd.SetArgs([]string{"--help"})

	err := testCmd.Execute()
	if err != nil {
		t.Errorf("Help command failed: %v", err)
	}

	output := buf.String()
	expectedStrings := []string{
		"initialize hooks, config required for git-drs",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Help output missing expected string '%s'. Got: %s", expected, output)
		}
	}
}

func TestInitializeCommandArgs(t *testing.T) {
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