package indexd_client

import (
	"log/slog"
	"os"
	"testing"
)

func TestGen3Remote_Getters(t *testing.T) {
	r := Gen3Remote{
		Endpoint:  "http://example.com",
		ProjectID: "test-project",
		Bucket:    "test-bucket",
	}

	if r.GetEndpoint() != "http://example.com" {
		t.Errorf("GetEndpoint() = %q, want %q", r.GetEndpoint(), "http://example.com")
	}

	if r.GetProjectId() != "test-project" {
		t.Errorf("GetProjectId() = %q, want %q", r.GetProjectId(), "test-project")
	}

	if r.GetBucketName() != "test-bucket" {
		t.Errorf("GetBucketName() = %q, want %q", r.GetBucketName(), "test-bucket")
	}
}

func TestGen3Remote_GetClient_Error(t *testing.T) {
	r := Gen3Remote{}
	params := map[string]string{"remote_name": "non-existent-remote"}

	// Create a logger
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	client, err := r.GetClient(params, logger)
	if err == nil {
		t.Error("Expected error for non-existent remote config")
	}
	if client != nil {
		t.Error("Expected nil client on error")
	}
}
