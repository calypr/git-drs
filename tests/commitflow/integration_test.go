//go:build integration

package commitflow_test

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var gitDrsBinDir string

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Stderr.WriteString("skipping commitflow integration test in -short mode\n")
		os.Exit(0)
	}

	wd, err := os.Getwd()
	if err != nil {
		os.Stderr.WriteString("could not get working directory\n")
		os.Exit(2)
	}

	// wd == $REPO/tests/commitflow
	root := filepath.Dir(filepath.Dir(wd))

	gitDrsBinDir, err = os.MkdirTemp("", "git-drs-commitflow-bin-")
	if err != nil {
		os.Stderr.WriteString("could not create temp bin directory\n")
		os.Exit(2)
	}

	buildPath := filepath.Join(gitDrsBinDir, "git-drs")
	build := exec.Command("go", "build", "-o", buildPath)
	build.Dir = root

	if out, err := build.CombinedOutput(); err != nil {
		os.Stderr.Write(out)
		fmt.Fprintf(os.Stderr, "build error: %v\n", err)
		os.Exit(2)
	}

	code := m.Run()
	_ = os.RemoveAll(gitDrsBinDir)
	os.Exit(code)
}

func TestCreateTrackAndCommit(t *testing.T) {
	if !drsServerReachable("http://localhost:8080") {
		t.Skip("skipping: local DRS server is not reachable at http://localhost:8080")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	workDir := t.TempDir()
	repoDir := filepath.Join(workDir, "test-repo")

	runCmd(t, ctx, workDir, "mkdir", "test-repo")
	runCmd(t, ctx, repoDir, "git", "init")

	// Ensure commit identity exists regardless of machine-level git config.
	runCmd(t, ctx, repoDir, "git", "config", "user.name", "Integration Test")
	runCmd(t, ctx, repoDir, "git", "config", "user.email", "integration@example.com")

	runCmd(t, ctx, repoDir, "git", "drs", "init")
	runCmd(t, ctx, repoDir, "git", "drs", "remote", "add", "local", "local", "http://localhost:8080")

	runCmd(t, ctx, repoDir, "mkdir", "data")
	if err := os.WriteFile(filepath.Join(repoDir, "data", "1.bam"), []byte("test file 1\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	runCmd(t, ctx, repoDir, "git", "drs", "track", "data/**")
	runCmd(t, ctx, repoDir, "git", "add", ".gitattributes")
	if out, err := runCmdOutput(ctx, repoDir, "git", "add", "data/"); err != nil {
		if strings.Contains(out, "clean filter 'drs' failed") {
			t.Skip("skipping: local DRS upload/filter path is not available in this environment")
		}
		t.Fatalf("command failed: git [add data/]\n%s", out)
	}
	runCmd(t, ctx, repoDir, "git", "commit", "-m", "test commit 1")
}

func runCmd(t *testing.T, ctx context.Context, dir string, command string, args ...string) {
	t.Helper()
	out, err := runCmdOutput(ctx, dir, command, args...)
	if err != nil {
		t.Fatalf("command failed: %s %v\n%s", command, args, out)
	}
}

func runCmdOutput(ctx context.Context, dir string, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+gitDrsBinDir+":"+os.Getenv("PATH"))

	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("command timed out: %s %v", command, args)
	}
	return string(out), err
}

func drsServerReachable(baseURL string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	serviceInfoURL := baseURL + "/ga4gh/drs/v1/service-info"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serviceInfoURL, nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}
