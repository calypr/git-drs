package globusauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/scttfrdmn/globus-go-sdk/v4/pkg/authorizers"
	"github.com/scttfrdmn/globus-go-sdk/v4/pkg/core"
	"github.com/scttfrdmn/globus-go-sdk/v4/pkg/services/transfer"
	"github.com/scttfrdmn/globus-go-sdk/v4/pkg/tokenstorage"
)

func sdkClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	client, err := transfer.NewClient(context.Background(), &core.Config{
		Authorizer: authorizers.NewAccessTokenAuthorizer("token"),
		Scopes:     []string{TransferScope},
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return &Client{transfer: client, authorizer: authorizers.NewAccessTokenAuthorizer("token"), httpClient: server.Client(), baseURL: server.URL}, server
}

func TestNewClientRequiresStoredOrEnvironmentToken(t *testing.T) {
	t.Setenv(TransferTokenEnv, "")
	t.Setenv(TokenFileEnv, filepath.Join(t.TempDir(), "tokens.json"))
	if _, err := NewClient(context.Background()); err == nil {
		t.Fatal("expected missing token error")
	}
}

func TestCredentialReadinessRecognizesStoredToken(t *testing.T) {
	t.Setenv(TransferTokenEnv, "")
	t.Setenv(TokenFileEnv, filepath.Join(t.TempDir(), "tokens.json"))
	storage, _, err := openStorage()
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Store(&tokenDataForTest); err != nil {
		t.Fatal(err)
	}
	storage.Close()
	if state, reason := CredentialReadiness(); state != Ready {
		t.Fatalf("readiness = %s (%s)", state, reason)
	}
}

func TestLoginScopesMakesDataAccessDependentOnTransfer(t *testing.T) {
	a := "https://auth.globus.org/scopes/a/data_access"
	b := "https://auth.globus.org/scopes/b/data_access"
	got := loginScopes([]string{a, "openid", b})
	want := TransferScope + "[*" + a + " *" + b + "]"
	if len(got) != 2 || got[0] != want || got[1] != "openid" {
		t.Fatalf("loginScopes = %#v, want [%q %q]", got, want, "openid")
	}
}

func TestCredentialReadinessReportsBrokenStorage(t *testing.T) {
	t.Setenv(TransferTokenEnv, "")
	name := filepath.Join(t.TempDir(), "tokens.json")
	t.Setenv(TokenFileEnv, name)
	if err := os.WriteFile(name, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if state, _ := CredentialReadiness(); state != Broken {
		t.Fatalf("readiness = %s, want broken", state)
	}
}

func TestCredentialFilesAreOwnerOnly(t *testing.T) {
	name := filepath.Join(t.TempDir(), "credentials", "tokens.json")
	t.Setenv(TokenFileEnv, name)
	storage, _, err := openStorage()
	if err != nil {
		t.Fatal(err)
	}
	storage.Close()
	if err := saveClientID(name, "client-id"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Dir(name), name, clientIDFile(name)} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0o600)
		if info.IsDir() {
			want = 0o700
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s permissions = %o, want %o", path, info.Mode().Perm(), want)
		}
	}
	t.Setenv(ClientIDEnv, "")
	if got, err := configuredClientID(name); err != nil || got != "client-id" {
		t.Fatalf("configuredClientID = %q, %v", got, err)
	}
}

func TestOpenStoragePreservesExistingDirectoryPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(TokenFileEnv, filepath.Join(dir, "tokens.json"))
	storage, _, err := openStorage()
	if err != nil {
		t.Fatal(err)
	}
	storage.Close()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("directory permissions = %o, want 755", info.Mode().Perm())
	}
}

func TestCheckUsesTransferAPI(t *testing.T) {
	var gotAuth string
	client, server := sdkClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v0.10/task_list" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"DATA_TYPE": "task_list", "DATA": []any{}})
	}))
	defer server.Close()

	if err := Check(context.Background(), client); err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if gotAuth != "Bearer token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestStoredTokenDiagnosticReportsPathAndExpiry(t *testing.T) {
	name := filepath.Join(t.TempDir(), "tokens.json")
	t.Setenv(TokenFileEnv, name)
	storage, _, err := openStorage()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	token := tokenDataForTest
	token.ExpiresAt = time.Date(2026, 8, 17, 20, 0, 0, 0, time.UTC)
	if err := storage.Store(&token); err != nil {
		t.Fatal(err)
	}
	want := "token file " + name + "; recorded expiry 2026-08-17T20:00:00Z"
	if got := storedTokenDiagnostic(storage); got != want {
		t.Fatalf("storedTokenDiagnostic = %q, want %q", got, want)
	}
}

