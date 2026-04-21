package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGetGitHooksDir(t *testing.T) {
	// Create a temp repo
	tmpDir := t.TempDir()
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(originalCwd)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to tmpDir: %v", err)
	}

	cmd := exec.Command("git", "init")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	// Create a subdirectory
	subDir := filepath.Join(tmpDir, "some", "sub", "dir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	// Change to subdirectory
	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("failed to chdir to subdir: %v", err)
	}

	// Get hooks dir
	hooksDir, err := GetGitHooksDir()
	if err != nil {
		t.Fatalf("GetGitHooksDir failed: %v", err)
	}

	expectedHooksDir := filepath.Join(tmpDir, ".git", "hooks")

	// Clean and resolve symlinks for macOS (/var -> /private/var)
	hooksDir, _ = filepath.EvalSymlinks(hooksDir)
	expectedHooksDir, _ = filepath.EvalSymlinks(expectedHooksDir)

	if hooksDir != expectedHooksDir {
		t.Errorf("expected hooks dir %s, got %s", expectedHooksDir, hooksDir)
	}
}

func TestRemoteBasicAuthRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(originalCwd)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to tmpDir: %v", err)
	}

	cmd := exec.Command("git", "init")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	if err := SetRemoteBasicAuth("origin", "alice", "secret"); err != nil {
		t.Fatalf("SetRemoteBasicAuth failed: %v", err)
	}
	user, pass, err := GetRemoteBasicAuth("origin")
	if err != nil {
		t.Fatalf("GetRemoteBasicAuth failed: %v", err)
	}
	if user != "alice" {
		t.Fatalf("expected username alice, got %q", user)
	}
	if pass != "secret" {
		t.Fatalf("expected password secret, got %q", pass)
	}
}
