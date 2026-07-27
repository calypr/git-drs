package remoteruntime

import (
	"os"
	"os/exec"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/gitrepo"
	syconf "github.com/calypr/syfon/client/config"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	cmd := exec.Command("git", "init", tmpDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, string(out))
	}
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
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	return tmpDir
}

func TestNewLocalIncludesRepoBasicAuth(t *testing.T) {
	setupTestRepo(t)

	cfg, err := config.UpdateRemote(config.Remote("origin"), config.RemoteSelect{
		Local: &config.LocalRemote{BaseURL: "http://localhost:8080"},
	})
	if err != nil {
		t.Fatalf("UpdateRemote failed: %v", err)
	}
	if err := gitrepo.SetRemoteBasicAuth("origin", "alice", "secret"); err != nil {
		t.Fatalf("SetRemoteBasicAuth failed: %v", err)
	}
	logger := drslog.GetLogger()
	gitCtx, err := New(cfg, config.Remote("origin"), logger)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if gitCtx == nil || gitCtx.Client == nil {
		t.Fatalf("expected usable GitContext, got %+v", gitCtx)
	}
}

func TestNewTerraRemoteContext(t *testing.T) {
	setupTestRepo(t)

	cfg, err := config.UpdateRemote(config.Remote("anvil"), config.RemoteSelect{
		Terra: &config.TerraRemote{
			Endpoint: "https://data.terra.bio",
			Auth:     "google-adc",
			Mode:     "read-only",
		},
	})
	if err != nil {
		t.Fatalf("UpdateRemote failed: %v", err)
	}

	gitCtx, err := New(cfg, config.Remote("anvil"), drslog.GetLogger())
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if gitCtx.RemoteType != config.TerraServerType {
		t.Fatalf("RemoteType = %q, want %q", gitCtx.RemoteType, config.TerraServerType)
	}
	if gitCtx.Endpoint != "https://data.terra.bio" {
		t.Fatalf("Endpoint = %q, want Terra endpoint", gitCtx.Endpoint)
	}
	if gitCtx.Client != nil {
		t.Fatalf("Terra runtime should not create a Syfon client, got %+v", gitCtx.Client)
	}
	if !gitCtx.CanResolve() || !gitCtx.CanDownload() || !gitCtx.IsReadOnly() || gitCtx.CanUpload() || gitCtx.CanRegister() {
		t.Fatalf("unexpected Terra capabilities: %+v", gitCtx.Capabilities)
	}
}

func TestNewGenericTerraUsesProviderAdapter(t *testing.T) {
	setupTestRepo(t)
	cfg := &config.Config{Remotes: map[config.Remote]config.RemoteSelect{
		"anvil": {Generic: &config.GenericRemote{Endpoint: "https://data.terra.bio", Provider: "terra", Auth: "google-adc"}},
	}}
	gitCtx, err := New(cfg, "anvil", drslog.GetLogger())
	if err != nil {
		t.Fatal(err)
	}
	if gitCtx.RemoteType != config.TerraServerType || !gitCtx.IsReadOnly() || !gitCtx.CanDownload() {
		t.Fatalf("generic Terra did not resolve through Terra adapter: %+v", gitCtx)
	}
}

func TestNewRejectsProviderWithoutOperationalAdapter(t *testing.T) {
	cfg := &config.Config{Remotes: map[config.Remote]config.RemoteSelect{
		"cgc": {Generic: &config.GenericRemote{Endpoint: "https://example.test", Provider: "cgc", Auth: "bearer"}},
	}}
	if _, err := New(cfg, "cgc", drslog.GetLogger()); err == nil {
		t.Fatal("expected unimplemented provider adapter to fail explicitly")
	}
}

func TestLocalClientResolvesBucketScopeMappings(t *testing.T) {
	setupTestRepo(t)

	if err := gitrepo.SetBucketMapping("org-a", "", "mapped-bucket", "program-root"); err != nil {
		t.Fatalf("SetBucketMapping org: %v", err)
	}
	if err := gitrepo.SetBucketMapping("org-a", "proj-1", "mapped-bucket", "project-subpath"); err != nil {
		t.Fatalf("SetBucketMapping project: %v", err)
	}

	gitCtx, err := localClient("origin", config.LocalRemote{
		BaseURL:      "http://localhost:8080",
		Organization: "org-a",
		ProjectID:    "proj-1",
		Bucket:       "configured-bucket",
	}, drslog.GetLogger())
	if err != nil {
		t.Fatalf("localClient failed: %v", err)
	}
	if gitCtx.BucketName != "mapped-bucket" {
		t.Fatalf("BucketName = %q, want mapped-bucket", gitCtx.BucketName)
	}
	if gitCtx.StoragePrefix != "program-root/project-subpath" {
		t.Fatalf("StoragePrefix = %q, want program-root/project-subpath", gitCtx.StoragePrefix)
	}
}

func TestNewGitContextReadsLFSConcurrentTransfers(t *testing.T) {
	setupTestRepo(t)

	if err := gitrepo.SetGitConfigOptions(map[string]string{
		"lfs.concurrenttransfers": "7",
	}); err != nil {
		t.Fatalf("SetGitConfigOptions failed: %v", err)
	}

	cred := syconf.Credential{
		APIEndpoint: "https://example.test",
		AccessToken: "token",
	}
	remote := config.Gen3Remote{
		Endpoint:     "https://example.test",
		Organization: "org1",
		ProjectID:    "proj1",
		Bucket:       "bucket1",
	}

	gitCtx, err := newGitContext(cred, remote, drslog.GetLogger())
	if err != nil {
		t.Fatalf("newGitContext failed: %v", err)
	}
	if gitCtx.UploadConcurrency != 7 {
		t.Fatalf("UploadConcurrency = %d, want 7", gitCtx.UploadConcurrency)
	}
}
