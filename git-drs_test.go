package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionOutsideRepository(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "git-drs")
	build := exec.Command("go", "build", "-o", binary, ".")
	output, err := build.CombinedOutput()
	require.NoError(t, err, "%s", output)

	dir := t.TempDir()
	command := exec.Command(binary, "version")
	command.Dir = dir
	// Prevent an inherited Git environment from pointing at a repository.
	command.Env = append(os.Environ(), "GIT_DIR=", "GIT_WORK_TREE=", "GIT_CEILING_DIRECTORIES="+filepath.Dir(dir))
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err = command.Output()
	require.NoError(t, err, "%s", stderr.String())
	require.Empty(t, stderr.String())
	require.Regexp(t, `^git-drs \S+\n$`, string(output))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "version should not create repository or log files")
}
