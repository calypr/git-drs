package version

import (
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionCommand(t *testing.T) {
	t.Run("prints version with git-drs prefix", func(t *testing.T) {
		// Arrange
		cmd := Cmd
		cmd.SetArgs([]string{})

		// Act
		output := testutils.CaptureStdout(t, func() {
			err := cmd.Execute()
			require.NoError(t, err)
		})

		// Assert
		assert.Contains(t, output, "git-drs", "output should contain 'git-drs'")
		assert.NotEmpty(t, output, "output should not be empty")
	})

	t.Run("version string matches expected format", func(t *testing.T) {
		// Arrange
		cmd := Cmd
		cmd.SetArgs([]string{})

		// Act
		output := testutils.CaptureStdout(t, func() {
			err := cmd.Execute()
			require.NoError(t, err)
		})

		// Assert
		output = strings.TrimSpace(output)

		// Should contain "git-drs" followed by version number
		parts := strings.Fields(output)
		assert.GreaterOrEqual(t, len(parts), 2, "output should have at least 2 parts")
		if len(parts) >= 2 {
			assert.Equal(t, "git-drs", parts[0], "first part should be 'git-drs'")

			// Version should contain numbers and dots (e.g., "0.4.0-rc5")
			version := parts[1]
			assert.NotEmpty(t, version, "version should not be empty")
			assert.True(t,
				strings.Contains(version, ".") || strings.Contains(version, "-"),
				"version should contain dot or dash separator",
			)
		}
	})

	t.Run("accepts no arguments", func(t *testing.T) {
		// Arrange
		cmd := Cmd
		cmd.SetArgs([]string{})

		// Act
		output := testutils.CaptureStdout(t, func() {
			err := cmd.Execute()
			require.NoError(t, err)
		})

		// Assert
		assert.Contains(t, output, "git-drs")
	})

	t.Run("ignores extra arguments gracefully", func(t *testing.T) {
		// Arrange
		cmd := Cmd
		cmd.SetArgs([]string{"extra", "args"})

		// Act
		output := testutils.CaptureStdout(t, func() {
			err := cmd.Execute()
			require.NoError(t, err)
		})

		// Assert - Cobra doesn't enforce arg validation for Run (vs RunE)
		// So this should still work and print version
		assert.Contains(t, output, "git-drs")
	})
}

func TestVersionCommand_Output(t *testing.T) {
	tests := []struct {
		name           string
		expectedPrefix string
	}{
		{
			name:           "contains git-drs prefix",
			expectedPrefix: "git-drs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			cmd := Cmd
			cmd.SetArgs([]string{})

			// Act
			output := testutils.CaptureStdout(t, func() {
				err := cmd.Execute()
				require.NoError(t, err)
			})

			// Assert
			assert.True(t,
				strings.HasPrefix(output, tt.expectedPrefix),
				"output should start with '%s', got: %s", tt.expectedPrefix, output,
			)
		})
	}
}
