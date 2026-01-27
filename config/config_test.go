package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	anvil_client "github.com/calypr/git-drs/client/anvil"
	indexd_client "github.com/calypr/git-drs/client/indexd"
	"gopkg.in/yaml.v3"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	cmd := exec.Command("git", "init", tmpDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, string(out))
	}

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
		Anvil: &anvil_client.AnvilRemote{Endpoint: "https://anvil.example", Auth: anvil_client.AnvilAuth{TerraProject: "terra"}},
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
	if _, err := LoadConfig(); err == nil {
		t.Fatalf("expected error when config missing")
	}
}

func TestLoadConfigRequiresDefaultRemote(t *testing.T) {
	repo := setupTestRepo(t)
	configDir := filepath.Join(repo, ".git", "drs")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.yaml")
	file, err := os.Create(configPath)
	if err != nil {
		t.Fatalf("create config: %v", err)
	}
	defer file.Close()

	cfg := Config{Remotes: map[Remote]RemoteSelect{
		Remote("origin"): {Anvil: &anvil_client.AnvilRemote{Endpoint: "https://anvil.example", Auth: anvil_client.AnvilAuth{TerraProject: "terra"}}},
	}}
	if err := yaml.NewEncoder(file).Encode(cfg); err != nil {
		t.Fatalf("encode config: %v", err)
	}

	if _, err := LoadConfig(); err == nil {
		t.Fatalf("expected error for missing default_remote")
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
		t.Fatalf("expected provided remote, got %s (%v)", remote, err)
	}
}

func TestConfig_AddRemote(t *testing.T) {
	cfg := &Config{
		Remotes: make(map[Remote]RemoteSelect),
	}

	remoteName := Remote("test-remote")
	// Using Gen3 as example
	cfg.Remotes[remoteName] = RemoteSelect{
		Gen3: &indexd_client.Gen3Remote{},
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
			remote1: {Gen3: &indexd_client.Gen3Remote{}},
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
		cfg.Remotes[r] = RemoteSelect{Gen3: &indexd_client.Gen3Remote{}}
	}

	if len(cfg.Remotes) != 3 {
		t.Errorf("Expected 3 remotes, got %d", len(cfg.Remotes))
	}
}
