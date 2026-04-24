package addurl

import (
	"os"
	"os/exec"
	"testing"

	"log/slog"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/gitrepo"
)

type fakeRemote struct {
	project      string
	organization string
	bucket       string
	prefix       string
}

func (f fakeRemote) GetProjectId() string { return f.project }
func (f fakeRemote) GetOrganization() string {
	return f.organization
}
func (f fakeRemote) GetEndpoint() string { return "" }
func (f fakeRemote) GetBucketName() string {
	return f.bucket
}
func (f fakeRemote) GetStoragePrefix() string {
	return f.prefix
}
func (f fakeRemote) GetClient(string, *slog.Logger) (*config.GitContext, error) {
	return nil, nil
}

func TestResolveTargetScope_DefaultFallsBackToRemoteConfig(t *testing.T) {
	remote := fakeRemote{
		project:      "proj-a",
		organization: "org-a",
		bucket:       "remote-bucket",
		prefix:       "remote/prefix",
	}
	org, project, scope, err := resolveTargetScope(remote)
	if err != nil {
		t.Fatalf("resolveTargetScope: %v", err)
	}
	if org != "org-a" || project != "proj-a" {
		t.Fatalf("unexpected scope target: org=%s project=%s", org, project)
	}
	if scope.Bucket != "remote-bucket" || scope.Prefix != "remote/prefix" {
		t.Fatalf("unexpected bucket scope: %+v", scope)
	}
}

func TestResolveTargetScope_UsesBucketMapping(t *testing.T) {
	tmpDir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(orig)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := gitrepo.SetBucketMapping("org-a", "proj-b", "mapped-bucket", "mapped/prefix"); err != nil {
		t.Fatalf("SetBucketMapping: %v", err)
	}

	remote := fakeRemote{
		project:      "proj-b",
		organization: "org-a",
		bucket:       "remote-bucket",
		prefix:       "remote/prefix",
	}
	org, project, scope, err := resolveTargetScope(remote)
	if err != nil {
		t.Fatalf("resolveTargetScope: %v", err)
	}
	if org != "org-a" || project != "proj-b" {
		t.Fatalf("unexpected scope target: org=%s project=%s", org, project)
	}
	if scope.Bucket != "mapped-bucket" || scope.Prefix != "mapped/prefix" {
		t.Fatalf("unexpected bucket scope: %+v", scope)
	}
}
