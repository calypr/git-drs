package ping

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/testutils"
)

func TestPingCmdArgs(t *testing.T) {
	if err := Cmd.Args(Cmd, nil); err != nil {
		t.Fatalf("unexpected error with no args: %v", err)
	}
	if err := Cmd.Args(Cmd, []string{"origin"}); err != nil {
		t.Fatalf("unexpected error with one arg: %v", err)
	}
	if err := Cmd.Args(Cmd, []string{"origin", "extra"}); err == nil {
		t.Fatal("expected error for extra args")
	}
}

func TestResolveStatusLocalRemote(t *testing.T) {
	tmpDir := testutils.SetupTestGitRepo(t)
	testutils.CreateTestConfig(t, tmpDir, &config.Config{
		DefaultRemote: config.Remote(config.ORIGIN),
		Remotes: map[config.Remote]config.RemoteSelect{
			config.Remote(config.ORIGIN): {
				Local: &config.LocalRemote{
					BaseURL:       "http://127.0.0.1:8080",
					ProjectID:     "end_to_end_test",
					Bucket:        "cbds",
					Organization:  "calypr",
					BasicUsername: "drs-user",
					BasicPassword: "drs-pass",
				},
			},
		},
	})
	if err := gitrepo.SetBucketMapping("calypr", "end_to_end_test", "cbds", "prefix"); err != nil {
		t.Fatalf("SetBucketMapping failed: %v", err)
	}

	status, _, err := resolveStatus(nil, drslog.NewNoOpLogger())
	if err != nil {
		t.Fatalf("resolveStatus returned error: %v", err)
	}
	if status.Remote != "origin" || !status.IsDefault {
		t.Fatalf("unexpected remote selection: %+v", status)
	}
	if status.RemoteType != "local" || status.Endpoint != "http://127.0.0.1:8080" {
		t.Fatalf("unexpected remote type/endpoint: %+v", status)
	}
	if status.Organization != "calypr" || status.Project != "end_to_end_test" {
		t.Fatalf("unexpected scope: %+v", status)
	}
	if status.Bucket != "cbds" || status.StoragePrefix != "prefix" {
		t.Fatalf("unexpected bucket scope: %+v", status)
	}
	if status.AuthMode != "none" {
		t.Fatalf("expected auth mode none from client credential shape, got %+v", status)
	}
}

func TestPingRunEPrintsStatusAndHealth(t *testing.T) {
	tmpDir := testutils.SetupTestGitRepo(t)
	testutils.CreateTestConfig(t, tmpDir, &config.Config{
		DefaultRemote: config.Remote(config.ORIGIN),
		Remotes: map[config.Remote]config.RemoteSelect{
			config.Remote(config.ORIGIN): {
				Local: &config.LocalRemote{
					BaseURL:      "http://127.0.0.1:8080",
					ProjectID:    "end_to_end_test",
					Bucket:       "cbds",
					Organization: "calypr",
				},
			},
		},
	})
	if err := gitrepo.SetBucketMapping("calypr", "end_to_end_test", "cbds", "prefix"); err != nil {
		t.Fatalf("SetBucketMapping failed: %v", err)
	}

	oldHealth := pingHealth
	pingHealth = func(ctx context.Context, gc *config.GitContext) error {
		if gc == nil || gc.ProjectId != "end_to_end_test" {
			t.Fatalf("unexpected git context: %+v", gc)
		}
		return nil
	}
	t.Cleanup(func() { pingHealth = oldHealth })

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = oldStdout })

	runErr := Cmd.RunE(Cmd, nil)
	_ = w.Close()
	if runErr != nil {
		t.Fatalf("Cmd.RunE returned error: %v", runErr)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	got := buf.String()
	for _, want := range []string{
		"remote: origin (default)",
		"type: local",
		"endpoint: http://127.0.0.1:8080",
		"organization: calypr",
		"project: end_to_end_test",
		"bucket: cbds",
		"storage_prefix: prefix",
		"health: ok",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected output to contain %q, got %q", want, got)
		}
	}
}
