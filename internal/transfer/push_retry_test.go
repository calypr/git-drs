package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/drs"
	syclient "github.com/calypr/syfon/client"
	sycommon "github.com/calypr/syfon/client/common"
	sytransfer "github.com/calypr/syfon/client/transfer"
)

type pushRetryRoundTripFunc func(*http.Request) (*http.Response, error)

func (f pushRetryRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type failFirstResolveUploadBackend struct {
	uploadMetadataBackend
	resolveAttempts int
	uploads         int
}

func (b *failFirstResolveUploadBackend) ResolveUploadURL(_ context.Context, _, _ string, _ sycommon.FileMetadata, _ string) (string, error) {
	b.resolveAttempts++
	if b.resolveAttempts == 1 {
		return "", errors.New("simulated upload URL resolution failure")
	}
	return "https://upload.example/single", nil
}

func (b *failFirstResolveUploadBackend) Upload(ctx context.Context, target string, body io.Reader, size int64) error {
	b.uploads++
	return b.uploadMetadataBackend.Upload(ctx, target, body, size)
}

func TestFailedPayloadUploadIsRetriedAfterMetadataRegistration(t *testing.T) {
	const payload = "cached-payload"
	sum := sha256.Sum256([]byte(payload))
	oid := hex.EncodeToString(sum[:])
	root := t.TempDir()
	objectsRoot := filepath.Join(root, "lfs", "objects")
	cachePath, err := lfs.ObjectPath(objectsRoot, oid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}

	phase := 1
	registeredWithMarker := false
	apiClient, err := syclient.New("http://example.test", syclient.WithHTTPClient(&http.Client{
		Transport: pushRetryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			var status int
			var body string
			switch req.URL.Path {
			case "/index/bulk/sha256/missing":
				status = http.StatusOK
				if phase == 1 {
					body = `{"checked":1,"missing_sha256":["` + oid + `"]}`
				} else {
					body = `{"checked":1,"missing_sha256":[]}`
				}
			case "/index/bulk/hashes":
				status = http.StatusOK
				body = `{"results":{"` + oid + `":[]}}`
			case "/index/bulk":
				markerPath, pathErr := pendingPushUploadPath(&pushRuntime{
					API:            &remoteruntime.GitContext{Endpoint: "http://example.test"},
					Scope:          pushScope{Organization: "org", Project: "project"},
					DRSObjectsRoot: filepath.Join(root, "drs", "objects"),
				}, oid)
				if pathErr != nil {
					return nil, pathErr
				}
				if _, statErr := os.Stat(markerPath); statErr != nil {
					return nil, fmt.Errorf("registration started before pending marker: %w", statErr)
				}
				registeredWithMarker = true
				status = http.StatusCreated
				body = `{"records":[]}`
			default:
				return nil, fmt.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
			}
			return &http.Response{
				StatusCode: status,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}))
	if err != nil {
		t.Fatal(err)
	}
	backend := &failFirstResolveUploadBackend{}
	newSession := func() *batchSyncSession {
		return &batchSyncSession{
			ctx: context.Background(),
			rt: &pushRuntime{
				API:            &remoteruntime.GitContext{Client: apiClient, Endpoint: "http://example.test", Organization: "org", ProjectId: "project"},
				Backend:        backend,
				Logger:         slog.Default(),
				Scope:          pushScope{Organization: "org", Project: "project"},
				Tuning:         pushTuning{MultiPartThreshold: 1024, UploadConcurrency: 1},
				ObjectsRoot:    objectsRoot,
				DRSObjectsRoot: filepath.Join(root, "drs", "objects"),
			},
			oids:           []string{oid},
			filesByOID:     map[string]lfs.LfsFileInfo{oid: {Oid: oid, Name: "data/payload.bin", Size: int64(len(payload)), IsPointer: true}},
			drsObjByOID:    make(map[string]*drsapi.DrsObject),
			existingByHash: make(map[string][]drsapi.DrsObject),
			presentInScope: make(map[string]bool),
			uploadRequired: make(map[string]bool),
		}
	}

	first := newSession()
	if err := first.lookupMetadata(); err != nil {
		t.Fatalf("first lookupMetadata: %v", err)
	}
	if err := first.ensureMetadataRegistered(); err != nil {
		t.Fatalf("first ensureMetadataRegistered: %v", err)
	}
	if !registeredWithMarker {
		t.Fatal("metadata registration did not observe a pending upload marker")
	}
	firstCandidates, err := first.identifyUploadCandidates()
	if err != nil || len(firstCandidates) != 1 {
		t.Fatalf("first upload candidates = %d, err=%v; want one", len(firstCandidates), err)
	}
	if err := first.executeUploadPlan(firstCandidates); err == nil || !strings.Contains(err.Error(), "simulated upload URL resolution failure") {
		t.Fatalf("first upload error = %v, want upload URL resolution failure", err)
	}

	markerPath, err := pendingPushUploadPath(first.rt, oid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("failed upload removed pending marker: %v", err)
	}

	phase = 2
	second := newSession()
	if err := second.lookupMetadata(); err != nil {
		t.Fatalf("retry lookupMetadata: %v", err)
	}
	if err := second.ensureMetadataRegistered(); err != nil {
		t.Fatalf("retry ensureMetadataRegistered: %v", err)
	}
	secondCandidates, err := second.identifyUploadCandidates()
	if err != nil || len(secondCandidates) != 1 {
		t.Fatalf("retry upload candidates = %d, err=%v; want one", len(secondCandidates), err)
	}
	if err := second.executeUploadPlan(secondCandidates); err != nil {
		t.Fatalf("retry executeUploadPlan: %v", err)
	}
	if backend.uploads != 1 {
		t.Fatalf("successful retry uploads = %d, want 1", backend.uploads)
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("successful retry left pending marker, stat error = %v", err)
	}
}

type capturePushUploadBackend struct {
	uploadMetadataBackend
	resolveCalls int
	uploaded     []byte
}

func (b *capturePushUploadBackend) ResolveUploadURL(context.Context, string, string, sycommon.FileMetadata, string) (string, error) {
	b.resolveCalls++
	return "https://upload.example/single", nil
}

func (b *capturePushUploadBackend) Upload(_ context.Context, _ string, body io.Reader, _ int64) error {
	var err error
	b.uploaded, err = io.ReadAll(body)
	return err
}

type markerClearFailureBackend struct {
	capturePushUploadBackend
	markerPath string
}

func (b *markerClearFailureBackend) Upload(_ context.Context, _ string, body io.Reader, _ int64) error {
	var err error
	b.uploaded, err = io.ReadAll(body)
	if err != nil {
		return err
	}
	if err := os.Remove(b.markerPath); err != nil {
		return err
	}
	if err := os.Mkdir(b.markerPath, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(b.markerPath, "pending"), []byte("pending\n"), 0o600)
}

var _ sytransfer.MultipartBackend = (*capturePushUploadBackend)(nil)

func TestUploadFileForObjectRejectsCorruptCachedPayload(t *testing.T) {
	const expected = "good-payload"
	const corrupted = "evil-payload"
	sum := sha256.Sum256([]byte(expected))
	oid := hex.EncodeToString(sum[:])
	objectsRoot := filepath.Join(t.TempDir(), "lfs", "objects")
	cachePath, err := lfs.ObjectPath(objectsRoot, oid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte(corrupted), 0o600); err != nil {
		t.Fatal(err)
	}

	source, canUpload, err := resolveUploadSourcePathAt(objectsRoot, oid, "data.bin", true)
	if err != nil || !canUpload || source != cachePath {
		t.Fatalf("push source = %q, canUpload=%v, err=%v; want corrupt cache path selected for validation", source, canUpload, err)
	}
	backend := &capturePushUploadBackend{}
	obj := &drsapi.DrsObject{Id: "did:example:payload", Size: int64(len(expected)), Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: oid}}}
	err = uploadFileForCandidate(&pushRuntime{
		Backend: backend,
		Logger:  slog.Default(),
		Tuning:  pushTuning{MultiPartThreshold: 1 << 20},
	}, context.Background(), uploadCandidate{file: lfs.LfsFileInfo{Oid: oid}, obj: obj, src: source})
	if err == nil || !strings.Contains(err.Error(), "does not match SHA-256") {
		t.Fatalf("uploadFileForCandidate error = %v, want corrupt SHA-256 error", err)
	}
	if backend.resolveCalls != 0 || len(backend.uploaded) != 0 {
		t.Fatalf("corrupt cache reached upload backend: resolve calls=%d, uploaded=%q", backend.resolveCalls, backend.uploaded)
	}
}

