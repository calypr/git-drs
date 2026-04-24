package lfs

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestAddFilesFromDryRun(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Setup mock files on disk
	files := map[string]string{
		"simple.txt":                          "content1",
		"path with spaces.txt":                "content2",
		"folder/file.txt":                     "content3",
		"arrow => file.txt":                   "content4",
		"my favorite directory/test file.csv": "content5",
		"data/BigMHC Training and Evaluation Data/el_train.csv": "content6",
	}

	for path, content := range files {
		fullPath := filepath.Join(tempDir, filepath.FromSlash(path))
		err := os.MkdirAll(filepath.Dir(fullPath), 0755)
		if err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		// Write enough content to be > 2048 bytes to skip the LFS pointer check
		// Or just write something that doesn't look like a pointer.
		padding := make([]byte, 2048)
		err = os.WriteFile(fullPath, append(padding, []byte(content)...), 0644)
		if err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
	}

	// 2. Define dry-run output scenarios
	oid1 := "1111111111111111111111111111111111111111111111111111111111111111"
	oid2 := "2222222222222222222222222222222222222222222222222222222222222222"
	oid3 := "3333333333333333333333333333333333333333333333333333333333333333"
	oid4 := "4444444444444444444444444444444444444444444444444444444444444444"
	oid5 := "5555555555555555555555555555555555555555555555555555555555555555"
	oid6 := "6666666666666666666666666666666666666666666666666666666666666666"

	dryRunOutput := oid1 + " simple.txt\n" +
		"(1/1) " + oid2 + " path with spaces.txt\n" +
		oid3 + " => folder/file.txt\n" + // Note: '=>' isn't really used this way by LFS, but testing our robustness
		oid4 + " -> arrow => file.txt\n" + // Testing separator + spaces + arrow in filename
		oid5 + " my favorite directory/test file.csv\n" +
		oid6 + " data/BigMHC Training and Evaluation Data/el_train.csv\n"

	// 3. Run the parser
	lfsFileMap := make(map[string]LfsFileInfo)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	err := addFilesFromDryRun(dryRunOutput, tempDir, logger, lfsFileMap)
	if err != nil {
		t.Fatalf("addFilesFromDryRun failed: %v", err)
	}

	// 4. Validate results
	expected := []struct {
		path string
		oid  string
	}{
		{"simple.txt", oid1},
		{"path with spaces.txt", oid2},
		{"folder/file.txt", oid3},
		{"arrow => file.txt", oid4},
		{"my favorite directory/test file.csv", oid5},
		{"data/BigMHC Training and Evaluation Data/el_train.csv", oid6},
	}

	if len(lfsFileMap) != len(expected) {
		t.Errorf("expected %d files, got %d", len(expected), len(lfsFileMap))
	}

	for _, exp := range expected {
		info, ok := lfsFileMap[exp.path]
		if !ok {
			t.Errorf("expected path %q not found in map", exp.path)
			continue
		}
		if info.Oid != exp.oid {
			t.Errorf("path %q: expected OID %q, got %q", exp.path, exp.oid, info.Oid)
		}
		if info.Size <= 2048 {
			t.Errorf("path %q: expected size > 2048, got %d", exp.path, info.Size)
		}
	}
}
