package drsmap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/calypr/git-drs/common"
)

// Mock helper process
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	// Check args to decide what to return
	args := os.Args
	for i, arg := range args {
		if arg == "push" && i+1 < len(args) && args[i+1] == "--dry-run" {
			// git lfs push --dry-run
			oid := os.Getenv("MOCK_LFS_OID")
			if oid != "" {
				fmt.Printf("push %s => dummy.bin\n", oid)
			}
			os.Exit(0)
		}
		if arg == "ls-files" {
			// Get filename (it's after -I)
			if i+2 < len(args) {
				filename := args[i+2]
				if filename == "data.bin" {
					fmt.Printf(`{"files":[{"Name":"data.bin","Size":11,"Checkout":true,"Downloaded":true,"OidType":"sha256","Oid":"hash","Version":"https://git-lfs.github.com/spec/v1"}]}`)
					os.Exit(0)
				}
				if filename == "readme.txt" {
					fmt.Print(`{"files":[]}`) // OR empty output
					os.Exit(0)
				}
			}
		}
	}
	os.Exit(0)
}

func TestCheckIfLfsFile(t *testing.T) {
	// Mock execCommand
	oldExec := execCommand
	defer func() { execCommand = oldExec }()
	execCommand = func(name string, arg ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", name}
		cs = append(cs, arg...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
		return cmd
	}

	// 1. Check LFS file
	isLfs, info, err := CheckIfLfsFile("data.bin")
	if err != nil {
		t.Fatalf("CheckIfLfsFile failed: %v", err)
	}
	if !isLfs {
		t.Error("Expected data.bin to be identified as LFS file")
	}
	if info == nil {
		t.Error("Expected info to be non-nil")
	} else if info.Name != "data.bin" {
		t.Errorf("Expected info name 'data.bin', got '%s'", info.Name)
	}

	// 2. Check non-LFS file
	isLfs, _, err = CheckIfLfsFile("readme.txt")
	if err != nil {
		t.Fatalf("CheckIfLfsFile failed for readme.txt: %v", err)
	}
	if isLfs {
		t.Error("Expected readme.txt NOT to be identified as LFS file")
	}
}

func TestUpdateDrsObjects(t *testing.T) {
	// Mock execCommandContext
	oldExec := execCommandContext
	defer func() { execCommandContext = oldExec }()
	execCommandContext = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", name}
		cs = append(cs, arg...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", fmt.Sprintf("MOCK_LFS_OID=%s", os.Getenv("MOCK_LFS_OID"))}
		return cmd
	}

	// 1. Setup temp repo
	repoDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origWd)

	// Create dummy LFS file map logic via helper output
	// UpdateDrsObjects calls GetAllLfsFiles -> RunLfsPushDryRun -> git lfs push --dry-run

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mockClient := &MockDRSClient{
		Project: "test-project",
	}

	// Create directories needed
	// UpdateDrsObjects calls GetObjectPath(projectdir.DRS_OBJS_PATH, oid)
	// We need to make sure projectdir.DRS_OBJS_PATH is valid or created.
	// Since we are in repoDir, .git/drs/lfs/objects needs to exist or be created by WriteDrsObj
	// WriteDrsObj calls MkdirAll, so we are good.

	// Also UpdateDrsObjects checks if file exists in projectdir.LFS_OBJS_PATH via GetObjectPath
	// Wait, UpdateDrsObjects:
	// "path, err := GetObjectPath(projectdir.LFS_OBJS_PATH, file.Oid)"
	// "if _, err := os.Stat(path); os.IsNotExist(err) { return error }"
	// So we MUST create the LFS object file at the expected path.
	// LFS_OBJS_PATH is ".git/lfs/objects".
	// The OID from our mock is "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" (sha of empty, or we use a dummy one)
	// Let's use dummy OID "1111111111111111111111111111111111111111111111111111111111111111"

	oid := "1111111111111111111111111111111111111111111111111111111111111111"
	lfsDir := filepath.Join(repoDir, ".git", "lfs", "objects", oid[:2], oid[2:4])
	os.MkdirAll(lfsDir, 0755)
	os.WriteFile(filepath.Join(lfsDir, oid), []byte("dummy content"), 0644)

	// Create the source file in worktree so GetAllLfsFiles can stat it
	os.WriteFile("dummy.bin", []byte("dummy content"), 0644)

	// Update helper process to return push dry run output
	os.Setenv("MOCK_LFS_OID", oid)
	defer os.Unsetenv("MOCK_LFS_OID")

	err := UpdateDrsObjects(mockClient, "origin", "", []string{"master"}, logger)
	if err != nil {
		t.Fatalf("UpdateDrsObjects failed: %v", err)
	}

	// Check if DRS object created
	drsPath, _ := GetObjectPath(common.DRS_OBJS_PATH, oid)
	if _, err := os.Stat(drsPath); os.IsNotExist(err) {
		t.Errorf("Expected DRS object file at %s", drsPath)
	}
}
