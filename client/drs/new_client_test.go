package drs

import (
	"log/slog"
	"os"
	"testing"

	"github.com/calypr/data-client/conf"
	"github.com/calypr/data-client/logs"
)

func TestNewGitDrsClient(t *testing.T) {
	logger := logs.NewSlogNoOpLogger()

	cred := conf.Credential{
		APIEndpoint: "http://example.test",
		Profile:     "test-profile",
	}

	remote := Gen3Remote{
		Endpoint:  "http://example.test",
		ProjectID: "test-project",
		Bucket:    "test-bucket",
	}

	client, err := NewGitDrsClient(cred, remote, logger)
	if err != nil {
		t.Fatalf("NewGitDrsClient failed: %v", err)
	}

	if client == nil {
		t.Fatal("Expected non-nil client")
	}

	impl, ok := client.(*GitDrsClient)
	if !ok {
		t.Fatal("Expected *GitDrsClient implementation")
	}

	if impl.Config.ProjectId != "test-project" {
		t.Errorf("Expected ProjectId 'test-project', got %s", impl.Config.ProjectId)
	}
	if impl.Config.BucketName != "test-bucket" {
		t.Errorf("Expected BucketName 'test-bucket', got %s", impl.Config.BucketName)
	}

	if impl.Base.String() != "http://example.test" {
		t.Errorf("Expected Base URL %s, got %s", "http://example.test", impl.Base.String())
	}
}

func TestNewGitDrsClient_MissingProject(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cred := conf.Credential{APIEndpoint: "http://idx.example"}
	remote := Gen3Remote{Endpoint: "http://idx.example"} // Missing ProjectID

	_, err := NewGitDrsClient(cred, remote, logger)
	if err == nil {
		t.Error("Expected error for missing project ID")
	}
}

func TestGitDrsClient_Accessors(t *testing.T) {
	config := &Config{
		ProjectId:  "test-proj",
		BucketName: "test-bucket",
		Upsert:     true,
	}
	client := &GitDrsClient{
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