func TestUploadCandidateDoesNotHashPlaceholderOIDAsPayload(t *testing.T) {
	const payload = "placeholder-payload"
	placeholderOID := strings.Repeat("a", 64)
	objectsRoot := filepath.Join(t.TempDir(), "lfs", "objects")
	cachePath, err := lfs.ObjectPath(objectsRoot, placeholderOID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}

	backend := &capturePushUploadBackend{}
	candidate := uploadCandidate{
		oid: placeholderOID,
		obj: &drsapi.DrsObject{
			Id:        "did:example:placeholder",
			Size:      int64(len(payload)),
			Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: placeholderOID}},
		},
		file: lfs.LfsFileInfo{Oid: placeholderOID, IsPointer: true, Placeholder: true},
		src:  cachePath,
	}
	if err := uploadFileForCandidate(&pushRuntime{
		Backend: backend,
		Logger:  slog.Default(),
		Tuning:  pushTuning{MultiPartThreshold: 1 << 20},
	}, context.Background(), candidate); err != nil {
		t.Fatalf("uploadFileForCandidate rejected a placeholder OID: %v", err)
	}
	if string(backend.uploaded) != payload {
		t.Fatalf("uploaded bytes = %q, want %q", backend.uploaded, payload)
	}
}

func TestUploadPlanDoesNotStartWhenPendingMarkerCannotBeWritten(t *testing.T) {
	const payload = "valid-payload"
	sum := sha256.Sum256([]byte(payload))
	oid := hex.EncodeToString(sum[:])
	root := t.TempDir()
	objectsRoot := filepath.Join(root, "lfs", "objects")
	source := writeUploadTestFile(t, payload)
	blockedRoot := filepath.Join(root, "blocked")
	if err := os.WriteFile(blockedRoot, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend := &capturePushUploadBackend{}
	rt := &pushRuntime{
		Backend:        backend,
		Logger:         slog.Default(),
		Tuning:         pushTuning{MultiPartThreshold: 1 << 20, UploadConcurrency: 1},
		ObjectsRoot:    objectsRoot,
		DRSObjectsRoot: filepath.Join(blockedRoot, "objects"),
	}
	candidate := uploadCandidate{
		oid:  oid,
		obj:  &drsapi.DrsObject{Id: "did:example:payload", Size: int64(len(payload)), Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: oid}}},
		file: lfs.LfsFileInfo{Oid: oid, Name: "data/payload.bin", IsPointer: true},
		src:  source,
	}

	err := (&batchSyncSession{ctx: context.Background(), rt: rt}).executeUploadPlan([]uploadCandidate{candidate})
	if err == nil || !strings.Contains(err.Error(), "record pending upload") {
		t.Fatalf("executeUploadPlan error = %v, want pending marker write error", err)
	}
	if backend.resolveCalls != 0 || len(backend.uploaded) != 0 {
		t.Fatalf("upload began without durable retry state: resolve calls=%d, uploaded=%q", backend.resolveCalls, backend.uploaded)
	}
}

