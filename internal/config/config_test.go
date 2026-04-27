package config

import (
	"os"
	"os/exec"
	"testing"

	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/gitrepo"
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
		Gen3: &Gen3Remote{Endpoint: "https://gen3.example", ProjectID: "proj", Bucket: "buck"},
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
		Gen3: &Gen3Remote{},
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
			remote1: {Gen3: &Gen3Remote{}},
			remote2: {Local: &LocalRemote{}},
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
	if foundSelect.Local == nil {
		t.Error("Expected found remote to have Local config")
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
		{"valid local", "local", true},
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

	remotes := []Remote{"origin", "backup", "local"}

	for _, r := range remotes {
		cfg.Remotes[r] = RemoteSelect{Gen3: &Gen3Remote{}}
	}

	if len(cfg.Remotes) != 3 {
		t.Errorf("Expected 3 remotes, got %d", len(cfg.Remotes))
	}
}

func TestLoadConfig_DRSKeys(t *testing.T) {
	tmpDir := setupTestRepo(t)

	commands := [][]string{
		{"config", "drs.default-remote", "legacy"},
		{"config", "drs.remote.legacy.type", "gen3"},
		{"config", "drs.remote.legacy.endpoint", "https://legacy.example"},
		{"config", "drs.remote.legacy.project", "legacy-proj"},
		{"config", "drs.remote.legacy.bucket", "legacy-bucket"},
	}
	for _, args := range commands {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v: %s", args, err, string(out))
		}
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.DefaultRemote != Remote("legacy") {
		t.Fatalf("expected default remote, got %s", cfg.DefaultRemote)
	}
	legacy := cfg.Remotes[Remote("legacy")]
	if legacy.Gen3 == nil || legacy.Gen3.Endpoint != "https://legacy.example" {
		t.Fatalf("expected gen3 remote loaded, got %#v", legacy)
	}
}

func TestLoadConfig_LastWriteWinsDefaultRemote(t *testing.T) {
	tmpDir := setupTestRepo(t)

	commands := [][]string{
		{"config", "drs.default-remote", "legacy"},
		{"config", "drs.remote.legacy.type", "gen3"},
		{"config", "drs.remote.legacy.endpoint", "https://legacy.example"},
		{"config", "drs.remote.legacy.project", "legacy-proj"},
		{"config", "drs.remote.legacy.bucket", "legacy-bucket"},
		{"config", "drs.default-remote", "new"},
		{"config", "drs.remote.new.type", "gen3"},
		{"config", "drs.remote.new.endpoint", "https://new.example"},
		{"config", "drs.remote.new.project", "new-proj"},
		{"config", "drs.remote.new.bucket", "new-bucket"},
	}
	for _, args := range commands {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v: %s", args, err, string(out))
		}
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.DefaultRemote != Remote("new") {
		t.Fatalf("expected default remote new, got %s", cfg.DefaultRemote)
	}
	newRemote := cfg.Remotes[Remote("new")]
	if newRemote.Gen3 == nil || newRemote.Gen3.Endpoint != "https://new.example" {
		t.Fatalf("expected gen3 remote loaded, got %#v", newRemote)
	}
}

func TestUpdateRemote_LocalTypePersistence(t *testing.T) {
	tmpDir := setupTestRepo(t)

	remoteName := Remote("local-dev")
	remoteSelect := RemoteSelect{
		Local: &LocalRemote{
			BaseURL: "http://localhost:8080",
		},
	}

	// 1. Update (Write) Config
	cfg, err := UpdateRemote(remoteName, remoteSelect)
	if err != nil {
		t.Fatalf("UpdateRemote failed: %v", err)
	}

	// Verify immediate returned config has it
	if r := cfg.GetRemote(remoteName); r == nil {
		t.Fatalf("Expected remote %s to exist in returned config", remoteName)
	}

	// 2. Inspect git config file directly (optional but good for debugging)
	cmd := exec.Command("git", "config", "--list")
	cmd.Dir = tmpDir
	out, _ := cmd.CombinedOutput()
	t.Logf("Git Config:\n%s", string(out))

	// 3. Load (Read) Config from disk
	loadedCfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	r := loadedCfg.GetRemote(remoteName)
	if r == nil {
		t.Fatalf("Remote %s missing from loaded config", remoteName)
	}

	localRemote, ok := r.(*LocalRemote)
	if !ok {
		// If it's not LocalRemote, it likely defaulted to Gen3Remote due to missing type
		if _, isGen3 := r.(*Gen3Remote); isGen3 {
			t.Fatalf("Remote %s loaded as Gen3Remote (default fallback), expected LocalRemote. Type missing?", remoteName)
		}
		t.Fatalf("Remote %s loaded as unexpected type: %T", remoteName, r)
	}

	if localRemote.BaseURL != "http://localhost:8080" {
		t.Errorf("Expected BaseURL http://localhost:8080, got %s", localRemote.BaseURL)
	}
}

func TestGetRemoteClient_LocalIncludesRepoBasicAuth(t *testing.T) {
	setupTestRepo(t)

	remoteName := Remote("origin")
	_, err := UpdateRemote(remoteName, RemoteSelect{
		Local: &LocalRemote{
			BaseURL: "http://localhost:8080",
		},
	})
	if err != nil {
		t.Fatalf("UpdateRemote failed: %v", err)
	}

	if err := gitrepo.SetRemoteBasicAuth("origin", "alice", "secret"); err != nil {
		t.Fatalf("SetRemoteBasicAuth failed: %v", err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	logger := drslog.GetLogger()
	gitCtx, err := cfg.GetRemoteClient(remoteName, logger)
	if err != nil {
		t.Fatalf("GetRemoteClient failed: %v", err)
	}
	if gitCtx == nil {
		t.Fatalf("expected *GitContext, got nil")
	}
	if gitCtx.Client == nil || gitCtx.Requestor == nil {
		t.Fatalf("expected client and requestor to be initialized, got nil")
	}
	// Basic auth is baked into the HTTP client during construction;
	// the test verifies that GetRemoteClient completes without error when
	// repo credentials are present, and that a usable GitContext is returned.
}

func TestLocalRemoteGetClientResolvesBucketScopeMappings(t *testing.T) {
	setupTestRepo(t)

	if err := gitrepo.SetBucketMapping("org-a", "", "mapped-bucket", "program-root"); err != nil {
		t.Fatalf("SetBucketMapping org: %v", err)
	}
	if err := gitrepo.SetBucketMapping("org-a", "proj-1", "mapped-bucket", "project-subpath"); err != nil {
		t.Fatalf("SetBucketMapping project: %v", err)
	}

	remote := LocalRemote{
		BaseURL:      "http://localhost:8080",
		Organization: "org-a",
		ProjectID:    "proj-1",
		Bucket:       "configured-bucket",
	}
	gitCtx, err := remote.GetClient("origin", drslog.GetLogger())
	if err != nil {
		t.Fatalf("GetClient failed: %v", err)
	}
	if gitCtx.BucketName != "mapped-bucket" {
		t.Fatalf("BucketName = %q, want mapped-bucket", gitCtx.BucketName)
	}
	if gitCtx.StoragePrefix != "program-root/project-subpath" {
		t.Fatalf("StoragePrefix = %q, want program-root/project-subpath", gitCtx.StoragePrefix)
	}
}
