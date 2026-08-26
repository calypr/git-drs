package transfer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calypr/git-drs/internal/globusauth"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

type fakeGlobusClient struct {
	batches [][]globusauth.TransferItem
	waitErr error
}

func (f *fakeGlobusClient) SubmitTransfer(context.Context, string, string, string, string, string) (string, error) {
	return "task", nil
}
func (f *fakeGlobusClient) SubmitTransferItems(_ context.Context, _, _ string, items []globusauth.TransferItem, _ string) (string, error) {
	f.batches = append(f.batches, items)
	return fmt.Sprintf("task-%d", len(f.batches)), nil
}
func (f *fakeGlobusClient) WaitForTask(context.Context, string, time.Duration) error {
	return f.waitErr
}
func (*fakeGlobusClient) Close() error { return nil }

func TestParseGlobusURL(t *testing.T) {
	loc, err := parseGlobusURL("globus://01234567-89ab-cdef-0123-456789abcdef/data/sample.bam")
	if err != nil {
		t.Fatalf("parseGlobusURL returned error: %v", err)
	}
	if loc.Collection != "01234567-89ab-cdef-0123-456789abcdef" {
		t.Fatalf("collection = %q", loc.Collection)
	}
	if loc.Path != "/data/sample.bam" {
		t.Fatalf("path = %q", loc.Path)
	}
}

func TestParseGlobusURLRejectsMissingPath(t *testing.T) {
	if _, err := parseGlobusURL("globus://collection-only"); err == nil {
		t.Fatal("expected missing path to fail")
	}
}

func TestGlobusDestinationForCachePathRequiresCollection(t *testing.T) {
	t.Setenv(globusDestCollectionEnv, "")
	if _, err := globusDestinationForCachePath(nil, "source", filepath.Join(".git", "drs", "objects", "aa")); err == nil {
		t.Fatal("expected missing destination collection to fail")
	}
}

func TestGlobusDestinationForCachePathUsesLFSCachePath(t *testing.T) {
	t.Setenv(globusDestCollectionEnv, "dest-collection")
	loc, err := globusDestinationForCachePath(nil, "source", filepath.Join(".git", "lfs", "objects", "aa", "bb"))
	if err != nil {
		t.Fatalf("globusDestinationForCachePath returned error: %v", err)
	}
	if loc.Collection != "dest-collection" {
		t.Fatalf("collection = %q", loc.Collection)
	}
	wantSuffix := "/.git/lfs/objects/aa/bb"
	if loc.Path != wantSuffix {
		t.Fatalf("path = %q, want %q", loc.Path, wantSuffix)
	}
}

func TestGlobusDestinationForCachePathRejectsNonCachePath(t *testing.T) {
	t.Setenv(globusDestCollectionEnv, "dest-collection")
	for _, destination := range []string{"file.bin", filepath.Join(t.TempDir(), ".git", "lfs", "objects", "aa")} {
		if _, err := globusDestinationForCachePath(nil, "source", destination); err == nil {
			t.Fatalf("expected destination %q to be rejected", destination)
		}
	}
}

func TestGlobusDestinationUsesSourceRouteAndRepositoryPath(t *testing.T) {
	t.Setenv(globusDestCollectionEnv, "")
	ctx := &remoteruntime.GitContext{
		GlobusCollections:      map[string]string{"source-a": "destination-west"},
		GlobusDestinationPaths: map[string]string{"destination-west": "/projects/research/repository"},
	}
	loc, err := globusDestinationForCachePath(ctx, "SOURCE-A", filepath.Join(".git", "lfs", "objects", "aa", "bb"))
	if err != nil {
		t.Fatal(err)
	}
	if loc.Collection != "destination-west" || loc.Path != "/projects/research/repository/.git/lfs/objects/aa/bb" {
		t.Fatalf("destination = %+v", loc)
	}
}

func TestGlobusDestinationEnforcesSharedSourceConstraint(t *testing.T) {
	t.Setenv(globusDestCollectionEnv, "")
	ctx := &remoteruntime.GitContext{
		AllowedGlobusSources:     []string{"source-a"},
		GlobusDefaultDestination: "destination",
	}
	if _, _, err := resolveGlobusDestination(ctx, "source-b"); err == nil || !strings.Contains(err.Error(), "source_collection_disallowed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if destination, _, err := resolveGlobusDestination(ctx, "SOURCE-A"); err != nil || destination != "destination" {
		t.Fatalf("allowed source: destination=%q error=%v", destination, err)
	}
}

