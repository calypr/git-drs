package pull

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	localdrsobject "github.com/calypr/git-drs/internal/drsobject"
	internalfilter "github.com/calypr/git-drs/internal/filter"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/remoteruntime"
	internaltransfer "github.com/calypr/git-drs/internal/transfer"
	drsapi "github.com/calypr/syfon/apigen/drs"
)

type checkoutFailingReader struct{}

func (checkoutFailingReader) Read([]byte) (int, error) {
	return 0, errors.New("injected copy failure")
}

func TestReplaceCheckoutFilePreservesPreviousFileOnCopyFailure(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "data.bin")
	oldContent := []byte("original pointer")
	if err := os.WriteFile(dst, oldContent, 0o444); err != nil {
		t.Fatal(err)
	}
	src := io.NopCloser(io.MultiReader(bytes.NewReader([]byte("partial new payload")), checkoutFailingReader{}))
	_, _, err := replaceCheckoutFile(context.Background(), dst, src, pointerFile{Name: "data.bin", Oid: strings.Repeat("a", 64), Size: 50}, 0o444, nil)
	if err == nil || !strings.Contains(err.Error(), "injected copy failure") {
		t.Fatalf("replaceCheckoutFile error = %v, want copy failure", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, oldContent) {
		t.Fatalf("destination after failed copy = %q, want %q", got, oldContent)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o444 {
		t.Fatalf("destination mode after failed copy = %o, want 444", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temporary checkout file remains: %+v", entries)
	}
}

func TestReplaceCheckoutFileReportsCopyAndCleansCanceledStage(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(dst, []byte("pointer"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := []byte("downloaded content")
	sum := sha256.Sum256(payload)
	pointer := pointerFile{Oid: hex.EncodeToString(sum[:]), Size: int64(len(payload))}
	var progress []int64
	if _, _, err := replaceCheckoutFile(context.Background(), dst, io.NopCloser(bytes.NewReader(payload)), pointer, 0o644, func(n int64) { progress = append(progress, n) }); err != nil {
		t.Fatal(err)
	}
	if len(progress) == 0 || progress[len(progress)-1] != int64(len(payload)) {
		t.Fatalf("copy progress = %v, want %d bytes", progress, len(payload))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := replaceCheckoutFile(ctx, dst, io.NopCloser(bytes.NewReader(payload)), pointer, 0o644, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled checkout = %v, want context.Canceled", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("canceled checkout left a temporary file: %v, %v", entries, err)
	}
}

func resetPullFlagsForTest() {
	includePatterns = nil
	dryRun = false
}

func TestCollectPointerFilesFiltersAndSorts(t *testing.T) {
	resetPullFlagsForTest()

	inventory := map[string]lfs.LfsFileInfo{
		"data/b.bin": {Name: "data/b.bin", Oid: "bbbb", Size: 2},
		"data/a.bin": {Name: "data/a.bin", Oid: "aaaa", Size: 1},
		"misc/c.bin": {Name: "misc/c.bin", Oid: "cccc", Size: 3},
	}

	files := collectPointerFiles(inventory, []string{"data/**"}, gitrepo.DRSObjectsPath)
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Name != "data/a.bin" || files[1].Name != "data/b.bin" {
		t.Fatalf("unexpected file order: %+v", files)
	}
}

func TestPlaceholderPointerValidationUsesSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "object")
	if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := pointerFile{Name: "data/file.bin", Oid: strings.Repeat("a", 64), Size: int64(len("payload")), Placeholder: true}
	state, err := inspectCachedPointer(path, file)
	if err != nil || !state.complete {
		t.Fatalf("state = %+v, err = %v", state, err)
	}
	if err := verifyPointerAtPath(path, file); err != nil {
		t.Fatalf("verifyPointerAtPath returned error: %v", err)
	}
}

func TestVerifyPointerReportsHashedBytes(t *testing.T) {
	content := []byte("downloaded payload")
	sum := sha256.Sum256(content)
	file := pointerFile{Oid: hex.EncodeToString(sum[:]), SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content))}
	path := filepath.Join(t.TempDir(), "object")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	var progress []int64
	if err := verifyPointerAtPathWithProgress(path, file, func(n int64) { progress = append(progress, n) }); err != nil {
		t.Fatal(err)
	}
	if len(progress) < 2 || progress[0] != 0 || progress[len(progress)-1] != int64(len(content)) {
		t.Fatalf("hash progress = %v, want start at zero and finish at %d", progress, len(content))
	}
	file.SHA256 = strings.Repeat("0", 64)
	if err := verifyPointerAtPathWithProgress(path, file, nil); err == nil {
		t.Fatal("distinct pointer checksum was not checked")
	}
}

func TestCachedObjectChangedDetectsReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "object")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := inspectCachedObject(path, "", 3)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := cachedObjectChanged(path, state)
	if err != nil || changed {
		t.Fatalf("unchanged cache reported changed=%t, err=%v", changed, err)
	}
	replacement := filepath.Join(filepath.Dir(path), "replacement")
	if err := os.WriteFile(replacement, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	changed, err = cachedObjectChanged(path, state)
	if err != nil || !changed {
		t.Fatalf("replaced cache reported changed=%t, err=%v", changed, err)
	}
}

