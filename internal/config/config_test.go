package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigIgnoresFormerRepositoryYAMLPath(t *testing.T) {
	dir := setupTestRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, ".git-drs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git-drs", "config.yaml"), []byte("not: [valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("former repository YAML path must not be loaded: %v", err)
	}
	if len(cfg.Remotes) != 0 {
		t.Fatalf("unexpected remotes loaded from former YAML path: %+v", cfg.Remotes)
	}
}

func TestLoadConfigMergesSharedPolicyWithLocalOverrides(t *testing.T) {
	dir := setupTestRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, ".git-drs"), 0o755); err != nil {
		t.Fatal(err)
	}
	policy := `version: 1
remotes:
  research:
    endpoint: https://drs.example.org
    provider: gen3
    auth: bearer
    scope: example/tutorial
    selection:
      access_method: prefer:globus
    transfer:
      globus:
        allowed_source_collections: [SOURCE-A]
`
	if err := os.WriteFile(filepath.Join(dir, sharedPolicyPath), []byte(policy), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"config", "drs.remote.research.endpoint", "https://internal.example.org"},
		{"config", "drs.remote.research.access-method", "require:https"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	remote := cfg.Remotes[Remote("research")]
	if cfg.DefaultRemote != "research" {
		t.Fatalf("default remote = %q", cfg.DefaultRemote)
	}
	if remote.Generic == nil || remote.Generic.Endpoint != "https://internal.example.org" || remote.Generic.Provider != "gen3" || remote.Generic.Auth != "bearer" || remote.Generic.Scope != "example/tutorial" || remote.AccessMethod != "require:https" {
		t.Fatalf("effective remote = %+v", remote)
	}
	if len(remote.AllowedGlobusSources) != 1 || remote.AllowedGlobusSources[0] != "source-a" {
		t.Fatalf("allowed sources = %v", remote.AllowedGlobusSources)
	}
}

func TestLoadConfigSharedPolicyValidation(t *testing.T) {
	for name, policy := range map[string]string{
		"unsupported version": "version: 2\nremotes: {}\n",
		"unknown field":       "version: 1\nunknown: true\nremotes: {}\n",
		"invalid mode":        "version: 1\nremotes:\n  r:\n    selection:\n      access_method: sometimes\n",
		"local routing":       "version: 1\nremotes:\n  r:\n    globus:\n      default_destination: x\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := setupTestRepo(t)
			if err := os.MkdirAll(filepath.Join(dir, ".git-drs"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, sharedPolicyPath), []byte(policy), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(); err == nil {
				t.Fatal("expected invalid shared policy to fail")
			}
		})
	}
}

func TestLoadConfigDistinguishesMissingAndEmptySourceConstraint(t *testing.T) {
	for name, test := range map[string]struct {
		globus  string
		wantNil bool
	}{
		"missing": {globus: "", wantNil: true},
		"empty":   {globus: "    transfer:\n      globus:\n        allowed_source_collections: []\n", wantNil: false},
	} {
		t.Run(name, func(t *testing.T) {
			dir := setupTestRepo(t)
			if err := os.MkdirAll(filepath.Join(dir, ".git-drs"), 0o755); err != nil {
				t.Fatal(err)
			}
			policy := "version: 1\nremotes:\n  research:\n" + test.globus
			if err := os.WriteFile(filepath.Join(dir, sharedPolicyPath), []byte(policy), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if gotNil := cfg.Remotes[Remote("research")].AllowedGlobusSources == nil; gotNil != test.wantNil {
				t.Fatalf("nil = %v, want %v", gotNil, test.wantNil)
			}
		})
	}
}

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
		Gen3: &Gen3Remote{Endpoint: "https://gen3.example", ProjectID: "proj", Bucket: "buck"}, AccessMethod: "prefer:globus",
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
	if got := loaded.Remotes[Remote("origin")].AccessMethod; got != "prefer:globus" {
		t.Fatalf("access method policy = %q", got)
	}
}

func TestLoadConfigPreservesGlobusRouteMaps(t *testing.T) {
	dir := setupTestRepo(t)
	commands := [][]string{
		{"config", "drs.remote.research.type", "local"},
		{"config", "drs.remote.research.endpoint", "http://localhost:8080"},
		{"config", "drs.remote.research.globus-default-destination", "destination-default"},
		{"config", "--add", "drs.remote.research.globus-collection", "SOURCE-A=destination-west"},
		{"config", "--add", "drs.remote.research.globus-collection", "source-b=destination-east"},
		{"config", "--add", "drs.remote.research.globus-destination-path", "destination-west=/projects/research"},
	}
	for _, args := range commands {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	remote := cfg.Remotes[Remote("research")]
	if remote.GlobusDefaultDestination != "destination-default" || remote.GlobusCollections["source-a"] != "destination-west" || remote.GlobusCollections["source-b"] != "destination-east" {
		t.Fatalf("Globus routing = %+v", remote)
	}
	if remote.GlobusDestinationPaths["destination-west"] != "/projects/research" {
		t.Fatalf("Globus destination paths = %+v", remote.GlobusDestinationPaths)
	}
}

func TestLoadConfigRejectsConflictingGlobusRoutes(t *testing.T) {
	dir := setupTestRepo(t)
	for _, value := range []string{"source-a=destination-one", "source-a=destination-two"} {
		cmd := exec.Command("git", "config", "--add", "drs.remote.research.globus-collection", value)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git config: %v: %s", err, out)
		}
	}
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "conflicting globus-collection") {
		t.Fatalf("LoadConfig error = %v", err)
	}
}

func TestParseConfigMapRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{"missing-separator", "source=", "=destination", "source=destination=extra"} {
		if _, err := parseConfigMap("globus-collection", []string{value}); err == nil {
			t.Fatalf("expected %q to fail", value)
		}
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

func TestRemoveRemote_TerraCleansAuthAndMode(t *testing.T) {
	tmpDir := setupTestRepo(t)
	remoteName := Remote("anvil")

	_, err := UpdateRemote(remoteName, RemoteSelect{
		Terra: &TerraRemote{
			Endpoint: "https://data.terra.bio",
			Auth:     "google-adc",
			Mode:     "read-only",
		},
	})
	if err != nil {
		t.Fatalf("UpdateRemote failed: %v", err)
	}

	cfg, err := RemoveRemote(remoteName)
	if err != nil {
		t.Fatalf("RemoveRemote failed: %v", err)
	}
	if _, ok := cfg.Remotes[remoteName]; ok {
		t.Fatalf("removed Terra remote %q was recreated", remoteName)
	}

	for _, key := range []string{
		"drs.remote.anvil.auth",
		"drs.remote.anvil.mode",
	} {
		cmd := exec.Command("git", "config", "--local", "--get", key)
		cmd.Dir = tmpDir
		if output, err := cmd.CombinedOutput(); err == nil {
			t.Errorf("expected %s to be unset, got %q", key, string(output))
		}
	}
}
