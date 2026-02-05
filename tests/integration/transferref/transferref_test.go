package transferref_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/calypr/git-drs/cmd/transferref"
	"github.com/calypr/git-drs/config"
)

func TestTransferRefCmd_Init(t *testing.T) {
	// Setup config for test
	tmp := t.TempDir()
	exec.Command("git", "init", tmp).Run()
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	// Mock config
	// Create dummy config using git config
	cmds := [][]string{
		{"config", "lfs.customtransfer.drs.default-remote", "origin"},
		{"config", "lfs.customtransfer.drs.remote.origin.type", "gen3"},
		{"config", "lfs.customtransfer.drs.remote.origin.project", "prog-proj"},
		// api_key is not used in LoadConfig structs currently for gen3 remote, but we set what we can
	}
	for _, args := range cmds {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}

	// Check that loading this config works (implicit validation of setup)
	if _, err := config.LoadConfig(); err != nil {
		t.Fatalf("Failed to load setup config: %v", err)
	}

	// Capture stdout
	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	// Simulate Stdin with "init" message
	input := `{"event":"init","operation":"download","remote":"origin","concurrent":true,"concurrenttransfers":3}`

	// We need to pipe this input to stdin
	rIn, wIn, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = rIn
	defer func() { os.Stdin = oldStdin }()

	go func() {
		wIn.Write([]byte(input + "\n"))
		wIn.Close()
	}()

	// Execute RunE via Cmd
	err := transferref.Cmd.RunE(transferref.Cmd, []string{})
	w.Close()

	if err != nil {
		t.Fatalf("Cmd.RunE failed: %v", err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Check for empty JSON object response to init {}
	if !strings.Contains(output, "{}") {
		t.Errorf("Expected '{}' in output, got: %s", output)
	}
}

func TestTransferRefCmd_Download(t *testing.T) {
	// Setup config for test
	tmp := t.TempDir()
	exec.Command("git", "init", tmp).Run()
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	// Mock config
	cmds := [][]string{
		{"config", "lfs.customtransfer.drs.default-remote", "origin"},
		{"config", "lfs.customtransfer.drs.remote.origin.type", "gen3"},
		{"config", "lfs.customtransfer.drs.remote.origin.project", "prog-proj"},
	}
	for _, args := range cmds {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}

	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	// Simulate init then terminate to cover loop
	input := `{"event":"init"}` + "\n" + `{"event":"terminate"}`

	rIn, wIn, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = rIn
	defer func() { os.Stdin = oldStdin }()

	go func() {
		wIn.Write([]byte(input + "\n"))
		wIn.Close()
	}()

	err := transferref.Cmd.RunE(transferref.Cmd, []string{})
	w.Close()

	if err != nil {
		t.Fatalf("Cmd.RunE failed: %v", err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "{}") {
		t.Errorf("Expected '{}' in output for init, got: %s", output)
	}
}
