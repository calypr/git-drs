package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCacheArgs(t *testing.T) {
	// Test with 1 argument (valid)
	err := Cmd.Args(Cmd, []string{"manifest.tsv"})
	assert.NoError(t, err)

	// Test with no arguments (invalid)
	err = Cmd.Args(Cmd, []string{})
	assert.Error(t, err)

	// Test with multiple arguments (invalid)
	err = Cmd.Args(Cmd, []string{"m1.tsv", "m2.tsv"})
	assert.Error(t, err)
}

func TestCacheRun(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cache-test-*")
	assert.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	originalDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalDir)

	validSHA := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	manifestPath := filepath.Join(tmpDir, "manifest.tsv")
	content := "files.sha256\tfiles.drs_uri\n" + validSHA + "\tdrs://example.com:obj1\n"
	err = os.WriteFile(manifestPath, []byte(content), 0644)
	assert.NoError(t, err)

	err = Cmd.RunE(Cmd, []string{manifestPath})
	assert.NoError(t, err)

	// Verify cache directory was created
	_, err = os.Stat(".git/drs/objects")
	assert.NoError(t, err)
}
