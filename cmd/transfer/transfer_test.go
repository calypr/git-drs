package transfer

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTransferCmdStructure(t *testing.T) {
	assert.Equal(t, "transfer", Cmd.Use)
	assert.NotEmpty(t, Cmd.Short)
}

func TestTransferRun_EmptyStdin(t *testing.T) {
	// Mock stdin and stdout
	oldStdin := os.Stdin
	oldStdout := os.Stdout
	defer func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	}()

	r, w, _ := os.Pipe()
	os.Stdin = r
	w.Close() // Close writer to trigger EOF immediately

	// Capture stdout
	stdoutR, stdoutW, _ := os.Pipe()
	os.Stdout = stdoutW

	go func() {
		_ = Cmd.RunE(Cmd, []string{})
		stdoutW.Close()
	}()

	var buf bytes.Buffer
	io.Copy(&buf, stdoutR)

	// Should at least fail gracefully or log something if stdin is empty
}
