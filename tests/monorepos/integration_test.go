// File: `./tests/monorepos/integration_test.go`
//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

var binPath string

func TestMain(m *testing.M) {
	// Call flag.Parse() to parse command-line flags, including testing flags.
	flag.Parse()
	if testing.Short() {
		os.Stderr.WriteString("skipping monorepo integration test in -short mode\n")
		os.Exit(0)
	}

	// when go test ./tests/monorepos runs, the working directory for that test binary is the package directory:
	// be a bit more robust about working directory:

	wd, err := os.Getwd()
	if err != nil {
		os.Stderr.WriteString("could not get working directory\n")
		os.Exit(2)
	}

	// wd == $REPO/tests/monorepos
	root := filepath.Dir(filepath.Dir(wd)) // go up two levels: tests/monorepos -> tests -> repo root

	tmp := os.TempDir()
	binPath = filepath.Join(tmp, "git-drs-integ")
	build := exec.Command("go", "build", "-o", binPath)
	build.Dir = root

	if out, err := build.CombinedOutput(); err != nil {
		os.Stderr.Write(out)
		fmt.Fprintf(os.Stderr, "build error: %v\n", err)
		os.Exit(2)
	}

	code := m.Run()
	_ = os.Remove(binPath)
	os.Exit(code)
}

func TestGitDrsShowsHelp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath) // , "init", "--cred", "/dev/null", "--profile", "test")
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("command timed out")
	}
	if err != nil {
		t.Fatalf("command failed: %v\n%s", err, out)
	}

	if !bytes.Contains(out, []byte("Available Commands")) {
		t.Fatalf("unexpected output: %s", out)
	}
}
