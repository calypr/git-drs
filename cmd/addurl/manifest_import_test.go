package addurl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	internalapi "github.com/calypr/syfon/apigen/internalapi"
)

func TestReadImportManifest(t *testing.T) {
	file := filepath.Join(t.TempDir(), "input.tsv")
	contents := "s3_path\trepo_path\tsha256\tsize_bytes\n" + "s3://source/a\tdir/a\t" + strings.Repeat("a", 64) + "\t45\n"
	if err := os.WriteFile(file, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	rows, err := readImportManifest(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].destination != "dir/a" || rows[0].size != 45 {
		t.Fatalf("unexpected rows: %+v", rows)
	}
	if err = os.WriteFile(file, []byte(strings.Replace(contents, "dir/a", "../escape", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readImportManifest(file); err == nil {
		t.Fatal("expected path traversal rejection")
	}
}

func TestProbeManifestUsesScopeAndResolvedLocation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/data/inspect/bulk" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body internalapi.InternalInspectObjectsBulkRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		got := body.Items[0]
		if got.Organization != "ExampleOrg" || got.Project != "ExampleProject" || got.Key != "study/a" || got.ExpectedSizeBytes == nil || *got.ExpectedSizeBytes != 45 {
			t.Errorf("request: %+v", got)
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"0","status":"present","exists":true,"size_match":true,"object_url":"s3://mapped-bucket/source-bucket/study/a"}]}`))
	}))
	defer server.Close()
	client, err := internalapi.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	entries := []manifestEntry{{source: "s3://source-bucket/study/a", size: 45}}
	if err = probeManifest(context.Background(), client, entries, "ExampleOrg", "ExampleProject", "s3://source-bucket/"); err != nil {
		t.Fatal(err)
	}
	if entries[0].resolved != "s3://mapped-bucket/source-bucket/study/a" {
		t.Fatalf("resolved %q", entries[0].resolved)
	}
}

func TestManifestImportCreatesRecordsAndCommit(t *testing.T) {
	var created internalapi.BulkCreateRequest
	var writes int
	var lfsRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/data/inspect/bulk":
			var req internalapi.InternalInspectObjectsBulkRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": req.Items[0].Id, "status": "present", "exists": true, "size_match": true, "object_url": "s3://bucket/prefix/a"}}})
		case "/index/bulk/documents":
			_, _ = w.Write([]byte(`[]`))
		case "/index/bulk/hashes":
			var req internalapi.BulkHashesRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			results := make(map[string][]internalapi.InternalRecord)
			for _, hash := range req.Hashes {
				if len(created.Records) > 0 {
					results[hash] = created.Records
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
		case "/index/bulk":
			writes++
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"records":[]}`))
		case "/info/lfs/objects/batch":
			lfsRequests++
			var req struct {
				Objects []struct {
					Oid  string `json:"oid"`
					Size int64  `json:"size"`
				} `json:"objects"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			w.Header().Set("Content-Type", "application/vnd.git-lfs+json")
			_ = json.NewEncoder(w).Encode(map[string]any{"transfer": "basic", "objects": req.Objects})
		case "/info/lfs/locks/verify":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	repo := setupGitRepo(t)
	gitCmd(t, repo, "checkout", "-b", "import-test")
	gitCmd(t, repo, "commit", "--allow-empty", "-m", "initial")
	bare := filepath.Join(t.TempDir(), "remote.git")
	gitCmd(t, repo, "init", "--bare", bare)
	gitCmd(t, repo, "remote", "add", "origin", bare)
	gitCmd(t, repo, "config", "remote.origin.lfsurl", server.URL+"/info/lfs")
	if _, err := exec.LookPath("git-lfs"); err == nil {
		hook := filepath.Join(repo, ".git", "hooks", "pre-push")
		if err := os.WriteFile(hook, []byte("#!/bin/sh\nexec git lfs pre-push \"$@\"\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	gitCmd(t, repo, "push", "-u", "origin", "import-test")
	for _, args := range [][]string{{"config", "drs.default-remote", "test"}, {"config", "drs.remote.test.type", "local"}, {"config", "drs.remote.test.endpoint", server.URL}, {"config", "drs.remote.test.organization", "Org"}, {"config", "drs.remote.test.project", "Project"}, {"config", "drs.remote.test.bucket", "bucket"}} {
		gitCmd(t, repo, args...)
	}
	gitCmd(t, repo, "config", "filter.drs.process", "false")
	manifest := filepath.Join(t.TempDir(), "input.tsv")
	sha := strings.Repeat("a", 64)
	if err := os.WriteFile(manifest, []byte("s3_path\trepo_path\tsha256\tsize_bytes\nsource/a\tdir/a\t"+sha+"\t45\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "dir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "dir/a"), []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}
	before, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	indexOnly := NewCommand()
	indexOnly.SetArgs([]string{manifest, "--repo", repo})
	if err := indexOnly.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(created.Records) != 1 {
		t.Fatalf("Syfon-only import did not register record: %+v", created)
	}
	data, err := os.ReadFile(filepath.Join(repo, "dir/a"))
	if err != nil || string(data) != "existing" {
		t.Fatalf("Syfon-only import changed file: %q: %v", data, err)
	}
	after, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil || string(before) != string(after) {
		t.Fatalf("Syfon-only import changed commit: %v", err)
	}
	if err := os.Remove(filepath.Join(repo, "dir/a")); err != nil {
		t.Fatal(err)
	}
	command := NewCommand()
	command.SetArgs([]string{manifest, "--repo", repo, "--git-only", "--branch", "import-test"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatalf("Git-only import changed Syfon records: %d writes", writes)
	}
	if lfsRequests != 0 {
		t.Fatalf("Git-only import invoked LFS upload API %d times", lfsRequests)
	}
	if len(created.Records) != 1 || created.Records[0].Hashes == nil || (*created.Records[0].Hashes)["sha256"] != sha {
		t.Fatalf("records: %+v", created)
	}
	methods := created.Records[0].AccessMethods
	if methods == nil || (*methods)[0].AccessUrl.Url != "s3://bucket/prefix/a" {
		t.Fatalf("access methods: %+v", methods)
	}
	show := exec.Command("git", "-C", repo, "show", "HEAD:dir/a")
	out, err := show.CombinedOutput()
	if err != nil {
		t.Fatalf("git show: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "version https://git-lfs.github.com/spec/v1") || !strings.Contains(string(out), "oid sha256:"+sha) || !strings.Contains(string(out), "size 45") {
		t.Fatalf("committed pointer: %s", out)
	}
	if _, err := exec.LookPath("git-lfs"); err == nil {
		cmd := exec.Command("git", "-C", repo, "lfs", "ls-files", "-n")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git lfs ls-files: %v: %s", err, out)
		}
		if !strings.Contains(string(out), "dir/a") {
			t.Fatalf("Git LFS does not recognize the imported pointer: %s", out)
		}
	}
	verify := exec.Command("git", "--git-dir", bare, "show", "refs/heads/import-test:dir/a")
	remotePointer, err := verify.CombinedOutput()
	if err != nil || !strings.Contains(string(remotePointer), "oid sha256:"+sha) {
		t.Fatalf("remote pointer: %s: %v", remotePointer, err)
	}
}

func TestProbeManifestRejectsSizeMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"0","status":"present","exists":true,"size_match":false,"object_url":"s3://bucket/a"}]}`))
	}))
	defer server.Close()
	client, err := internalapi.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	entries := []manifestEntry{{source: "s3://bucket/a", size: 45}}
	if err := probeManifest(context.Background(), client, entries, "org", "project", ""); err == nil {
		t.Fatal("expected size mismatch to fail")
	}
}
