package addurl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestGeneratePointerFile(t *testing.T) {
	testutils.SetupTestGitRepo(t)

	path := filepath.Join("data", "file.txt")
	err := generatePointerFile(path, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", 10)
	if err != nil {
		t.Fatalf("generatePointerFile error: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pointer file: %v", err)
	}

	if len(content) == 0 {
		t.Fatalf("expected pointer file content")
	}
}
