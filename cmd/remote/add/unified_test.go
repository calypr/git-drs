package add

import (
	"bytes"
	"os"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/testutils"
)

func TestUnifiedAddExpandsPreset(t *testing.T) {
	testutils.SetupTestGitRepo(t)
	resetUnifiedFlags(t)
	credentialFlag = "env:CGC_TOKEN"
	var out bytes.Buffer
	Cmd.SetOut(&out)
	if err := runUnified(Cmd, []string{"cgc"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Remotes["cgc"].Generic
	if r == nil || r.Provider != "cgc" || r.Auth != "bearer" || r.PresetVersion != 1 || r.Credential != "env:CGC_TOKEN" {
		t.Fatalf("unexpected remote: %+v", r)
	}
	if out.String() == "" {
		t.Fatal("expected effective configuration output")
	}
}

func TestUnifiedAddDerivesURLNameAndRejectsHTTP(t *testing.T) {
	testutils.SetupTestGitRepo(t)
	resetUnifiedFlags(t)
	if err := runUnified(Cmd, []string{"http://drs.example.org"}); err == nil {
		t.Fatal("expected HTTP rejection")
	}
	providerFlag = "ga4gh"
	if err := runUnified(Cmd, []string{"https://drs.example.org"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Remotes["drs.example.org"].Generic == nil {
		t.Fatal("derived remote not persisted")
	}
}

func TestUnifiedAddRejectsURLWithoutProviderBeforeInitializing(t *testing.T) {
	repo := testutils.SetupTestGitRepo(t)
	resetUnifiedFlags(t)

	err := runUnified(Cmd, []string{"https://drs.example.org"})
	if err == nil {
		t.Fatal("expected provider auto rejection")
	}
	if _, statErr := os.Stat(repo + "/.git-drs"); !os.IsNotExist(statErr) {
		t.Fatalf("remote validation modified repository state: .git-drs stat error = %v", statErr)
	}
	cfg, loadErr := config.LoadConfig()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(cfg.Remotes) != 0 {
		t.Fatalf("remote validation persisted configuration: %+v", cfg.Remotes)
	}
}

func resetUnifiedFlags(t *testing.T) {
	old := []string{scopeFlag, authFlag, credentialFlag, providerFlag, storageFlag, checkoutFlag}
	scopeFlag, authFlag, credentialFlag, providerFlag, storageFlag, checkoutFlag = "", "auto", "", "auto", "", ""
	t.Cleanup(func() {
		scopeFlag, authFlag, credentialFlag, providerFlag, storageFlag, checkoutFlag = old[0], old[1], old[2], old[3], old[4], old[5]
	})
}
