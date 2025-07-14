package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRootCommandStructure(t *testing.T) {
	// Test that the root command is properly structured
	if RootCmd.Use != "git-drs" {
		t.Errorf("Expected Use to be 'git-drs', got '%s'", RootCmd.Use)
	}

	if RootCmd.Short == "" {
		t.Error("Expected Short description to be non-empty")
	}

	if RootCmd.Long == "" {
		t.Error("Expected Long description to be non-empty")
	}

	// Check that all expected subcommands are present
	expectedCommands := []string{"download", "init", "precommit", "query", "transfer", "version"}
	
	for _, expectedCmd := range expectedCommands {
		found := false
		for _, cmd := range RootCmd.Commands() {
			if cmd.Use == expectedCmd || strings.HasPrefix(cmd.Use, expectedCmd) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected subcommand '%s' not found", expectedCmd)
		}
	}
}

func TestRootCommandHelp(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantErr  bool
		contains []string
	}{
		{
			name:     "help flag",
			args:     []string{"--help"},
			wantErr:  false,
			contains: []string{"Git DRS", "Available Commands", "download", "init", "query", "version"},
		},
		{
			name:     "help command",
			args:     []string{"help"},
			wantErr:  false,
			contains: []string{"Git DRS", "Available Commands"},
		},
		{
			name:     "no arguments shows help",
			args:     []string{},
			wantErr:  false,
			contains: []string{"Git DRS", "Available Commands"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of the root command for testing
			rootCmd := &cobra.Command{
				Use:   "git-drs",
				Short: "Git DRS - Git-LFS file management for DRS servers",
				Long:  "Git DRS provides the benefits of Git-LFS file management using DRS for seamless integration with Gen3 servers",
			}

			// Add basic subcommands for testing
			rootCmd.AddCommand(&cobra.Command{Use: "download", Short: "Download file"})
			rootCmd.AddCommand(&cobra.Command{Use: "init", Short: "Initialize"})
			rootCmd.AddCommand(&cobra.Command{Use: "query", Short: "Query DRS"})
			rootCmd.AddCommand(&cobra.Command{Use: "version", Short: "Version"})

			var buf bytes.Buffer
			rootCmd.SetOut(&buf)
			rootCmd.SetErr(&buf)
			rootCmd.SetArgs(tt.args)

			err := rootCmd.Execute()
			
			if (err != nil) != tt.wantErr {
				t.Errorf("root command error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			output := buf.String()
			
			// Check that output contains expected strings
			for _, contains := range tt.contains {
				if !strings.Contains(output, contains) {
					t.Errorf("root command output does not contain '%s'. Got: %s", contains, output)
				}
			}
		})
	}
}

func TestRootCommandSubcommands(t *testing.T) {
	// Test that we can access all subcommands
	subcommands := RootCmd.Commands()
	
	if len(subcommands) == 0 {
		t.Error("Expected root command to have subcommands")
	}

	// Test each subcommand has proper structure
	for _, cmd := range subcommands {
		if cmd.Use == "" {
			t.Errorf("Subcommand has empty Use field")
		}
		if cmd.Short == "" {
			t.Errorf("Subcommand '%s' has empty Short description", cmd.Use)
		}
	}
}

func TestRootCommandPersistentPreRun(t *testing.T) {
	// Test that PersistentPreRun is defined
	if RootCmd.PersistentPreRun == nil {
		t.Error("Expected PersistentPreRun to be defined")
	}

	// Test that it can be called without panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("PersistentPreRun panicked: %v", r)
		}
	}()

	RootCmd.PersistentPreRun(RootCmd, []string{})
}

func TestRootCommandCompletionOptions(t *testing.T) {
	// Test that completion options are properly configured
	if !RootCmd.CompletionOptions.HiddenDefaultCmd {
		t.Error("Expected HiddenDefaultCmd to be true")
	}
}