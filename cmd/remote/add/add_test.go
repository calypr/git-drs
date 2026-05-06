package add

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	bucketapi "github.com/calypr/syfon/apigen/client/bucketapi"
	"github.com/stretchr/testify/assert"
)

func TestAddCmd(t *testing.T) {
	assert.Equal(t, "add", Cmd.Use)
	assert.NotEmpty(t, Cmd.Short)
}

func TestGen3Cmd(t *testing.T) {
	assert.Equal(t, "gen3 [remote-name] <organization/project>", Gen3Cmd.Use)
}

func TestResolveBucketScopeFromServer(t *testing.T) {
	t.Run("matches project resource", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/data/buckets" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Fatalf("unexpected auth header: %q", got)
			}
			_, _ = w.Write([]byte(`{"S3_BUCKETS":{"cbds":{"programs":["/organization/HTAN_INT/project/BForePC"]}}}`))
		}))
		defer srv.Close()

		scope, err := resolveBucketScopeFromServer(context.Background(), srv.URL, "test-token", "HTAN_INT", "BForePC")
		if err != nil {
			t.Fatalf("resolveBucketScopeFromServer returned error: %v", err)
		}
		if scope.Bucket != "cbds" {
			t.Fatalf("unexpected bucket: %+v", scope)
		}
	})

	t.Run("falls back to org resource", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"S3_BUCKETS":{"cbds":{"programs":["/organization/HTAN_INT"]}}}`))
		}))
		defer srv.Close()

		scope, err := resolveBucketScopeFromServer(context.Background(), srv.URL, "test-token", "HTAN_INT", "BForePC")
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

		_, err := resolveBucketScopeFromServer(context.Background(), srv.URL, "test-token", "HTAN_INT", "BForePC")
		if err == nil {
			t.Fatal("expected error when no matching bucket is visible")
		}
	})
}