func TestUploadPlanReturnsErrorWhenPendingMarkerCannotBeCleared(t *testing.T) {
	const payload = "valid-payload"
	sum := sha256.Sum256([]byte(payload))
	oid := hex.EncodeToString(sum[:])
	root := t.TempDir()
	objectsRoot := filepath.Join(root, "lfs", "objects")
	source := writeUploadTestFile(t, payload)
	rt := &pushRuntime{
		API:            &remoteruntime.GitContext{Endpoint: "http://example.test"},
		Logger:         slog.Default(),
		Tuning:         pushTuning{MultiPartThreshold: 1 << 20, UploadConcurrency: 1},
		ObjectsRoot:    objectsRoot,
		DRSObjectsRoot: filepath.Join(root, "drs", "objects"),
	}
	markerPath, err := pendingPushUploadPath(rt, oid)
	if err != nil {
		t.Fatal(err)
	}
	backend := &markerClearFailureBackend{markerPath: markerPath}
	rt.Backend = backend
	candidate := uploadCandidate{
		oid:  oid,
		obj:  &drsapi.DrsObject{Id: "did:example:payload", Size: int64(len(payload)), Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: oid}}},
		file: lfs.LfsFileInfo{Oid: oid, Name: "data/payload.bin", IsPointer: true},
		src:  source,
	}

	err = (&batchSyncSession{ctx: context.Background(), rt: rt}).executeUploadPlan([]uploadCandidate{candidate})
	if err == nil || !strings.Contains(err.Error(), "clear pending upload") {
		t.Fatalf("executeUploadPlan error = %v, want pending marker removal error", err)
	}
	if string(backend.uploaded) != payload {
		t.Fatalf("uploaded bytes = %q, want %q", backend.uploaded, payload)
	}
	if st, statErr := os.Stat(markerPath); statErr != nil || !st.IsDir() {
		t.Fatalf("pending marker was lost after clear error: stat=%v err=%v", st, statErr)
	}
}
