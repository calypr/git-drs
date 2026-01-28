package transferref_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/cmd/transferref"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/projectdir"
)

func TestTransferRefCmd_Init(t *testing.T) {
	// Setup config for test
	tmp := t.TempDir()
	exec.Command("git", "init", tmp).Run()
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	// Mock config
	configDir := filepath.Join(tmp, projectdir.DRS_DIR)
	os.MkdirAll(configDir, 0755)

	// Create dummy config
	_ = config.Config{
		DefaultRemote: "origin",
		Remotes: map[config.Remote]config.RemoteSelect{
			"origin": {},
		},
	}
	f, _ := os.Create(filepath.Join(configDir, "config.yaml"))
	// We can't easily marshal dependencies that are skipped in test but basic config is fine
	// actually we just need a file to exist and be loadable
	f.WriteString("default_remote: origin\nremotes:\n  origin:\n    gen3:\n      api_key: test\n      project_id: prog-proj\n")
	f.Close()

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
	configDir := filepath.Join(tmp, projectdir.DRS_DIR)
	os.MkdirAll(configDir, 0755)
	f, _ := os.Create(filepath.Join(configDir, "config.yaml"))
	f.WriteString("default_remote: origin\nremotes:\n  origin:\n    gen3:\n      api_key: test\n      project_id: prog-proj\n")
	f.Close()

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
