package pushsync

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syclient "github.com/calypr/syfon/client"
	sycommon "github.com/calypr/syfon/client/common"
	"github.com/calypr/syfon/client/transfer"
)

type recordingReporter struct {
	plan   UploadPlanSummary
	events []UploadProgressEvent
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func (r *recordingReporter) OnUploadPlan(plan UploadPlanSummary) {
	r.plan = plan
}

func (r *recordingReporter) OnUploadProgress(ev UploadProgressEvent) {
	r.events = append(r.events, ev)
}

type pushUploadBackendStub struct {
	mu sync.Mutex

	resolveFunc func(context.Context, string, string, sycommon.FileMetadata, string) (string, error)
	uploadFunc  func(context.Context, string, io.Reader, int64) error

	lastResolve struct {
		guid     string
		filename string
		metadata sycommon.FileMetadata
		bucket   string
	}
	lastUpload struct {
		url  string
		size int64
		body string
	}
}

func setTestPushScope(rt *pushRuntime) {
	rt.Scope = pushScope{
		Organization: "syfon",
		Project:      "e2e",
		Bucket:       "syfon-e2e-bucket",
	}
}

func (b *pushUploadBackendStub) Name() string { return "push-upload-backend-stub" }

func (b *pushUploadBackendStub) Logger() transfer.TransferLogger { return transfer.NoOpLogger{} }

func (b *pushUploadBackendStub) Upload(ctx context.Context, url string, body io.Reader, size int64) error {
	var bodyText string
	if body != nil {
		data, _ := io.ReadAll(body)
		bodyText = string(data)
	}

	b.mu.Lock()
	b.lastUpload.url = url
	b.lastUpload.size = size
	b.lastUpload.body = bodyText
	b.mu.Unlock()

	if b.uploadFunc != nil {
		return b.uploadFunc(ctx, url, strings.NewReader(bodyText), size)
	}
	return nil
}

func (b *pushUploadBackendStub) ResolveUploadURL(ctx context.Context, guid string, filename string, metadata sycommon.FileMetadata, bucket string) (string, error) {
	b.mu.Lock()
	b.lastResolve.guid = guid
	b.lastResolve.filename = filename
	b.lastResolve.metadata = metadata
	b.lastResolve.bucket = bucket
	b.mu.Unlock()

	if b.resolveFunc != nil {
		return b.resolveFunc(ctx, guid, filename, metadata, bucket)
	}
	return "https://upload.example/" + filename, nil
}

func (b *pushUploadBackendStub) MultipartInit(context.Context, string) (string, error) {
	return "upload-id", nil
}

func (b *pushUploadBackendStub) MultipartPart(context.Context, string, string, int, io.Reader) (string, error) {
	return "etag", nil
}

func (b *pushUploadBackendStub) MultipartComplete(context.Context, string, string, []transfer.MultipartPart) error {
	return nil
}

func TestExecuteUploadPlanReportsProgress(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "a.bin")
	if err := os.WriteFile(filePath, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	reporter := &recordingReporter{}
	rt := newPushRuntime(nil)
	setTestPushScope(rt)
	rt.Logger = drslog.NewNoOpLogger()
	rt.Tuning.MultiPartThreshold = 1024
	rt.Tuning.UploadConcurrency = 2

	backend := &pushUploadBackendStub{
		uploadFunc: func(ctx context.Context, _ string, _ io.Reader, _ int64) error {
			cb := sycommon.GetProgress(ctx)
			if cb == nil {
				t.Fatal("expected progress callback in upload context")
			}
			_ = cb(sycommon.ProgressEvent{Event: "progress", Oid: sycommon.GetOid(ctx), BytesSoFar: 5, BytesSinceLast: 5})
			_ = cb(sycommon.ProgressEvent{Event: "progress", Oid: sycommon.GetOid(ctx), BytesSoFar: 11, BytesSinceLast: 6})
			return nil
		},
	}
	oldBackend := uploadBackendForRuntime
	uploadBackendForRuntime = func(*pushRuntime) transfer.MultipartBackend { return backend }
	t.Cleanup(func() { uploadBackendForRuntime = oldBackend })

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

func TestEnsureMetadataRegisteredReusesExistingDownloadableRecordWithoutUpload(t *testing.T) {
	tmp := t.TempDir()
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	filePath := filepath.Join(tmp, "sample.bin")
	if err := os.WriteFile(filePath, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	reusableURL := "s3://existing-bucket/cas/" + oid
	var registerReq drsapi.RegisterObjectsJSONRequestBody
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/ga4gh/drs/v1/objects/existing-id/access/s3":
			fallthrough
		case r.Method == http.MethodGet && r.URL.Path == "/ga4gh/drs/v1/objects/scoped-id/access/s3":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"url":"https://signed.example/existing-id"}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Request:    r,
			}, nil
		case r.Method == http.MethodPost && r.URL.Path == "/ga4gh/drs/v1/objects/register":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read register request body: %v", err)
			}
			if err := json.Unmarshal(body, &registerReq); err != nil {
				t.Fatalf("unmarshal register request: %v", err)
			}
			if len(registerReq.Candidates) != 1 {
				t.Fatalf("expected one registration candidate, got %+v", registerReq)
			}
			candidate := registerReq.Candidates[0]
			respBody, err := json.Marshal(drsapi.N201ObjectsCreated{
				Objects: []drsapi.DrsObject{{
					Id:               "scoped-id",
					Name:             candidate.Name,
					Size:             candidate.Size,
					Checksums:        candidate.Checksums,
					ControlledAccess: candidate.ControlledAccess,
					AccessMethods:    candidate.AccessMethods,
				}},
			})
			if err != nil {
				t.Fatalf("marshal register response: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(strings.NewReader(string(respBody))),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Request:    r,
			}, nil
		default:
			return nil, io.EOF
		}
	})}

	raw, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("syclient.New: %v", err)
	}
	client := raw.(*syclient.Client)

	rt := newPushRuntime(&config.GitContext{
		Client:       client,
		Organization: "syfon",
		ProjectId:    "e2e",
		BucketName:   "syfon-e2e-bucket",
		Logger:       drslog.NewNoOpLogger(),
	})
	setTestPushScope(rt)
	rt.ProbeURL = func(context.Context, string) error { return nil }

	existing := drsapi.DrsObject{
		Id:   "existing-id",
		Name: ptrString("sample.bin"),
		Size: 11,
		Checksums: []drsapi.Checksum{{
			Type:     "sha256",
			Checksum: oid,
		}},
		ControlledAccess: &[]string{"/organization/other/project/other"},
		AccessMethods: &[]drsapi.AccessMethod{{
			Type: drsapi.AccessMethodTypeS3,
			AccessUrl: &struct {
				Headers *[]string `json:"headers,omitempty"`
				Url     string    `json:"url"`
			}{Url: reusableURL},
		}},
	}

	session := &batchSyncSession{
		ctx: context.Background(),
		rt:  rt,
		filesByOID: map[string]lfs.LfsFileInfo{
			oid: {Oid: oid, Name: filePath, Size: 11},
		},
		oids:           []string{oid},
		drsObjByOID:    map[string]*drsapi.DrsObject{},
		existingByHash: map[string][]drsapi.DrsObject{oid: {existing}},
		uploadRequired: map[string]bool{},
	}

	if err := session.ensureMetadataRegistered(); err != nil {
		t.Fatalf("ensureMetadataRegistered returned error: %v", err)
	}
	if session.uploadRequired[oid] {
		t.Fatalf("expected metadata-only scoped registration, but upload was marked required")
	}
	if len(registerReq.Candidates) != 1 {
		t.Fatalf("expected a scoped registration request, got %+v", registerReq)
	}
	if registerReq.Candidates[0].AccessMethods == nil || len(*registerReq.Candidates[0].AccessMethods) != 1 {
		t.Fatalf("expected preserved access methods in scoped registration: %+v", registerReq.Candidates[0])
	}
	if got := (*registerReq.Candidates[0].AccessMethods)[0].AccessUrl.Url; got != reusableURL {
		t.Fatalf("scoped registration access url = %q, want reused %q", got, reusableURL)
	}
	if session.drsObjByOID[oid] == nil {
		t.Fatalf("expected resolved scoped object after registration")
	}
	needsUpload, err := session.needsUpload(oid)
	if err != nil {
		t.Fatalf("needsUpload returned error: %v", err)
	}
	if needsUpload {
		t.Fatalf("expected reusable downloadable record to skip upload")
	}
}

