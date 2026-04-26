package smudge

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestSmudgeCmd(t *testing.T) {
	testutils.RunCmdMainTest(t, "smudge")
}

func TestValidateArgs(t *testing.T) {
	testutils.RunCmdArgsTest(t)
}

func TestRunSmudgePassesThroughWithoutRemote(t *testing.T) {
	repoDir := t.TempDir()
	cmd := exec.Command("git", "init", repoDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, string(out))
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	pointer := []byte("version https://git-lfs.github.com/spec/v1\noid sha256:6ed660119e8462d7591bdd78a15a4fa63aa23cad046256695e360a5de8c5c1fa\nsize 32\n")
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdin: %v", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	origStdin := os.Stdin
	origStdout := os.Stdout
	os.Stdin = inR
	os.Stdout = outW
	t.Cleanup(func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
	})
	t.Cleanup(func() {
		_ = inR.Close()
		_ = outR.Close()
		_ = inW.Close()
		_ = outW.Close()
	})

	if _, err := inW.Write(pointer); err != nil {
		t.Fatalf("write stdin pointer: %v", err)
	}
	_ = inW.Close()

	if err := runSmudge(Cmd, []string{filepath.Join("data", "source.txt")}); err != nil {
		t.Fatalf("runSmudge returned error: %v", err)
	}
	_ = outW.Close()

	got, err := io.ReadAll(outR)
	if err != nil {
		t.Fatalf("read smudge output: %v", err)
	}
	if !bytes.Equal(got, pointer) {
		t.Fatalf("smudge output mismatch:\n got: %q\nwant: %q", got, pointer)
	}
}
