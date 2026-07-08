package config

import (
	"os/exec"
	"testing"
)

func TestAcceptanceLoadTerraRemoteConfig(t *testing.T) {
	repo := setupTestRepo(t)
	runGitConfig := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"config"}, args...)...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git config %v failed: %v: %s", args, err, string(out))
		}
	}

	runGitConfig("drs.default-remote", "anvil")
	runGitConfig("drs.remote.anvil.type", "terra")
	runGitConfig("drs.remote.anvil.endpoint", "https://drs.anvilproject.org")
	runGitConfig("drs.remote.anvil.auth", "google-adc")
	runGitConfig("drs.remote.anvil.mode", "read-only")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	remote := cfg.GetRemote(Remote("anvil"))
	if remote == nil {
		t.Fatalf("expected terra remote to load as a DRS remote")
	}
	if got := remote.GetEndpoint(); got != "https://drs.anvilproject.org" {
		t.Fatalf("unexpected terra endpoint: %q", got)
	}
}