func TestNormalizeGlobusRepositoryPathRejectsTraversal(t *testing.T) {
	for _, value := range []string{"relative/path", "/repo/../other", `C:\\repo`} {
		if _, err := normalizeGlobusRepositoryPath(value); err == nil {
			t.Fatalf("expected %q to fail", value)
		}
	}
}

func TestIsGlobusURL(t *testing.T) {
	if !isGlobusURL("globus://collection/path") {
		t.Fatal("expected globus URL")
	}
	if isGlobusURL("https://example.test/path") || isGlobusURL(os.DevNull) {
		t.Fatal("expected non-globus URL to be rejected")
	}
}

func TestDownloadGlobusBatchGroupsCompatibleFiles(t *testing.T) {
	repo := t.TempDir()
	oldWD, _ := os.Getwd()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	t.Setenv(globusDestCollectionEnv, "destination")
	fake := &fakeGlobusClient{}
	old := newGlobusClient
	newGlobusClient = func(context.Context) (globusClient, error) { return fake, nil }
	t.Cleanup(func() { newGlobusClient = old })
	var downloads []GlobusDownload
	for i, source := range []string{"source-a", "source-a", "source-b"} {
		payload := []byte(fmt.Sprintf("file-%d", i))
		sum := fmt.Sprintf("%x", sha256.Sum256(payload))
		cachePath := filepath.Join(".git", "lfs", "objects", sum[:2], sum[2:4], sum)
		if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cachePath, payload, 0o644); err != nil {
			t.Fatal(err)
		}
		obj := &drsapi.DrsObject{Size: int64(len(payload)), Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: sum}}}
		downloads = append(downloads, GlobusDownload{OID: sum, CachePath: cachePath, Object: obj, AccessURL: "globus://" + source + "/file"})
	}
	downloads = append(downloads, downloads[0])
	if err := DownloadGlobusBatch(t.Context(), nil, downloads); err != nil {
		t.Fatal(err)
	}
	if len(fake.batches) != 2 {
		t.Fatalf("submitted %d batches, want 2", len(fake.batches))
	}
	items := 0
	for _, batch := range fake.batches {
		items += len(batch)
	}
	if items != 3 {
		t.Fatalf("submitted %d transfer items, want 3 unique cache destinations", items)
	}
}

func TestDownloadGlobusBatchRemovesFailedGroup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		waitErr error
		corrupt bool
	}{
		{name: "task failure", waitErr: fmt.Errorf("transfer failed")},
		{name: "verification failure", corrupt: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			oldWD, _ := os.Getwd()
			if err := os.Chdir(repo); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chdir(oldWD) })
			t.Setenv(globusDestCollectionEnv, "destination")
			fake := &fakeGlobusClient{waitErr: tc.waitErr}
			old := newGlobusClient
			newGlobusClient = func(context.Context) (globusClient, error) { return fake, nil }
			t.Cleanup(func() { newGlobusClient = old })

			var downloads []GlobusDownload
			for i := range 2 {
				payload := []byte(fmt.Sprintf("file-%d", i))
				sum := fmt.Sprintf("%x", sha256.Sum256(payload))
				cachePath := filepath.Join(".git", "lfs", "objects", sum[:2], sum[2:4], sum)
				if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
					t.Fatal(err)
				}
				contents := payload
				if tc.corrupt && i == 1 {
					contents = []byte("broken")
				}
				if err := os.WriteFile(cachePath, contents, 0o644); err != nil {
					t.Fatal(err)
				}
				obj := &drsapi.DrsObject{Size: int64(len(payload)), Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: sum}}}
				downloads = append(downloads, GlobusDownload{OID: sum, CachePath: cachePath, Object: obj, AccessURL: "globus://source/file"})
			}

			if err := DownloadGlobusBatch(t.Context(), nil, downloads); err == nil {
				t.Fatal("expected batch failure")
			}
			for _, download := range downloads {
				if _, err := os.Stat(download.CachePath); !os.IsNotExist(err) {
					t.Fatalf("incomplete batch destination remains at %s: %v", download.CachePath, err)
				}
			}
		})
	}
}
