package pushsync

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	sycommon "github.com/calypr/syfon/client/common"
	"github.com/calypr/syfon/client/transfer"
)

type recordingReporter struct {
	plan   UploadPlanSummary
	events []UploadProgressEvent
}

func (r *recordingReporter) OnUploadPlan(plan UploadPlanSummary) {
	r.plan = plan
}

func (r *recordingReporter) OnUploadProgress(ev UploadProgressEvent) {
	r.events = append(r.events, ev)
}

func TestExecuteUploadPlanReportsProgress(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "a.bin")
	if err := os.WriteFile(filePath, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	reporter := &recordingReporter{}
	rt := newPushRuntime(nil)
	rt.Logger = drslog.NewNoOpLogger()
	rt.Tuning.MultiPartThreshold = 1024
	rt.Tuning.UploadConcurrency = 2

	oldUpload := uploadObjectFile
	uploadObjectFile = func(ctx context.Context, _ transfer.MultipartBackend, _ string, _ string, _ string, _ string, _ bool) error {
		cb := sycommon.GetProgress(ctx)
		if cb == nil {
			t.Fatal("expected progress callback in upload context")
		}
		_ = cb(sycommon.ProgressEvent{Event: "progress", Oid: sycommon.GetOid(ctx), BytesSoFar: 5, BytesSinceLast: 5})
		_ = cb(sycommon.ProgressEvent{Event: "progress", Oid: sycommon.GetOid(ctx), BytesSoFar: 11, BytesSinceLast: 6})
		return nil
	}
	t.Cleanup(func() { uploadObjectFile = oldUpload })

	session := &batchSyncSession{
		ctx:      context.Background(),
		rt:       rt,
		reporter: reporter,
	}
	candidates := []uploadCandidate{{
		oid:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		obj:  &drsapi.DrsObject{Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}},
		file: lfs.LfsFileInfo{Name: filePath},
		size: 11,
		src:  filePath,
	}}

	if err := session.executeUploadPlan(candidates); err != nil {
		t.Fatalf("executeUploadPlan returned error: %v", err)
	}
	if reporter.plan.TotalFiles != 1 || reporter.plan.TotalBytes != 11 {
		t.Fatalf("unexpected plan summary: %+v", reporter.plan)
	}
	if len(reporter.events) < 3 {
		t.Fatalf("expected progress + completed events, got %+v", reporter.events)
	}
	last := reporter.events[len(reporter.events)-1]
	if last.Phase != UploadProgressCompleted || last.BytesSoFar != 11 {
		t.Fatalf("unexpected final progress event: %+v", last)
	}
}

func TestExecuteUploadPlanHonorsUploadConcurrency(t *testing.T) {
	tmp := t.TempDir()
	rt := newPushRuntime(nil)
	rt.Logger = drslog.NewNoOpLogger()
	rt.Tuning.MultiPartThreshold = 1024
	rt.Tuning.UploadConcurrency = 2

	var active int32
	var maxActive int32
	var mu sync.Mutex
	releaseChans := make([]chan struct{}, 0, 3)

	oldUpload := uploadObjectFile
	uploadObjectFile = func(ctx context.Context, _ transfer.MultipartBackend, _ string, _ string, _ string, _ string, _ bool) error {
		cur := atomic.AddInt32(&active, 1)
		for {
			max := atomic.LoadInt32(&maxActive)
			if cur <= max || atomic.CompareAndSwapInt32(&maxActive, max, cur) {
				break
			}
		}

		release := make(chan struct{})
		mu.Lock()
		releaseChans = append(releaseChans, release)
		mu.Unlock()

		<-release
		atomic.AddInt32(&active, -1)
		return nil
	}
	t.Cleanup(func() { uploadObjectFile = oldUpload })

	makeCandidate := func(name string) uploadCandidate {
		path := filepath.Join(tmp, name)
		if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
			t.Fatalf("write temp file %s: %v", name, err)
		}
		return uploadCandidate{
			oid:  name + "-oid",
			obj:  &drsapi.DrsObject{Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: name + "-oid"}}},
			file: lfs.LfsFileInfo{Name: path},
			size: 5,
			src:  path,
		}
	}

	session := &batchSyncSession{
		ctx: context.Background(),
		rt:  rt,
	}
	candidates := []uploadCandidate{
		makeCandidate("a.bin"),
		makeCandidate("b.bin"),
		makeCandidate("c.bin"),
	}

	done := make(chan error, 1)
	go func() {
		done <- session.executeUploadPlan(candidates)
	}()

	for {
		mu.Lock()
		count := len(releaseChans)
		mu.Unlock()
		if count >= 2 {
			break
		}
	}

	if got := atomic.LoadInt32(&maxActive); got != 2 {
		t.Fatalf("max active uploads = %d, want 2", got)
	}

	mu.Lock()
	firstBatch := append([]chan struct{}(nil), releaseChans[:2]...)
	mu.Unlock()
	for _, ch := range firstBatch {
		close(ch)
	}

	for {
		mu.Lock()
		count := len(releaseChans)
		mu.Unlock()
		if count >= 3 {
			break
		}
	}

	mu.Lock()
	last := releaseChans[2]
	mu.Unlock()
	close(last)

	if err := <-done; err != nil {
		t.Fatalf("executeUploadPlan returned error: %v", err)
	}
	if got := atomic.LoadInt32(&maxActive); got != 2 {
		t.Fatalf("max active uploads after completion = %d, want 2", got)
	}
}