func TestCachedVerificationSkipsUnchangedFileAndRejectsModification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "object")
	content := []byte("verified content")
	digest := sha256.Sum256(content)
	file := pointerFile{Oid: hex.EncodeToString(digest[:]), Size: int64(len(content))}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := inspectCachedPointer(path, file)
	if err != nil || !state.complete {
		t.Fatalf("first verification = %+v, %v", state, err)
	}
	if _, ok := verifiedCacheInfo(path, file); !ok {
		t.Fatal("verified cache marker was not recorded")
	}
	if err := os.WriteFile(path, []byte("corrupt content!"), 0o644); err != nil {
		t.Fatal(err)
	}
	modified := state.info.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	if _, ok := verifiedCacheInfo(path, file); ok {
		t.Fatal("modified cache retained its verification")
	}
	state, err = inspectCachedPointer(path, file)
	if err != nil || state.complete {
		t.Fatalf("modified cache accepted: state=%+v err=%v", state, err)
	}
}

func TestIncompletePreallocatedDownloadIsNotVerifiedAsCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "object")
	content := []byte("verified content")
	digest := sha256.Sum256(content)
	file := pointerFile{Oid: hex.EncodeToString(digest[:]), Size: int64(len(content))}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if state, err := inspectCachedPointer(path, file); err != nil || !state.complete {
		t.Fatalf("initial cache verification = %+v, %v", state, err)
	}
	checkpoint := `{"identity":"sha256:` + file.Oid + `","size":` + strconv.FormatInt(file.Size, 10) + `,"complete":false}`
	if err := os.WriteFile(path+".syfon-download.json", []byte(checkpoint), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := inspectCachedPointer(path, file)
	if err != nil || !state.exists || state.complete {
		t.Fatalf("incomplete transfer was accepted as cached: %+v, %v", state, err)
	}
}

