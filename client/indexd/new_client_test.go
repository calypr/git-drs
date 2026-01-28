package indexd

import (
	"log/slog"
	"os"
	"testing"

	"github.com/calypr/data-client/conf"
)

func TestNewGitDrsIdxdClient(t *testing.T) {
	// Setup logger
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Setup inputs
	cred := conf.Credential{
		APIEndpoint: "http://indexd.example.com",
		Profile:     "test-profile",
	}

	remote := Gen3Remote{
		Endpoint:  "http://indexd.example.com",
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