func TestScopedDRSObjectForPushRebuildsAccessMethodsFromCurrentScope(t *testing.T) {
	assertScopedDRSObjectForPushRebuildsAccessMethod(t, "s3://objects/existing-did")
	assertScopedDRSObjectForPushRebuildsAccessMethod(t, "s3://7b9de5b9-19b2-536f-abcc-fe2a146c4eb5")
}

func assertScopedDRSObjectForPushRebuildsAccessMethod(t *testing.T, existingURL string) {
	t.Helper()
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "program-root.bin")
	if err := os.WriteFile(filePath, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	rt := &pushRuntime{
		API: &config.GitContext{
			Organization:  "syfon",
			ProjectId:     "e2e",
			BucketName:    "syfon-e2e-bucket",
			StoragePrefix: "program-root",
		},
		Scope: pushScope{
			Organization: "syfon",
			Project:      "e2e",
			Bucket:       "syfon-e2e-bucket",
			StoragePref:  "program-root",
		},
	}

	existing := &drsapi.DrsObject{
		Id:   "existing-did",
		Name: ptrString("program-root.bin"),
		Checksums: []drsapi.Checksum{{
			Type:     "sha256",
			Checksum: "3d71f043937a09b77826109db4f2b47c46f19923ef823f6a777a15fde0b2c9c7",
		}},
		AccessMethods: &[]drsapi.AccessMethod{{
			Type: drsapi.AccessMethodTypeS3,
			AccessUrl: &struct {
				Headers *[]string `json:"headers,omitempty"`
				Url     string    `json:"url"`
			}{Url: existingURL},
		}},
	}

	obj, err := scopedDRSObjectForPush(rt, "3d71f043937a09b77826109db4f2b47c46f19923ef823f6a777a15fde0b2c9c7", filePath, 7, existing)
	if err != nil {
		t.Fatalf("scopedDRSObjectForPush returned error: %v", err)
	}
	if obj.AccessMethods == nil || len(*obj.AccessMethods) != 1 {
		t.Fatalf("expected rebuilt access method, got %+v", obj.AccessMethods)
	}
	got := (*obj.AccessMethods)[0].AccessUrl.Url
	want := "s3://syfon-e2e-bucket/program-root/3d71f043937a09b77826109db4f2b47c46f19923ef823f6a777a15fde0b2c9c7"
	if got != want {
		t.Fatalf("access url = %q, want %q", got, want)
	}
}

func TestScopedDRSObjectForPushPreservesExplicitAddURLAccessMethod(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "from-bucket.bin")
	if err := os.WriteFile(filePath, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	rt := &pushRuntime{
		API: &config.GitContext{
			Organization: "syfon",
			ProjectId:    "e2e",
			BucketName:   "syfon-e2e-bucket",
		},
		Scope: pushScope{
			Organization: "syfon",
			Project:      "e2e",
			Bucket:       "syfon-e2e-bucket",
		},
	}

	oid := "95d536cc8df0a8e265832c6bd0422d69593f564d5ff0518e77535c45bc10bfde"
	explicitURL := "s3://syfon-e2e-bucket/syfon/e2e/addurl/" + oid
	existing := &drsapi.DrsObject{
		Id:   "existing-did",
		Name: ptrString("from-bucket.bin"),
		Checksums: []drsapi.Checksum{{
			Type:     "sha256",
			Checksum: oid,
		}},
		AccessMethods: &[]drsapi.AccessMethod{{
			Type: drsapi.AccessMethodTypeS3,
			AccessUrl: &struct {
				Headers *[]string `json:"headers,omitempty"`
				Url     string    `json:"url"`
			}{Url: explicitURL},
		}},
	}

	obj, err := scopedDRSObjectForPush(rt, oid, filePath, 7, existing)
	if err != nil {
		t.Fatalf("scopedDRSObjectForPush returned error: %v", err)
	}
	if obj.AccessMethods == nil || len(*obj.AccessMethods) != 1 || (*obj.AccessMethods)[0].AccessUrl == nil {
		t.Fatalf("expected access method, got %+v", obj.AccessMethods)
	}
	if got := (*obj.AccessMethods)[0].AccessUrl.Url; got != explicitURL {
		t.Fatalf("access url = %q, want explicit add-url %q", got, explicitURL)
	}
}

func ptrString(s string) *string { return &s }
