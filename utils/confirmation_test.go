package utils

import (
	"bytes"
	"os"
	"testing"
)

func TestPromptForConfirmation(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	_, _ = writer.WriteString("YES\n")
	_ = writer.Close()

	oldStdin := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = reader.Close()
	})

	buf := &bytes.Buffer{}
	if err := PromptForConfirmation(buf, "Confirm", "yes", false); err != nil {
		t.Fatalf("PromptForConfirmation error: %v", err)
	}
}

func TestPromptForConfirmation_Mismatch(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	_, _ = writer.WriteString("no\n")
	_ = writer.Close()

	oldStdin := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = reader.Close()
	})

	buf := &bytes.Buffer{}
	if err := PromptForConfirmation(buf, "Confirm", "yes", true); err == nil {
		t.Fatalf("expected mismatch error")
	}
}

func TestDisplayHelpers(t *testing.T) {
	buf := &bytes.Buffer{}
	DisplayWarningHeader(buf, "delete files")
	DisplayField(buf, "Key", "Value")
	DisplayFooter(buf)

	out := buf.String()
	if out == "" || !bytes.Contains([]byte(out), []byte("WARNING")) {
		t.Fatalf("unexpected output: %s", out)
	}
}
