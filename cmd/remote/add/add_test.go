package add

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calypr/git-drs/internal/testutils"
	bucketapi "github.com/calypr/syfon/apigen/bucketapi"
	syconf "github.com/calypr/syfon/client/config"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
)

func TestAddCmd(t *testing.T) {
	assert.Equal(t, "add", Cmd.Use)
	assert.NotEmpty(t, Cmd.Short)
}

func TestGen3Cmd(t *testing.T) {
	assert.Equal(t, "gen3 [remote-name] <organization/project>", Gen3Cmd.Use)
	assert.Empty(t, Gen3Cmd.Deprecated)
	assert.False(t, Gen3Cmd.Hidden)
}

func TestGen3InitRefreshesWithoutExistingProfileStore(t *testing.T) {
	testutils.SetupTestGitRepo(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user/credentials/api/access_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"refreshed-token"}`))
		case "/data/buckets":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"S3_BUCKETS":{"cbds":{"programs":["/organization/HTAN_INT/project/BForePC"]}}}`))
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	apiKey, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": server.URL,
		"iat": time.Now().Add(-time.Hour).Unix(),
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	}).SignedString([]byte("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	credentialFile := filepath.Join(home, "credentials.json")
	credentialJSON, err := json.Marshal(map[string]string{"api_key": apiKey})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialFile, credentialJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	if err := gen3Init("research", credentialFile, "", "", "HTAN_INT/BForePC", logger); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "failed to save refreshed token") || strings.Contains(logs.String(), "error occurred when loading config file") {
		t.Fatalf("unexpected credential save warning: %s", logs.String())
	}
	profile, err := syconf.NewConfigure(nil).Load("research")
	if err != nil {
		t.Fatalf("load saved Gen3 profile: %v", err)
	}
	if profile.AccessToken != "refreshed-token" {
		t.Fatalf("saved access token = %q, want refreshed token", profile.AccessToken)
	}
}

func TestParseScopeArg(t *testing.T) {
	t.Run("splits org and project on slash", func(t *testing.T) {
		org, project, err := parseScopeArg("HTAN_INT/BForePC")
		if err != nil {
			t.Fatalf("parseScopeArg returned error: %v", err)
		}
		if org != "HTAN_INT" || project != "BForePC" {
			t.Fatalf("unexpected scope parse result: %q/%q", org, project)
		}
	})

	t.Run("rejects legacy single token input", func(t *testing.T) {
		_, _, err := parseScopeArg("BForePC")
		if err == nil {
			t.Fatal("expected invalid scope error")
		}
	})

	t.Run("rejects empty org or project", func(t *testing.T) {
		for _, raw := range []string{"/BForePC", "HTAN_INT/", "HTAN_INT//BForePC"} {
			_, _, err := parseScopeArg(raw)
			if err == nil {
				t.Fatalf("expected invalid scope error for %q", raw)
			}
		}
	})
}

func TestResolveBucketScopeFromServer(t *testing.T) {
	t.Run("rejects missing endpoint or token", func(t *testing.T) {
		for _, tc := range []struct {
			name, endpoint, token, want string
		}{
			{name: "endpoint", endpoint: "", token: "test-token", want: "missing API endpoint"},
			{name: "token", endpoint: "http://example.test", token: "", want: "missing access token"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := resolveBucketScopeFromServer(context.Background(), tc.endpoint, tc.token, "org", "project", "")
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("error = %v, want substring %q", err, tc.want)
				}
			})
		}
	})

	t.Run("matches project resource", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/data/buckets" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Fatalf("unexpected auth header: %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"S3_BUCKETS":{"cbds":{"programs":["/organization/HTAN_INT/project/BForePC"]}}}`))
		}))
		defer srv.Close()

		scope, err := resolveBucketScopeFromServer(context.Background(), srv.URL, "test-token", "HTAN_INT", "BForePC", "")
		if err != nil {
			t.Fatalf("resolveBucketScopeFromServer returned error: %v", err)
		}
		if scope.Bucket != "cbds" {
			t.Fatalf("unexpected bucket: %+v", scope)
		}
	})

	t.Run("falls back to org resource", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"S3_BUCKETS":{"cbds":{"programs":["/organization/HTAN_INT"]}}}`))
		}))
		defer srv.Close()

		scope, err := resolveBucketScopeFromServer(context.Background(), srv.URL, "test-token", "HTAN_INT", "BForePC", "")
		if err != nil {
			t.Fatalf("resolveBucketScopeFromServer returned error: %v", err)
		}
		if scope.Bucket != "cbds" {
			t.Fatalf("unexpected bucket: %+v", scope)
		}
	})

	t.Run("no match", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := bucketapi.BucketsResponse{S3BUCKETS: map[string]bucketapi.BucketMetadata{
				"cbds": {},
			}}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Fatalf("encode response: %v", err)
			}
		}))
		defer srv.Close()

		_, err := resolveBucketScopeFromServer(context.Background(), srv.URL, "test-token", "HTAN_INT", "BForePC", "")
		if err == nil {
			t.Fatal("expected error when no matching bucket is visible")
		}
	})

	t.Run("reports ambiguity with candidate buckets", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := bucketapi.BucketsResponse{S3BUCKETS: map[string]bucketapi.BucketMetadata{
				"EllrottLab": {Programs: &[]string{"/organization/Ellrott_Lab/project/hla2vec"}},
				"cbds":       {Programs: &[]string{"/organization/Ellrott_Lab/project/hla2vec"}},
			}}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Fatalf("encode response: %v", err)
			}
		}))
		defer srv.Close()

		_, err := resolveBucketScopeFromServer(context.Background(), srv.URL, "test-token", "Ellrott_Lab", "hla2vec", "")
		if err == nil {
			t.Fatal("expected ambiguity error")
		}
		if !strings.Contains(err.Error(), "multiple visible server buckets matched") {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(err.Error(), "EllrottLab, cbds") {
			t.Fatalf("expected candidate list in error, got: %v", err)
		}
	})

	t.Run("uses selected bucket when ambiguity exists", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := bucketapi.BucketsResponse{S3BUCKETS: map[string]bucketapi.BucketMetadata{
				"EllrottLab": {Programs: &[]string{"/organization/Ellrott_Lab/project/hla2vec"}},
				"cbds":       {Programs: &[]string{"/organization/Ellrott_Lab/project/hla2vec"}},
			}}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Fatalf("encode response: %v", err)
			}
		}))
		defer srv.Close()

		scope, err := resolveBucketScopeFromServer(context.Background(), srv.URL, "test-token", "Ellrott_Lab", "hla2vec", "cbds")
		if err != nil {
			t.Fatalf("resolveBucketScopeFromServer returned error: %v", err)
		}
		if scope.Bucket != "cbds" {
			t.Fatalf("unexpected bucket: %+v", scope)
		}
	})
}
