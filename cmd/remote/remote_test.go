package remote

import (
	"context"
	"log/slog"
	"os/exec"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/testutils"
	syconf "github.com/calypr/syfon/client/config"
	"github.com/stretchr/testify/assert"
)

func TestRemoteListArgs(t *testing.T) {
	// Test with no arguments (valid)
	err := ListCmd.Args(ListCmd, []string{})
	assert.NoError(t, err)

	// Test with arguments (invalid)
	err = ListCmd.Args(ListCmd, []string{"extra"})
	assert.Error(t, err)
}

func TestRemoteListRun(t *testing.T) {
	tmpDir := testutils.SetupTestGitRepo(t)
	testutils.CreateDefaultTestConfig(t, tmpDir)

	oldLoadProfileCredential := loadProfileCredential
	oldEnsureValidCredential := ensureValidCredential
	t.Cleanup(func() {
		loadProfileCredential = oldLoadProfileCredential
		ensureValidCredential = oldEnsureValidCredential
	})

	loadProfileCredential = func(profile string) (*syconf.Credential, error) {
		return &syconf.Credential{Profile: profile, AccessToken: "token", APIEndpoint: "https://example.test"}, nil
	}
	called := false
	ensureValidCredential = func(ctx context.Context, cred *syconf.Credential, _ *slog.Logger) error {
		called = true
		return nil
	}

	// Capture stdout
	output := testutils.CaptureStdout(t, func() {
		err := ListCmd.RunE(ListCmd, []string{})
		assert.NoError(t, err)
	})

	assert.Contains(t, output, "origin")
	assert.Contains(t, output, "gen3")
	assert.True(t, called, "expected remote list to validate the configured credential")
}

func TestRemoteSetArgs(t *testing.T) {
	// Test with 1 argument (valid)
	err := SetCmd.Args(SetCmd, []string{"origin"})
	assert.NoError(t, err)

	// Test with no arguments (invalid)
	err = SetCmd.Args(SetCmd, []string{})
	assert.Error(t, err)

	// Test with multiple arguments (invalid)
	err = SetCmd.Args(SetCmd, []string{"origin", "extra"})
	assert.Error(t, err)
}

func TestRemoteRemoveArgs(t *testing.T) {
	err := RemoveCmd.Args(RemoveCmd, []string{"origin"})
	assert.NoError(t, err)

	err = RemoveCmd.Args(RemoveCmd, []string{})
	assert.Error(t, err)

	err = RemoveCmd.Args(RemoveCmd, []string{"origin", "extra"})
	assert.Error(t, err)
}

func TestRemoteRemoveRunReassignsDefaultAndCleansKeys(t *testing.T) {
	tmpDir := testutils.SetupTestGitRepo(t)
	testutils.CreateTestConfig(t, tmpDir, &config.Config{
		DefaultRemote: "origin",
		Remotes: map[config.Remote]config.RemoteSelect{
			"origin": {
				Gen3: &config.Gen3Remote{
					Endpoint:  "https://origin.example",
					ProjectID: "origin-proj",
					Bucket:    "origin-bucket",
				},
			},
			"backup": {
				Gen3: &config.Gen3Remote{
					Endpoint:  "https://backup.example",
					ProjectID: "backup-proj",
					Bucket:    "backup-bucket",
				},
			},
		},
	})

	for _, args := range [][]string{
		{"config", "drs.remote.origin.token", "token"},
		{"config", "drs.remote.origin.username", "alice"},
		{"config", "drs.remote.origin.password", "secret"},
		{"config", "remote.origin.lfsurl", "https://origin.example/info/lfs"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		err := cmd.Run()
		assert.NoError(t, err)
	}

	err := RemoveCmd.RunE(RemoveCmd, []string{"origin"})
	assert.NoError(t, err)

	cfg, err := config.LoadConfig()
	assert.NoError(t, err)
	assert.NotContains(t, cfg.Remotes, config.Remote("origin"))
	assert.Equal(t, config.Remote("backup"), cfg.DefaultRemote)

	for _, key := range []string{
		"drs.remote.origin.type",
		"drs.remote.origin.endpoint",
		"drs.remote.origin.project",
		"drs.remote.origin.bucket",
		"drs.remote.origin.token",
		"drs.remote.origin.username",
		"drs.remote.origin.password",
		"remote.origin.lfsurl",
	} {
		val, err := exec.Command("git", "config", "--get", key).CombinedOutput()
		assert.Empty(t, string(val))
		assert.Error(t, err)
	}
}

func TestRemoteRemoveRunClearsDefaultWhenLastRemoteRemoved(t *testing.T) {
	tmpDir := testutils.SetupTestGitRepo(t)
	testutils.CreateDefaultTestConfig(t, tmpDir)

	err := RemoveCmd.RunE(RemoveCmd, []string{"origin"})
	assert.NoError(t, err)

	cfg, err := config.LoadConfig()
	assert.NoError(t, err)
	assert.Empty(t, cfg.Remotes)
	assert.Equal(t, config.Remote(""), cfg.DefaultRemote)

	val, err := exec.Command("git", "config", "--get", "drs.default-remote").CombinedOutput()
	assert.Empty(t, string(val))
	assert.Error(t, err)
}