func TestReplaceCheckoutFileRejectsWrongHashBeforePromotion(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(dst, []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte("expected"))
	pointer := pointerFile{Oid: hex.EncodeToString(want[:]), Size: int64(len("bad data"))}
	if _, _, err := replaceCheckoutFile(context.Background(), dst, io.NopCloser(strings.NewReader("bad data")), pointer, 0o644, nil); err == nil {
		t.Fatal("incorrect checkout content was accepted")
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "previous" {
		t.Fatalf("destination changed after checksum failure: %q, %v", got, err)
	}
}

func TestSavePlaceholderChecksumsPersistsLocally(t *testing.T) {
	t.Chdir(t.TempDir())
	payload := []byte("downloaded without a published checksum")
	temporaryOID := strings.Repeat("a", 64)
	realSum := sha256.Sum256(payload)
	realOID := hex.EncodeToString(realSum[:])
	cachePath, err := lfs.ObjectPath(gitrepo.LFSObjectsPath, temporaryOID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := localdrsobject.WriteObject(gitrepo.DRSObjectsPath, &drsapi.DrsObject{Id: "object-1"}, temporaryOID); err != nil {
		t.Fatal(err)
	}
	files := []pointerFile{{Oid: temporaryOID, Size: int64(len(payload)), Placeholder: true}}
	if err := savePlaceholderChecksums(t.Context(), nil, files, nil, gitrepo.LFSObjectsPath, gitrepo.DRSObjectsPath); err != nil {
		t.Fatal(err)
	}
	obj, err := localdrsobject.ReadObject(gitrepo.DRSObjectsPath, temporaryOID)
	if err != nil || objectSHA256(obj) != realOID || files[0].SHA256 != realOID {
		t.Fatalf("saved object = %+v, files = %+v, err = %v", obj, files, err)
	}
	learned := collectPointerFiles(map[string]lfs.LfsFileInfo{
		"data/file.bin": {Oid: temporaryOID, Size: int64(len(payload)), Placeholder: true},
	}, nil, gitrepo.DRSObjectsPath)
	corrupt := append([]byte(nil), payload...)
	corrupt[0]++
	if err := os.WriteFile(cachePath, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := inspectCachedPointer(cachePath, learned[0])
	if err != nil || state.complete {
		t.Fatalf("same-sized corrupt placeholder cache accepted: state=%+v err=%v", state, err)
	}
}

func TestPullDryRunListsMatchingPaths(t *testing.T) {
	resetPullFlagsForTest()
	repo := t.TempDir()
	t.Chdir(repo)
	runGitCmdTest(t, repo, "init")

	oldLoadCfg := loadCfg
	oldResolveRemote := resolveRemote
	oldNewRemoteClient := newRemoteClient
	oldInventory := loadWorktreeInventory
	t.Cleanup(func() {
		loadCfg = oldLoadCfg
		resolveRemote = oldResolveRemote
		newRemoteClient = oldNewRemoteClient
		loadWorktreeInventory = oldInventory
	})

	loadCfg = func() (*config.Config, error) { return &config.Config{}, nil }
	resolveRemote = func(cfg *config.Config, name string) (config.Remote, error) { return config.Remote("origin"), nil }
	newRemoteClient = func(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*remoteruntime.GitContext, error) {
		t.Fatal("newRemoteClient should not be called during dry-run")
		return nil, nil
	}
	loadWorktreeInventory = func(_ *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
		return map[string]lfs.LfsFileInfo{
			"data/a.bin": {Name: "data/a.bin", Oid: "aaaa", Size: 1},
			"misc/b.bin": {Name: "misc/b.bin", Oid: "bbbb", Size: 2},
		}, nil
	}

	includePatterns = []string{"data/**"}
	dryRun = true

	var out bytes.Buffer
	Cmd.SetOut(&out)
	Cmd.SetErr(&out)
	Cmd.SetArgs([]string{"--dry-run"})
	t.Cleanup(func() {
		Cmd.SetOut(nil)
		Cmd.SetErr(nil)
		Cmd.SetArgs(nil)
		resetPullFlagsForTest()
	})

	if err := Cmd.RunE(Cmd, []string{}); err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if got := out.String(); got != "data/a.bin\n" {
		t.Fatalf("unexpected dry-run output: %q", got)
	}
}

func TestPullUsesTrackedInventoryForHydratedFiles(t *testing.T) {
	repo := t.TempDir()
	runGitCmdTest(t, repo, "init")
	runGitCmdTest(t, repo, "config", "user.email", "test@example.com")
	runGitCmdTest(t, repo, "config", "user.name", "Test User")
	runGitCmdTest(t, repo, "config", "filter.drs.clean", "cat")
	runGitCmdTest(t, repo, "config", "filter.drs.smudge", "cat")
	runGitCmdTest(t, repo, "config", "filter.drs.process", "cat")
	runGitCmdTest(t, repo, "config", "filter.drs.required", "false")

	attrPath := filepath.Join(repo, ".gitattributes")
	if err := os.WriteFile(attrPath, []byte("*.dat filter=drs diff=drs merge=drs -text\n"), 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}

	oid := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	pointerPath := filepath.Join(repo, "data", "hydrated.dat")
	writePointerFile(t, pointerPath, oid, "321")

	runGitCmdTest(t, repo, "add", ".")
	runGitCmdTest(t, repo, "commit", "-m", "commit tracked pointer")

	if err := os.WriteFile(pointerPath, []byte("localized payload"), 0o644); err != nil {
		t.Fatalf("hydrate tracked file: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	files, err := loadWorktreeInventory(drslog.NewNoOpLogger())
	if err != nil {
		t.Fatalf("loadWorktreeInventory error: %v", err)
	}

	info, ok := files["data/hydrated.dat"]
	if !ok {
		t.Fatal("expected hydrated tracked file in pull inventory")
	}
	if info.Oid != oid || info.Size != 321 {
		t.Fatalf("unexpected hydrated tracked info: %+v", info)
	}
}

func TestInspectCachedObject(t *testing.T) {
	tmpDir := t.TempDir()
	objectPath := filepath.Join(tmpDir, "obj")

	state, err := inspectCachedObject(objectPath, "abc", 10)
	if err != nil {
		t.Fatalf("missing file returned error: %v", err)
	}
	if state.exists || state.complete {
		t.Fatal("missing file should not be complete")
	}

	if err := os.WriteFile(objectPath, []byte("12345"), 0o644); err != nil {
		t.Fatalf("write partial object: %v", err)
	}
	state, err = inspectCachedObject(objectPath, "abc", 10)
	if err != nil {
		t.Fatalf("partial file returned error: %v", err)
	}
	if !state.exists {
		t.Fatal("partial file should exist")
	}
	if state.complete {
		t.Fatal("partial file should not be complete")
	}

	fullContent := []byte("1234567890")
	sum := sha256.Sum256(fullContent)
	oid := hex.EncodeToString(sum[:])
	if err := os.WriteFile(objectPath, fullContent, 0o644); err != nil {
		t.Fatalf("write full object: %v", err)
	}
	state, err = inspectCachedObject(objectPath, oid, 10)
	if err != nil {
		t.Fatalf("complete file returned error: %v", err)
	}
	if !state.complete {
		t.Fatal("full file should be complete")
	}

	if err := os.WriteFile(objectPath, []byte("abcdefghij"), 0o644); err != nil {
		t.Fatalf("write same-size corrupt object: %v", err)
	}
	state, err = inspectCachedObject(objectPath, oid, 10)
	if err != nil {
		t.Fatalf("same-size corrupt file returned error: %v", err)
	}
	if state.complete {
		t.Fatal("same-size corrupt file should not be complete")
	}
}

func TestInspectCachedObjectAcceptsZeroByteObject(t *testing.T) {
	objectPath := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(objectPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := inspectCachedObject(objectPath, "", 0)
	if err != nil || !state.complete {
		t.Fatalf("zero-byte object state = %+v, err = %v", state, err)
	}
}

func TestStrictAccessMethod(t *testing.T) {
	t.Setenv("GIT_DRS_ACCESS_METHOD", "")
	t.Setenv("GIT_DRS_TRANSFER_PROVIDER", "")
	if method, ok := strictAccessMethod("globus", ""); !ok || method != "globus" {
		t.Fatalf("command requirement = %q, %v", method, ok)
	}
	t.Setenv("GIT_DRS_ACCESS_METHOD", "require:globus")
	if method, ok := strictAccessMethod("", ""); !ok || method != "globus" {
		t.Fatalf("environment requirement = %q, %v", method, ok)
	}
	t.Setenv("GIT_DRS_ACCESS_METHOD", "")
	if method, ok := strictAccessMethod("", "prefer:globus"); ok || method != "" {
		t.Fatalf("preference incorrectly treated as strict: %q, %v", method, ok)
	}
	t.Setenv("GIT_DRS_ACCESS_METHOD", "prefer:https")
	t.Setenv("GIT_DRS_TRANSFER_PROVIDER", "require:globus")
	if method, ok := strictAccessMethod("", "require:globus"); ok || method != "" {
		t.Fatalf("lower-priority requirement overrode preference: %q, %v", method, ok)
	}
}

func TestVerifyObjectAtPath(t *testing.T) {
	tmpDir := t.TempDir()
	objectPath := filepath.Join(tmpDir, "obj")
	content := []byte("hello world")
	sum := sha256.Sum256(content)
	oid := hex.EncodeToString(sum[:])

	if err := os.WriteFile(objectPath, content, 0o644); err != nil {
		t.Fatalf("write object: %v", err)
	}
	if err := verifyObjectAtPath(objectPath, oid, int64(len(content))); err != nil {
		t.Fatalf("verifyObjectAtPath returned error for valid object: %v", err)
	}
	if err := verifyObjectAtPath(objectPath, oid, int64(len(content)+1)); err == nil {
		t.Fatal("expected size mismatch error")
	}
}

func TestCheckoutDownloadedFilesRejectsInvalidCachedObject(t *testing.T) {
	repo := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	if err := os.MkdirAll(gitrepo.LFSObjectsPath, 0o755); err != nil {
		t.Fatalf("mkdir LFS root: %v", err)
	}

	oid := strings.Repeat("a", 64)
	cachePath, err := lfs.ObjectPath(gitrepo.LFSObjectsPath, oid)
	if err != nil {
		t.Fatalf("ObjectPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("corrupt payload"), 0o644); err != nil {
		t.Fatalf("write corrupt cache: %v", err)
	}

	progress := internaltransfer.NewPullProgressRenderer(io.Discard)
	files := []pointerFile{{Name: "data/file.bin", Oid: oid, Size: 100}}
	progress.OnPlan(toPullFiles(files))

	_, err = checkoutDownloadedFiles(context.Background(), files, progress, false, gitrepo.LFSObjectsPath)
	if err == nil {
		t.Fatal("expected checkoutDownloadedFiles to reject invalid cached object")
	}
	if _, statErr := os.Stat(filepath.Join(repo, "data", "file.bin")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no checked-out file, stat err=%v", statErr)
	}
}

func TestCheckoutDownloadedFilesReusesVerifiedWorktreeFile(t *testing.T) {
	repo := t.TempDir()
	t.Chdir(repo)
	payload := []byte("already hydrated data")
	sum := sha256.Sum256(payload)
	file := pointerFile{Name: "data/file.bin", Oid: hex.EncodeToString(sum[:]), Size: int64(len(payload))}
	cacheRoot := gitrepo.LFSObjectsPath
	cachePath, err := lfs.ObjectPath(cacheRoot, file.Oid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(repo, file.Name)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	progress := internaltransfer.NewPullProgressRenderer(io.Discard)
	progress.OnPlan(toPullFiles([]pointerFile{file}))
	if _, err := checkoutDownloadedFiles(context.Background(), []pointerFile{file}, progress, false, cacheRoot); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(dst)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("verified worktree file was replaced: before=%v after=%v err=%v", before, after, err)
	}
	if err := os.WriteFile(dst, bytes.Repeat([]byte("x"), len(payload)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := checkoutDownloadedFiles(context.Background(), []pointerFile{file}, progress, false, cacheRoot); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("changed worktree file was not restored: %q, %v", got, err)
	}
}

func TestCheckoutDownloadedFilesRejectsEscapingAndSymlinkPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test skipped on Windows")
	}
	repo := t.TempDir()
	t.Chdir(repo)
	cacheRoot := gitrepo.LFSObjectsPath
	payload := []byte("safe payload")
	sum := sha256.Sum256(payload)
	oid := hex.EncodeToString(sum[:])
	cachePath, err := lfs.ObjectPath(cacheRoot, oid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("sentinel"), 0o644); err != nil {
		t.Fatal(err)
	}
	progress := internaltransfer.NewPullProgressRenderer(io.Discard)
	for _, name := range []string{"../outside.txt", "link/outside.txt"} {
		if name == "link/outside.txt" {
			if err := os.Symlink(filepath.Dir(outside), filepath.Join(repo, "link")); err != nil {
				t.Fatal(err)
			}
		}
		files := []pointerFile{{Name: name, Oid: oid, Size: int64(len(payload))}}
		progress.OnPlan(toPullFiles(files))
		if _, err := checkoutDownloadedFiles(context.Background(), files, progress, false, gitrepo.LFSObjectsPath); err == nil {
			t.Fatalf("checkoutDownloadedFiles accepted unsafe path %q", name)
		}
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "sentinel" {
		t.Fatalf("outside sentinel was changed to %q", got)
	}
}

func TestCheckedOutContentCleansBackToPointer(t *testing.T) {
	repo := t.TempDir()
	runGitCmdTest(t, repo, "init")
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	if err := os.MkdirAll(gitrepo.LFSObjectsPath, 0o755); err != nil {
		t.Fatalf("mkdir LFS root: %v", err)
	}

	payload := []byte("real hydrated payload")
	sum := sha256.Sum256(payload)
	oid := hex.EncodeToString(sum[:])
	cachePath, err := lfs.ObjectPath(gitrepo.LFSObjectsPath, oid)
	if err != nil {
		t.Fatalf("ObjectPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(cachePath, payload, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	progress := internaltransfer.NewPullProgressRenderer(io.Discard)
	files := []pointerFile{{Name: "data/file.bin", Oid: oid, Size: int64(len(payload))}}
	progress.OnPlan(toPullFiles(files))
	if _, err := checkoutDownloadedFiles(context.Background(), files, progress, false, gitrepo.LFSObjectsPath); err != nil {
		t.Fatalf("checkoutDownloadedFiles: %v", err)
	}

	var cleaned bytes.Buffer
	src, err := os.Open(filepath.Join(repo, "data", "file.bin"))
	if err != nil {
		t.Fatalf("open checked-out file: %v", err)
	}
	defer src.Close()

	if err := internalfilter.CleanContent(context.Background(), filepath.Join(repo, ".git", "lfs"), "data/file.bin", src, &cleaned, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("CleanContent: %v", err)
	}

	wantPointer := "version https://git-lfs.github.com/spec/v1\n" +
		"oid sha256:" + oid + "\n" +
		"size " + strconv.Itoa(len(payload)) + "\n"
	if got := cleaned.String(); got != wantPointer {
		t.Fatalf("unexpected cleaned pointer:\n got: %q\nwant: %q", got, wantPointer)
	}
}

func TestCheckoutDownloadedFilesFromReadOnlyRemoteSetsReadOnlyPermission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not supported on Windows")
	}

	repo := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	payload := []byte("read-only remote payload")
	sum := sha256.Sum256(payload)
	oid := hex.EncodeToString(sum[:])
	cachePath, err := lfs.ObjectPath(gitrepo.LFSObjectsPath, oid)
	if err != nil {
		t.Fatalf("ObjectPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(cachePath, payload, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	files := []pointerFile{{Name: "data/file.bin", Oid: oid, Size: int64(len(payload))}}
	progress := internaltransfer.NewPullProgressRenderer(io.Discard)
	progress.OnPlan(toPullFiles(files))
	if _, err := checkoutDownloadedFiles(context.Background(), files, progress, true, gitrepo.LFSObjectsPath); err != nil {
		t.Fatalf("checkoutDownloadedFiles: %v", err)
	}

	info, err := os.Stat(files[0].Name)
	if err != nil {
		t.Fatalf("stat checked-out file: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o444); got != want {
		t.Fatalf("checked-out permissions = %o, want %o", got, want)
	}
}

func TestRefreshGitIndexForHydratedFilesClearsDirtyStatus(t *testing.T) {
	repo := t.TempDir()
	gitDRS := buildGitDRSBinaryForTest(t)

	runGitCmdTest(t, repo, "init")
	runGitCmdTest(t, repo, "config", "user.email", "test@example.com")
	runGitCmdTest(t, repo, "config", "user.name", "Test User")
	runGitCmdTest(t, repo, "config", "filter.drs.clean", gitDRS+" clean -- %f")
	runGitCmdTest(t, repo, "config", "filter.drs.smudge", gitDRS+" smudge -- %f")
	runGitCmdTest(t, repo, "config", "filter.drs.process", gitDRS+" filter")
	runGitCmdTest(t, repo, "config", "filter.drs.required", "true")

	attrPath := filepath.Join(repo, ".gitattributes")
	if err := os.WriteFile(attrPath, []byte("*.bin filter=drs diff=drs merge=drs -text\n"), 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}

	worktreePath := filepath.Join(repo, "sample.bin")
	unrelatedPath := filepath.Join(repo, "unrelated.bin")
	payload := []byte("hello world payload")
	oid := "drs://drs.anv0:v2_example-without-sha256"
	pointer := "version https://calypr.github.io/spec/v1\n" +
		"oid " + oid + "\n" +
		"size " + strconv.Itoa(len(payload)) + "\n"
	if err := os.WriteFile(worktreePath, []byte(pointer), 0o644); err != nil {
		t.Fatalf("write DRS pointer: %v", err)
	}
	writePointerFile(t, unrelatedPath, strings.Repeat("a", 64), "37")

	runGitCmdTest(t, repo, "add", ".gitattributes", "sample.bin", "unrelated.bin")
	runGitCmdTest(t, repo, "commit", "-m", "commit pointer")

	repoLFSRoot := filepath.Join(repo, ".git", "lfs", "objects")
	if err := os.MkdirAll(repoLFSRoot, 0o755); err != nil {
		t.Fatalf("mkdir LFS objects: %v", err)
	}
	cachePath, err := lfs.ObjectPath(repoLFSRoot, oid)
	if err != nil {
		t.Fatalf("ObjectPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(cachePath, payload, 0o644); err != nil {
		t.Fatalf("write cache payload: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	files := []pointerFile{{Name: "sample.bin", Oid: oid, Size: int64(len(payload))}}
	progress := internaltransfer.NewPullProgressRenderer(io.Discard)
	progress.OnPlan(toPullFiles(files))
	receipts, err := checkoutDownloadedFiles(context.Background(), files, progress, false, repoLFSRoot)
	if err != nil {
		t.Fatalf("checkoutDownloadedFiles: %v", err)
	}
	if len(receipts) != len(files) {
		t.Fatalf("checkout receipts = %d, want %d", len(receipts), len(files))
	}
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)
	// A selected-path git add can run clean filters for other tracked files
	// whose stat data changed. Keep this unrelated indexed pointer as raw
	// pointer text to verify it follows ordinary clean behavior.
	if err := os.Chtimes(unrelatedPath, time.Now().Add(2*time.Second), time.Now().Add(2*time.Second)); err != nil {
		t.Fatalf("touch unrelated pointer: %v", err)
	}
	t.Setenv(internalfilter.IndexRefreshEnv, "stale")
	t.Setenv(internalfilter.IndexRefreshReceiptsEnv, filepath.Join(t.TempDir(), "stale-manifest.json"))
	if err := refreshGitIndexForHydratedFiles(files, receipts); err != nil {
		t.Fatalf("refreshGitIndexForHydratedFiles: %v", err)
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("read temporary receipt directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "git-drs-index-refresh-") {
			t.Fatalf("receipt manifest remained after git add: %s", entry.Name())
		}
	}

	after := runGitOutputTest(t, repo, "status", "--short", "--", "sample.bin")
	if strings.TrimSpace(after) != "" {
		t.Fatalf("expected hydrated file to be clean after refresh, got %q", after)
	}
	cached := runGitOutputTest(t, repo, "diff", "--cached", "--", "sample.bin")
	if strings.TrimSpace(cached) != "" {
		t.Fatalf("expected no staged semantic diff after refresh, got %q", cached)
	}
	if !alreadyHydratedInGit(context.Background(), files, repo) {
		t.Fatal("clean hydrated file should need no checkout or index refresh on repeat pull")
	}
	runGitCmdTest(t, repo, "update-index", "--skip-worktree", "sample.bin")
	if alreadyHydratedInGit(context.Background(), files, repo) {
		t.Fatal("skip-worktree index flag must not hide a file from pull")
	}
	runGitCmdTest(t, repo, "update-index", "--no-skip-worktree", "sample.bin")
	unrelatedStatus := runGitOutputTest(t, repo, "status", "--short", "--", "unrelated.bin")
	if strings.TrimSpace(unrelatedStatus) != "" {
		t.Fatalf("unrelated indexed pointer became dirty after refresh: %q", unrelatedStatus)
	}

	changed := append([]byte(nil), payload...)
	changed[0] ^= 1
	if err := os.WriteFile(filepath.Join(repo, "sample.bin"), changed, 0o644); err != nil {
		t.Fatalf("edit hydrated worktree file: %v", err)
	}
	if alreadyHydratedInGit(context.Background(), files, repo) {
		t.Fatal("same-size worktree edit incorrectly skipped checkout")
	}
	modified := runGitOutputTest(t, repo, "status", "--short", "--", "sample.bin")
	if !strings.Contains(modified, "sample.bin") || !strings.Contains(modified, " M") {
		t.Fatalf("same-size edit was not reported as modified: %q", modified)
	}
	cached = runGitOutputTest(t, repo, "diff", "--cached", "--", "sample.bin")
	if strings.TrimSpace(cached) != "" {
		t.Fatalf("same-size worktree edit unexpectedly changed the index: %q", cached)
	}
}

func TestAlreadyHydratedInGitRejectsUnhydratedPointerWithMatchingSize(t *testing.T) {
	repo := t.TempDir()
	runGitCmdTest(t, repo, "init")
	t.Chdir(repo)
	oid := strings.Repeat("a", 64)
	size := 0
	var pointer string
	for {
		pointer = "version https://git-lfs.github.com/spec/v1\noid sha256:" + oid + "\nsize " + strconv.Itoa(size) + "\n"
		if len(pointer) == size {
			break
		}
		size = len(pointer)
	}
	if err := os.WriteFile("pointer.bin", []byte(pointer), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCmdTest(t, repo, "add", "pointer.bin")
	if alreadyHydratedInGit(context.Background(), []pointerFile{{Name: "pointer.bin", Oid: oid, Size: int64(size)}}, repo) {
		t.Fatal("indexed pointer text must not be mistaken for hydrated content")
	}
}

func TestRefreshGitIndexPreservesLeadingSpacePath(t *testing.T) {
	repo := t.TempDir()
	runGitCmdTest(t, repo, "init")
	const path = " lead.bin"
	if err := os.WriteFile(filepath.Join(repo, path), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := refreshGitIndexForHydratedFiles([]pointerFile{{Name: path}}, nil); err != nil {
		t.Fatal(err)
	}
	staged := runGitOutputTest(t, repo, "diff", "--cached", "--name-only", "-z")
	if staged != path+"\x00" {
		t.Fatalf("staged path = %q, want exact leading-space path", staged)
	}
}

func buildGitDRSBinaryForTest(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	binPath := filepath.Join(t.TempDir(), "git-drs")
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Dir = moduleRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build git-drs failed: %v\n%s", err, string(out))
	}
	return binPath
}

func writePointerFile(t *testing.T, path, oid, size string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir pointer dir: %v", err)
	}
	content := "version https://git-lfs.github.com/spec/v1\n" +
		"oid sha256:" + oid + "\n" +
		"size " + size + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write pointer file: %v", err)
	}
}

func runGitCmdTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func runGitOutputTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		t.Fatalf("git %v failed: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}
