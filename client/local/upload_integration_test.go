package local

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManualUpload_Integration(t *testing.T) {
	if os.Getenv("GIT_DRS_RUN_LOCAL_INTEGRATION") != "true" {
		t.Skip("set GIT_DRS_RUN_LOCAL_INTEGRATION=true to run local server integration test")
	}

	// 1. Configuration
	baseURL := "http://localhost:8080"
	bucket := "cbds"
	project := "git_drs_e2e_test"
	organization := "cbdsTest"

	// 2. Setup Client
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	remote := LocalRemote{
		BaseURL:      baseURL,
		Bucket:       bucket,
		ProjectID:    project,
		Organization: organization,
	}
	client := NewLocalClient(remote, logger)

	// 3. Create a unique dummy file
	timestamp := time.Now().Format(time.RFC3339Nano)
	content := []byte(fmt.Sprintf("Integration Test Content [%s]", timestamp))

	tmpDir := t.TempDir()
	fileName := "test_upload.greeting"
	filePath := filepath.Join(tmpDir, fileName)
	downloadPath := filepath.Join(tmpDir, "downloaded.greeting")

	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	// 4. Calculate SHA256 (OID)
	hasher := sha256.New()
	hasher.Write(content)
	oid := hex.EncodeToString(hasher.Sum(nil))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 5. Execute RegisterFile (Triggers Upload via GetUploadURL + Upload)
	t.Logf("Step 1: Registering and Uploading OID: %s", oid)
	drsObj, err := client.RegisterFile(ctx, oid, filePath)
	if err != nil {
		t.Fatalf("RegisterFile/Upload failed: %v", err)
	}

	// 6. Verification: Download back using the DRS logic
	t.Logf("Step 2: Downloading file back from signed URL...")
	// DownloadFile uses client.Backend.GetDownloadURL and client.Backend.Download
	err = client.DownloadFile(ctx, drsObj.Id, downloadPath)
	if err != nil {
		t.Fatalf("DownloadFile failed: %v", err)
	}

	// 7. Verification: Compare content
	downloadedContent, err := os.ReadFile(downloadPath)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}

	if !bytes.Equal(content, downloadedContent) {
		t.Errorf("Content mismatch!\nExpected: %s\nGot:      %s", string(content), string(downloadedContent))
	} else {
		t.Logf("Success: Uploaded and Downloaded content matches exactly.")
	}
}
