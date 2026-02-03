package indexd

import (
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/calypr/data-client/conf"
	"github.com/calypr/data-client/logs"
)

func TestNewGitDrsIdxdClient(t *testing.T) {
	// Setup mock server
	mock := &mockIndexdServer{}
	server := httptest.NewServer(mock.handler(t))
	defer server.Close()
	logger := logs.NewSlogNoOpLogger()

	// Setup inputs
	cred := conf.Credential{
		APIEndpoint: server.URL,
		Profile:     "test-profile",
	}

	remote := Gen3Remote{
		Endpoint:  server.URL,
		ProjectID: "test-project",
		Bucket:    "test-bucket",
	}

	// Call function
	client, err := NewGitDrsIdxdClient(cred, remote, logger)
	if err != nil {
		t.Fatalf("NewGitDrsIdxdClient failed: %v", err)
	}

	if client == nil {
		t.Fatal("Expected non-nil client")
	}

	// Verify internals if possible (type assertion)
	impl, ok := client.(*GitDrsIdxdClient)
	if !ok {
		t.Fatal("Expected *GitDrsIdxdClient implementation")
	}

	if impl.Config.ProjectId != "test-project" {
		t.Errorf("Expected ProjectId 'test-project', got %s", impl.Config.ProjectId)
	}
	if impl.Config.BucketName != "test-bucket" {
		t.Errorf("Expected BucketName 'test-bucket', got %s", impl.Config.BucketName)
	}

	// Verify Base URL matches mock server
	if impl.Base.String() != server.URL {
		t.Errorf("Expected Base URL %s, got %s", server.URL, impl.Base.String())
	}
}

func TestNewGitDrsIdxdClient_MissingProject(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cred := conf.Credential{APIEndpoint: "http://idx.example"}
	remote := Gen3Remote{Endpoint: "http://idx.example"} // Missing ProjectID

	_, err := NewGitDrsIdxdClient(cred, remote, logger)
	if err == nil {
		t.Error("Expected error for missing project ID")
	}
}

func TestGitDrsIdxdClient_Accessors(t *testing.T) {
	config := &Config{
		ProjectId:  "test-proj",
		BucketName: "test-bucket",
		Upsert:     true,
	}
	client := &GitDrsIdxdClient{
		Config: config,
	}

	if client.GetProjectId() != "test-proj" {
		t.Errorf("GetProjectId() = %q, want test-proj", client.GetProjectId())
	}
	if client.GetBucketName() != "test-bucket" {
		t.Errorf("GetBucketName() = %q, want test-bucket", client.GetBucketName())
	}
	if client.GetUpsert() != true {
		t.Errorf("GetUpsert() = %v, want true", client.GetUpsert())
	}
}