func TestNeedsUploadHonorsForceUpload(t *testing.T) {
	oid := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	session := &batchSyncSession{
		ctx: context.Background(),
		rt: &pushRuntime{
			Tuning: pushTuning{ForceUpload: true},
		},
		drsObjByOID: map[string]*drsapi.DrsObject{
			oid: {},
		},
		existingByHash: map[string][]drsapi.DrsObject{
			oid: {{}},
		},
		uploadRequired: map[string]bool{},
	}

	needsUpload, err := session.needsUpload(oid)
	if err != nil {
		t.Fatalf("needsUpload returned error: %v", err)
	}
	if !needsUpload {
		t.Fatalf("expected force upload to require upload even with existing records")
	}
}

func TestExecuteUploadPlanHonorsUploadConcurrency(t *testing.T) {
	tmp := t.TempDir()
	rt := newPushRuntime(nil)
	setTestPushScope(rt)
	rt.Logger = drslog.NewNoOpLogger()
	rt.Tuning.MultiPartThreshold = 1024
	rt.Tuning.UploadConcurrency = 2

	var active int32
	var maxActive int32
	var mu sync.Mutex
	releaseChans := make([]chan struct{}, 0, 3)

	backend := &pushUploadBackendStub{
		uploadFunc: func(context.Context, string, io.Reader, int64) error {
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
		},
	}
	oldBackend := uploadBackendForRuntime
	uploadBackendForRuntime = func(*pushRuntime) transfer.MultipartBackend { return backend }
	t.Cleanup(func() { uploadBackendForRuntime = oldBackend })

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

func TestUploadFileForObjectUsesScopedKeyForMalformedRegisteredAccessURL(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "project-subpath.bin")
	if err := os.WriteFile(filePath, []byte("project subpath payload"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	rt := &pushRuntime{
		Logger: drslog.NewNoOpLogger(),
		Scope: pushScope{
			Organization: "syfon",
			Project:      "e2e",
			Bucket:       "syfon-e2e-bucket",
			StoragePref:  "program-root/project-subpath",
		},
		Tuning: pushTuning{MultiPartThreshold: 1024},
	}

	oid := "412f8568bfb0e62937ee40c6fcdeaa1cf55910c558c0152250340356c8829a47"
	obj := &drsapi.DrsObject{
		Id:   "f781273b-52eb-5ac2-a484-775235eef303",
		Name: ptrString("project-subpath.bin"),
		Size: 23,
		Checksums: []drsapi.Checksum{{
			Type:     "sha256",
			Checksum: oid,
		}},
		ControlledAccess: &[]string{"/organization/syfon/project/e2e"},
		AccessMethods: &[]drsapi.AccessMethod{{
			Type: drsapi.AccessMethodTypeS3,
			AccessUrl: &struct {
				Headers *[]string `json:"headers,omitempty"`
				Url     string    `json:"url"`
			}{Url: "s3://f781273b-52eb-5ac2-a484-775235eef303"},
		}},
	}

	backend := &pushUploadBackendStub{}
	oldBackend := uploadBackendForRuntime
	uploadBackendForRuntime = func(*pushRuntime) transfer.MultipartBackend { return backend }
	t.Cleanup(func() { uploadBackendForRuntime = oldBackend })

	if err := uploadFileForObject(rt, context.Background(), obj, filePath, false); err != nil {
		t.Fatalf("uploadFileForObject returned error: %v", err)
	}
	if backend.lastUpload.body != "project subpath payload" {
		t.Fatalf("uploaded body = %q, want project subpath payload", backend.lastUpload.body)
	}
	if backend.lastResolve.guid != obj.Id {
		t.Fatalf("upload guid = %q, want DID %q", backend.lastResolve.guid, obj.Id)
	}
	if backend.lastResolve.bucket != "syfon-e2e-bucket" {
		t.Fatalf("upload bucket hint = %q, want syfon-e2e-bucket", backend.lastResolve.bucket)
	}
	if got := backend.lastResolve.metadata.Authorizations["syfon"]; len(got) != 1 || got[0] != "e2e" {
		t.Fatalf("upload scope metadata = %+v, want syfon/e2e", backend.lastResolve.metadata.Authorizations)
	}
	wantKey := "program-root/project-subpath/" + oid
	if backend.lastResolve.filename != wantKey {
		t.Fatalf("upload object key = %q, want %q", backend.lastResolve.filename, wantKey)
	}
	if backend.lastUpload.url != "https://upload.example/"+wantKey {
		t.Fatalf("upload URL = %q, want signed URL for %q", backend.lastUpload.url, wantKey)
	}
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