func TestSubmitTransferUsesSDKSubmissionIDAndPayload(t *testing.T) {
	client, server := sdkClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0.10/submission_id":
			_ = json.NewEncoder(w).Encode(map[string]string{"value": "submission-1"})
		case "/v0.10/transfer":
			var payload transfer.Transfer
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.SubmissionID != "submission-1" || len(payload.Items) != 1 {
				t.Fatalf("unexpected payload: %+v", payload)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"task_id": "task-1"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	taskID, err := client.SubmitTransfer(context.Background(), "src", "/src/file", "dst", "/dst/file", "label")
	if err != nil {
		t.Fatal(err)
	}
	if taskID != "task-1" {
		t.Fatalf("taskID = %q", taskID)
	}
}

func TestSubmitTransferItemsBatchesFilesAndChecksums(t *testing.T) {
	client, server := sdkClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0.10/submission_id":
			_ = json.NewEncoder(w).Encode(map[string]string{"value": "submission-1"})
		case "/v0.10/transfer":
			var payload transfer.Transfer
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if len(payload.Items) != 2 || payload.Items[0].ExternalChecksum != "abc" || payload.Items[0].ChecksumAlgorithm != "SHA256" || !payload.VerifyChecksum {
				t.Fatalf("unexpected payload: %+v", payload)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"task_id": "task-1"})
		}
	}))
	defer server.Close()
	_, err := client.SubmitTransferItems(t.Context(), "src", "dst", []TransferItem{{SourcePath: "/a", DestinationPath: "/x", SHA256: "abc"}, {SourcePath: "/b", DestinationPath: "/y"}}, "label")
	if err != nil {
		t.Fatal(err)
	}
}

func TestListFilesRecursesAndSorts(t *testing.T) {
	client, server := sdkClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") == "/root/" {
			if r.URL.Query().Get("offset") == "2" {
				_ = json.NewEncoder(w).Encode(map[string]any{"DATA": []map[string]any{{"name": "c", "type": "file", "size": 3}}, "total": 3})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"DATA": []map[string]any{{"name": "b", "type": "file", "size": 2, "last_modified": "2026-08-12 19:08:37+00:00"}, {"name": "sub", "type": "dir", "last_modified": "2026-08-12 19:08:37+00:00"}}, "total": 3})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"DATA": []map[string]any{{"name": "a", "type": "file", "size": 1}}, "total": 1})
	}))
	defer server.Close()
	files, err := client.ListFiles(t.Context(), "collection", "/root")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || files[0].Path != "/root/b" || files[0].LastModified != "2026-08-12 19:08:37+00:00" || files[1].Path != "/root/c" || files[2].Path != "/root/sub/a" {
		t.Fatalf("files = %+v", files)
	}
}

func TestStatFile(t *testing.T) {
	client, server := sdkClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0.10/operation/endpoint/collection/stat" || r.URL.Query().Get("path") != "/root/a" {
			t.Fatalf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "file", "size": 12, "last_modified": "2026-08-12 19:08:37+00:00"})
	}))
	defer server.Close()
	file, err := client.StatFile(t.Context(), "collection", "/root/a")
	if err != nil {
		t.Fatal(err)
	}
	if file.Path != "/root/a" || file.Size != 12 || file.LastModified == "" || file.Directory {
		t.Fatalf("file = %+v", file)
	}
}

func TestWaitForTaskRecoversFromInactiveStatus(t *testing.T) {
	calls := 0
	client, server := sdkClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		status := "INACTIVE"
		if calls == 2 {
			status = "SUCCEEDED"
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
	}))
	defer server.Close()

	if err := client.WaitForTask(context.Background(), "task-1", time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestWaitForTaskStopsOnInactiveFatalError(t *testing.T) {
	client, server := sdkClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":      "INACTIVE",
			"nice_status": "Permission denied",
			"fatal_error": map[string]string{"description": "destination permission denied"},
		})
	}))
	defer server.Close()

	err := client.WaitForTask(context.Background(), "task-1", time.Hour)
	if err == nil || !strings.Contains(err.Error(), "destination permission denied") {
		t.Fatalf("WaitForTask error = %v", err)
	}
}

func TestLogoutRemovesStoredTokens(t *testing.T) {
	t.Setenv(TokenFileEnv, filepath.Join(t.TempDir(), "tokens.json"))
	storage, _, err := openStorage()
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Store(&tokenDataForTest); err != nil {
		t.Fatal(err)
	}
	storage.Close()
	if err := Logout(); err != nil {
		t.Fatal(err)
	}
	storage, _, err = openStorage()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.Close()
	got, err := storage.Get(transferResource)
	if err != nil || got != nil {
		t.Fatalf("token after logout = %+v, err = %v", got, err)
	}
}

var tokenDataForTest = tokenstorage.TokenData{
	ResourceServer: transferResource,
	AccessToken:    "token",
	ExpiresAt:      time.Now().Add(time.Hour),
}
