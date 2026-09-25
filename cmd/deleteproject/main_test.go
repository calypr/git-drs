package deleteproject

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/testutils"
)

func TestDeleteProjectRunERejectsMismatchedConfirm(t *testing.T) {
	var deleteRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/index":
			if got := r.URL.Query().Get("project"); got != "requested-project" {
				t.Errorf("expected project query requested-project, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"records": []any{}})
		case r.Method == http.MethodDelete && r.URL.Path == "/index":
			deleteRequests++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"deleted":1}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tmpDir := testutils.SetupTestGitRepo(t)
	testutils.CreateTestConfig(t, tmpDir, &config.Config{
		DefaultRemote: config.Remote(config.ORIGIN),
		Remotes: map[config.Remote]config.RemoteSelect{
			config.Remote(config.ORIGIN): {
				Local: &config.LocalRemote{BaseURL: server.URL, ProjectID: "configured-project"},
			},
		},
	})

	oldRemote, oldConfirm := remote, confirmFlag
	t.Cleanup(func() {
		remote, confirmFlag = oldRemote, oldConfirm
	})
	remote = ""
	confirmFlag = "wrong-project"

	err := Cmd.RunE(Cmd, []string{"requested-project"})
	if err == nil || !strings.Contains(err.Error(), "does not match project ID 'requested-project'") {
		t.Fatalf("expected mismatched confirmation error, got %v", err)
	}
	if deleteRequests != 0 {
		t.Fatalf("expected no delete request after confirmation mismatch, got %d", deleteRequests)
	}
}

func TestDeleteProjectRunEDeletesWithExactConfirm(t *testing.T) {
	var deleteRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/index":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"records":[]}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/index":
			deleteRequests++
			if got := r.URL.Query().Get("project"); got != "requested-project" {
				t.Errorf("expected project query requested-project, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"deleted":1}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	tmpDir := testutils.SetupTestGitRepo(t)
	testutils.CreateTestConfig(t, tmpDir, &config.Config{
		DefaultRemote: config.Remote(config.ORIGIN),
		Remotes: map[config.Remote]config.RemoteSelect{
			config.Remote(config.ORIGIN): {
				Local: &config.LocalRemote{BaseURL: server.URL, ProjectID: "configured-project"},
			},
		},
	})

	oldRemote, oldConfirm := remote, confirmFlag
	t.Cleanup(func() {
		remote, confirmFlag = oldRemote, oldConfirm
	})
	remote = ""
	confirmFlag = "requested-project"

	if err := Cmd.RunE(Cmd, []string{"requested-project"}); err != nil {
		t.Fatalf("expected exact confirmation to permit deletion, got %v", err)
	}
	if deleteRequests != 1 {
		t.Fatalf("expected one delete request, got %d", deleteRequests)
	}
}
