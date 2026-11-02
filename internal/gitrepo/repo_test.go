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

func TestGitConfigReadersPropagateCommandErrors(t *testing.T) {
	tmpDir := t.TempDir()
	originalCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(originalCwd)

	cmd := exec.Command("git", "init", tmpDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, out)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir to temporary repository: %v", err)
	}

	const missingKey = "drs.test-config-reader-missing"
	if value, err := GetGitConfigString(missingKey); err != nil || value != "" {
		t.Fatalf("missing string key = %q, %v; want empty value and nil error", value, err)
	}
	if values, err := GetGitConfigStrings(missingKey); err != nil || len(values) != 0 {
		t.Fatalf("missing string keys = %v, %v; want no values and nil error", values, err)
	}
	if _, err := GetGitConfigString("drs.remote.origin.storage_prefix"); err == nil {
		t.Fatal("GetGitConfigString returned nil error for an invalid config key")
	}
	if _, err := GetGitConfigStrings("drs.remote.origin.storage_prefix"); err == nil {
		t.Fatal("GetGitConfigStrings returned nil error for an invalid config key")
	}

	configPath := filepath.Join(tmpDir, ".git", "config")
	file, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open Git config: %v", err)
	}
	if _, err := file.WriteString("\ninvalid_key = value\n"); err != nil {
		_ = file.Close()
		t.Fatalf("append malformed Git config: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close Git config: %v", err)
	}

	if _, err := GetGitConfigString(missingKey); err == nil {
		t.Fatal("GetGitConfigString returned nil error for malformed Git config")
	}
	if _, err := GetGitConfigStrings(missingKey); err == nil {
		t.Fatal("GetGitConfigStrings returned nil error for malformed Git config")
	}
}
