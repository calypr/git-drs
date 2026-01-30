package indexd_client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bytedance/sonic/encoder"
	"github.com/calypr/git-drs/cloud"
)

type testAuthHandler struct {
	token string
}

func (t testAuthHandler) AddAuthHeader(req *http.Request) error {
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	return nil
}

func TestGetBucketDetailsWithAuth(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		resp := cloud.S3BucketsResponse{
			S3Buckets: map[string]*cloud.S3Bucket{
				"bucket": {Region: "us-east-1", EndpointURL: "https://s3.example.com"},
			},
		}
		w.WriteHeader(http.StatusOK)
		_ = encoder.NewStreamEncoder(w).Encode(resp)
	}))
	defer server.Close()

	bucket, err := GetBucketDetailsWithAuth(context.Background(), "bucket", server.URL, testAuthHandler{token: "token"}, server.Client())
	if err != nil {
		t.Fatalf("GetBucketDetailsWithAuth error: %v", err)
	}
	if authHeader != "Bearer token" {
		t.Fatalf("expected auth header set, got %q", authHeader)
	}
	if bucket.Region != "us-east-1" || bucket.EndpointURL != "https://s3.example.com" {
		t.Fatalf("unexpected bucket details: %+v", bucket)
	}
}

func TestGetBucketDetailsWithAuth_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := cloud.S3BucketsResponse{
			S3Buckets: map[string]*cloud.S3Bucket{},
		}
		w.WriteHeader(http.StatusOK)
		_ = encoder.NewStreamEncoder(w).Encode(resp)
	}))
	defer server.Close()

	bucket, err := GetBucketDetailsWithAuth(context.Background(), "missing", server.URL, nil, server.Client())
	if err != nil {
		t.Fatalf("GetBucketDetailsWithAuth error: %v", err)
	}
	if bucket != nil {
		t.Fatalf("expected nil bucket, got %+v", bucket)
	}
}
