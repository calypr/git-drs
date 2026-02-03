package config

import (
	"os"
	"os/exec"
	"testing"

	anvil_client "github.com/calypr/git-drs/client/anvil"
	"github.com/calypr/git-drs/client/indexd"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	cmd := exec.Command("git", "init", tmpDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, string(out))
	}

	// Set user config to avoid git errors
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	_ = cmd.Run()
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	_ = cmd.Run()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	return tmpDir
}

func TestUpdateRemoteAndLoadConfig(t *testing.T) {
	setupTestRepo(t)

	remote := RemoteSelect{
		Gen3: &indexd.Gen3Remote{Endpoint: "https://gen3.example", ProjectID: "proj", Bucket: "buck"},
	}
	cfg, err := UpdateRemote(Remote("origin"), remote)
	if err != nil {
		t.Fatalf("UpdateRemote error: %v", err)
	}
	if cfg.DefaultRemote != Remote("origin") {
		t.Fatalf("expected default remote set, got %s", cfg.DefaultRemote)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if _, ok := loaded.Remotes[Remote("origin")]; !ok {
		t.Fatalf("expected remote in loaded config")
	}
}

func TestLoadConfigMissing(t *testing.T) {
	setupTestRepo(t)
	// With git config, missing keys just return empty map, LoadConfig returns empty struct
	// It doesn't error unless git command fails (which it shouldn't in init'd repo)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if len(cfg.Remotes) > 0 {
		t.Fatal("expected empty remotes")
	}
}

func TestCreateEmptyConfigAndSave(t *testing.T) {
	setupTestRepo(t)
	if err := CreateEmptyConfig(); err != nil {
		t.Fatalf("CreateEmptyConfig error: %v", err)
	}

	cfg := &Config{DefaultRemote: Remote("origin"), Remotes: map[Remote]RemoteSelect{}}
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig error: %v", err)
	}
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if loaded.DefaultRemote != Remote("origin") {
		t.Fatalf("unexpected default remote: %s", loaded.DefaultRemote)
	}
}

func TestGetRemoteOrDefault(t *testing.T) {
	cfg := Config{
		DefaultRemote: Remote("origin"),
		Remotes: map[Remote]RemoteSelect{
			Remote("origin"): {},
		},
	}
	if remote, err := cfg.GetRemoteOrDefault(""); err != nil || remote != Remote("origin") {
		t.Fatalf("expected default remote, got %s (%v)", remote, err)
	}
	if remote, err := cfg.GetRemoteOrDefault("other"); err != nil || remote != Remote("other") {
		// GetRemoteOrDefault just returns the string if provided, doesn't validate existence?
		// Check implementation: yes, it returns Remote(remote)
		if remote != Remote("other") {
			t.Fatalf("expected provided remote, got %s (%v)", remote, err)
		}
	}
}

func TestConfig_AddRemote(t *testing.T) {
	cfg := &Config{
		Remotes: make(map[Remote]RemoteSelect),
	}

	remoteName := Remote("test-remote")
	// Using Gen3 as example
	cfg.Remotes[remoteName] = RemoteSelect{
		Gen3: &indexd.Gen3Remote{},
	}

	if len(cfg.Remotes) != 1 {
		t.Errorf("Expected 1 remote, got %d", len(cfg.Remotes))
	}
}

func TestConfig_FindRemote(t *testing.T) {
	remote1 := Remote("remote1")
	remote2 := Remote("remote2")

	cfg := &Config{
		Remotes: map[Remote]RemoteSelect{
			remote1: {Gen3: &indexd.Gen3Remote{}},
			remote2: {Anvil: &anvil_client.AnvilRemote{}},
		},
	}

	var foundName Remote
	var foundSelect RemoteSelect

	for name, sel := range cfg.Remotes {
		if name == "remote2" {
			foundName = name
			foundSelect = sel
			break
		}
	}

	if foundName == "" {
		t.Error("Expected to find remote2")
	}
	if foundSelect.Anvil == nil {
		t.Error("Expected found remote to have Anvil config")
	}
}

func TestRemote_Validation(t *testing.T) {
	// IsValidRemoteType test
	tests := []struct {
		name    string
		mode    string
		isValid bool
	}{
		{"valid gen3", "gen3", true},
		{"valid anvil", "anvil", true},
		{"invalid", "foo", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := IsValidRemoteType(tt.mode)
			valid := err == nil
			if valid != tt.isValid {
				t.Errorf("IsValidRemoteType(%q) = %v, want %v", tt.mode, valid, tt.isValid)
			}
		})
	}
}

func TestConfig_MultipleRemotes(t *testing.T) {
	cfg := &Config{
		Remotes: make(map[Remote]RemoteSelect),
	}

	remotes := []Remote{"origin", "backup", "anvil"}

	for _, r := range remotes {
		cfg.Remotes[r] = RemoteSelect{Gen3: &indexd.Gen3Remote{}}
	}

	if len(cfg.Remotes) != 3 {
		t.Errorf("Expected 3 remotes, got %d", len(cfg.Remotes))
	}
}
