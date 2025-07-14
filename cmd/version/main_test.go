package version

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bmeg/git-drs/version"
	"github.com/spf13/cobra"
)

func TestVersionCommand(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantErr  bool
		contains []string
	}{
		{
			name:     "version command with no args",
			args:     []string{},
			wantErr:  false,
			contains: []string{"git commit:", "git branch:", "git upstream:", "build date:", "version:"},
		},
		{
			name:    "version command with extra args",
			args:    []string{"extra", "args"},
			wantErr: false, // Version command doesn't validate args
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a buffer to capture output
			var buf bytes.Buffer
			
			// Create a copy of the command
			cmd := &cobra.Command{
				Use:   "version",
				Short: "Get version",
				Long:  ``,
				Run: func(cmd *cobra.Command, args []string) {
					buf.WriteString(version.String())
				},
			}

			// Set args and execute
			cmd.SetArgs(tt.args)
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)

			err := cmd.Execute()
			
			if (err != nil) != tt.wantErr {
				t.Errorf("version command error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			output := buf.String()
			
			// Check that output contains expected strings
			for _, contains := range tt.contains {
				if !strings.Contains(output, contains) {
					t.Errorf("version command output does not contain '%s'. Got: %s", contains, output)
				}
			}
		})
	}
}

func TestVersionCommandStructure(t *testing.T) {
	// Test that the command is properly structured
	if Cmd.Use != "version" {
		t.Errorf("Expected Use to be 'version', got '%s'", Cmd.Use)
	}

	if Cmd.Short == "" {
		t.Error("Expected Short description to be non-empty")
	}

	if Cmd.Run == nil {
		t.Error("Expected Run function to be defined")
	}
}

func TestVersionOutput(t *testing.T) {
	// Test version output format
	output := version.String()
	
	expectedLines := []string{
		"git commit:",
		"git branch:",
		"git upstream:",
		"build date:",
		"version:",
	}
	
	for _, line := range expectedLines {
		if !strings.Contains(output, line) {
			t.Errorf("Version output missing expected line '%s'. Got: %s", line, output)
		}
	}
}

func TestVersionLogFields(t *testing.T) {
	// Test LogFields function
	fields := version.LogFields()
	
	if len(fields) != 10 { // 5 fields * 2 (key-value pairs)
		t.Errorf("Expected 10 log fields, got %d", len(fields))
	}
	
	expectedKeys := []string{
		"GitCommit", "GitBranch", "GitUpstream", "BuildDate", "Version",
	}
	
	for i, key := range expectedKeys {
		if fields[i*2] != key {
			t.Errorf("Expected log field key '%s', got '%v'", key, fields[i*2])
		}
	}
}