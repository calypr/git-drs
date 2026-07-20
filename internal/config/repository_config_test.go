package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryConfigLoadsAndLocalGitOverrides(t *testing.T) {
	repo := setupTestRepo(t)
	dir := filepath.Join(repo, ".git-drs")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := "version: 1\ndefault_remote: anvil\nremotes:\n  anvil:\n    type: terra\n    endpoint: https://data.terra.bio\n    auth: google-adc\n    mode: read-only\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git/config"), append(mustRead(t, filepath.Join(repo, ".git/config")), []byte("\n[drs]\n\tdefault-remote = local-choice\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GetRemote("anvil") == nil || cfg.DefaultRemote != "local-choice" {
		t.Fatalf("repository remote or local override missing: %+v", cfg)
	}
}

func TestRepositoryConfigRejectsSecretFields(t *testing.T) {
	repo := setupTestRepo(t)
	dir := filepath.Join(repo, ".git-drs")
	_ = os.Mkdir(dir, 0o755)
	data := "version: 1\nremotes:\n  anvil:\n    type: terra\n    endpoint: https://data.terra.bio\n    auth: google-adc\n    mode: read-only\n    token: do-not-load\n"
	_ = os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(data), 0o644)
	_, err := LoadConfig()
	if err == nil || !strings.Contains(err.Error(), "field token not found") {
		t.Fatalf("expected fail-closed secret rejection, got %v", err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
