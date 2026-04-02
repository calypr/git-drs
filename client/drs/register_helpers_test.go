package drs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/calypr/data-client/conf"
	datadrs "github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/logs"
	"github.com/calypr/git-drs/lfs"
)

func newTestGitDrsClientWithEndpoint(t *testing.T, endpoint string) *GitDrsClient {
	t.Helper()

	slogLogger := logs.NewSlogNoOpLogger()
	dataLogger, _ := logs.New("test")
	cred := &conf.Credential{
		Profile:     "test",
		APIEndpoint: endpoint,
		AccessToken: "token",
	}
	g3 := g3client.NewGen3InterfaceFromCredential(cred, dataLogger, g3client.WithClients(g3client.IndexdClient, g3client.FenceClient, g3client.SowerClient))

	return &GitDrsClient{
		Logger: slogLogger,
		G3:     g3,
		Config: &Config{
			ProjectId:    "proj",
			Organization: "org",
			BucketName:   "bucket",
		},
	}
}

func TestUploadKeyFromObject(t *testing.T) {
	checksums := []datadrs.Checksum{{Type: "sha256", Checksum: "abc123"}}

	tests := []struct {
		name   string
		obj    *datadrs.DRSObject
		bucket string
		want   string
	}{
		{
			name: "uses full s3 path when bucket matches",
			obj: &datadrs.DRSObject{
				Checksums: checksums,
				AccessMethods: []datadrs.AccessMethod{
					{AccessUrl: datadrs.AccessURL{Url: "s3://bucket/prefix/path/file.bin"}},
				},
			},
			bucket: "bucket",
			want:   "prefix/path/file.bin",
		},
		{
			name: "falls back to checksum when bucket mismatches",
			obj: &datadrs.DRSObject{
				Checksums: checksums,
				AccessMethods: []datadrs.AccessMethod{
					{AccessUrl: datadrs.AccessURL{Url: "s3://other/path/file.bin"}},
				},
			},
			bucket: "bucket",
			want:   "abc123",
		},
		{
			name: "falls back to checksum when no access url",
			obj: &datadrs.DRSObject{
				Checksums: checksums,
			},
			bucket: "bucket",
			want:   "abc123",
		},
		{
			name:   "nil object returns empty",
			obj:    nil,
			bucket: "bucket",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uploadKeyFromObject(tt.obj, tt.bucket)
			if got != tt.want {
				t.Fatalf("uploadKeyFromObject() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetSHA256ValidityMap(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/index/bulk/sha256/validity" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"oid1":true,"oid2":false}`))
		}))
		defer srv.Close()

		cl := newTestGitDrsClientWithEndpoint(t, srv.URL)
		got, err := cl.getSHA256ValidityMap(context.Background(), []string{"oid1", "oid2"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got["oid1"] || got["oid2"] {
			t.Fatalf("unexpected map: %#v", got)
		}
	})

	t.Run("non-200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()

		cl := newTestGitDrsClientWithEndpoint(t, srv.URL)
		_, err := cl.getSHA256ValidityMap(context.Background(), []string{"oid1"})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid-json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{invalid`))
		}))
		defer srv.Close()

		cl := newTestGitDrsClientWithEndpoint(t, srv.URL)
		_, err := cl.getSHA256ValidityMap(context.Background(), []string{"oid1"})
		if err == nil {
			t.Fatal("expected decode error")
		}
	})

	t.Run("missing-endpoint", func(t *testing.T) {
		cl := newTestGitDrsClientWithEndpoint(t, "")
		_, err := cl.getSHA256ValidityMap(context.Background(), []string{"oid1"})
		if err == nil {
			t.Fatal("expected missing endpoint error")
		}
	})
}

func TestBatchSyncForPushEmptyInput(t *testing.T) {
	cl := &GitDrsClient{}
	if err := cl.BatchSyncForPush(context.Background(), map[string]lfs.LfsFileInfo{}); err != nil {
		t.Fatalf("expected nil error for empty input, got %v", err)
	}
}

func TestResolveUploadSourcePath(t *testing.T) {
	t.Run("uses local lfs object when present", func(t *testing.T) {
		tmp := t.TempDir()
		old, _ := os.Getwd()
		if err := os.Chdir(tmp); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		defer os.Chdir(old)

		oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		objPath := filepath.Join(".git", "lfs", "objects", oid[:2], oid[2:4], oid)
		if err := os.MkdirAll(filepath.Dir(objPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(objPath, []byte("payload"), 0o644); err != nil {
			t.Fatalf("write lfs object: %v", err)
		}

		got, ok, err := resolveUploadSourcePath(oid, "data/file.bin", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatalf("expected upload source to be available")
		}
		if got != objPath {
			t.Fatalf("expected %s, got %s", objPath, got)
		}
	})

	t.Run("pointer without local payload becomes metadata-only", func(t *testing.T) {
		tmp := t.TempDir()
		old, _ := os.Getwd()
		if err := os.Chdir(tmp); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		defer os.Chdir(old)

		oid := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		got, ok, err := resolveUploadSourcePath(oid, "data/file.bin", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Fatalf("expected no upload source, got %s", got)
		}
	})

	t.Run("pointer with sentinel payload becomes metadata-only", func(t *testing.T) {
		tmp := t.TempDir()
		old, _ := os.Getwd()
		if err := os.Chdir(tmp); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		defer os.Chdir(old)

		oid := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
		objPath := filepath.Join(".git", "lfs", "objects", oid[:2], oid[2:4], oid)
		if err := os.MkdirAll(filepath.Dir(objPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		sentinel, err := lfs.BuildAddURLSentinel("etag-123", "s3://bucket/path/file.bin")
		if err != nil {
			t.Fatalf("build sentinel: %v", err)
		}
		if err := os.WriteFile(objPath, sentinel, 0o644); err != nil {
			t.Fatalf("write sentinel: %v", err)
		}

		got, ok, err := resolveUploadSourcePath(oid, "data/file.bin", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Fatalf("expected metadata-only (no upload source), got %s", got)
		}
	})

	t.Run("non-pointer uses worktree path", func(t *testing.T) {
		tmp := t.TempDir()
		old, _ := os.Getwd()
		if err := os.Chdir(tmp); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		defer os.Chdir(old)

		oid := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		worktreePath := filepath.Join("data", "file.bin")
		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(worktreePath, []byte("payload"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}

		got, ok, err := resolveUploadSourcePath(oid, worktreePath, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatalf("expected upload source to be available")
		}
		if got != worktreePath {
			t.Fatalf("expected %s, got %s", worktreePath, got)
		}
	})
}
