package delete

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/calypr/git-drs/internal/remoteruntime"
	"github.com/calypr/git-drs/internal/testutils"
	syclient "github.com/calypr/syfon/client"
	"github.com/stretchr/testify/assert"
)

func TestDeleteCmdArgs(t *testing.T) {
	// Test with 2 arguments (valid)
	err := Cmd.Args(Cmd, []string{"sha256", "oid"})
	assert.NoError(t, err)

	// Test with 1 argument (invalid)
	err = Cmd.Args(Cmd, []string{"sha256"})
	assert.Error(t, err)

	// Test with 3 arguments (invalid)
	err = Cmd.Args(Cmd, []string{"sha256", "oid", "extra"})
	assert.Error(t, err)
}

func TestDeleteRun_Error(t *testing.T) {
	tmpDir := testutils.SetupTestGitRepo(t)
	// No config
	err := Cmd.RunE(Cmd, []string{"sha256", "oid"})
	assert.Error(t, err)

	// Invalid hash type
	testutils.CreateDefaultTestConfig(t, tmpDir)
	err = Cmd.RunE(Cmd, []string{"md5", "oid"})
	assert.Error(t, err)
}

func TestDeleteByHashUsesBulkHashEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/index/bulk/delete" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var request struct {
			Hashes []string `json:"hashes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if len(request.Hashes) != 1 || request.Hashes[0] != "abc" {
			t.Errorf("unexpected hashes: %#v", request.Hashes)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"deleted":1}`))
	}))
	defer server.Close()

	client, err := syclient.New(server.URL, syclient.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("new Syfon client: %v", err)
	}
	if err := deleteByHash(context.Background(), &remoteruntime.GitContext{Client: client}, "abc"); err != nil {
		t.Fatalf("deleteByHash returned error: %v", err)
	}
}
