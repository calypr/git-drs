package transfer

import (
	"context"
	"io"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"

	drsapi "github.com/calypr/syfon/apigen/drs"
	sycommon "github.com/calypr/syfon/client/common"
	sytransfer "github.com/calypr/syfon/client/transfer"
)

type uploadMetadataBackend struct {
	resolveMetadata sycommon.FileMetadata
	initMetadata    sycommon.FileMetadata
	uploadTarget    string
	initCalled      bool
}

func (b *uploadMetadataBackend) Logger() sytransfer.TransferLogger {
	return sytransfer.NoOpLogger{}
}

func (b *uploadMetadataBackend) Upload(_ context.Context, target string, body io.Reader, _ int64) error {
	b.uploadTarget = target
	_, err := io.Copy(io.Discard, body)
	return err
}

func (b *uploadMetadataBackend) MultipartInit(context.Context, string) (string, error) {
	return "fallback-upload-id", nil
}

func (b *uploadMetadataBackend) MultipartPart(_ context.Context, _ string, _ string, _ int, body io.Reader) (string, error) {
	_, err := io.Copy(io.Discard, body)
	return "etag", err
}

func (b *uploadMetadataBackend) MultipartComplete(context.Context, string, string, []sytransfer.MultipartPart) error {
	return nil
}

func (b *uploadMetadataBackend) ResolveUploadURL(_ context.Context, _ string, _ string, metadata sycommon.FileMetadata, _ string) (string, error) {
	b.resolveMetadata = metadata
	return "https://upload.example/single", nil
}

func (b *uploadMetadataBackend) InitMultipartUploadWithMetadata(_ context.Context, _ string, _ string, _ string, metadata sycommon.FileMetadata) (string, string, error) {
	b.initCalled = true
	b.initMetadata = metadata
	return "multipart-upload-id", "object-key", nil
}

func uploadTestObject(size int64) *drsapi.DrsObject {
	return &drsapi.DrsObject{
		Id:   "did:example:upload",
		Size: size,
		Checksums: []drsapi.Checksum{{
			Type:     "sha256",
			Checksum: strings.Repeat("a", 64),
		}},
	}
}

func writeUploadTestFile(t *testing.T, contents string) string {
	t.Helper()
	path := t.TempDir() + "/payload"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write upload test file: %v", err)
	}
	return path
}

func withUploadBackend(t *testing.T, backend sytransfer.MultipartBackend) {
	t.Helper()
	previous := uploadBackendForRuntime
	uploadBackendForRuntime = func(*pushRuntime) sytransfer.MultipartBackend {
		return backend
	}
	t.Cleanup(func() {
		uploadBackendForRuntime = previous
	})
}

func TestUploadFileForObjectPassesScopeToSingleResolver(t *testing.T) {
	backend := &uploadMetadataBackend{}
	withUploadBackend(t, backend)
	path := writeUploadTestFile(t, "payload")
	rt := &pushRuntime{
		Logger: slog.Default(),
		Scope: pushScope{
			Organization: " org ",
			Project:      " project ",
		},
		Tuning: pushTuning{MultiPartThreshold: 1024},
	}

	if err := uploadFileForObject(rt, context.Background(), uploadTestObject(7), path); err != nil {
		t.Fatalf("uploadFileForObject: %v", err)
	}

	want := sycommon.FileMetadata{Authorizations: map[string][]string{"org": {"project"}}}
	if !reflect.DeepEqual(backend.resolveMetadata, want) {
		t.Fatalf("single upload metadata = %#v, want %#v", backend.resolveMetadata, want)
	}
	if backend.uploadTarget != "https://upload.example/single" {
		t.Fatalf("single upload target = %q", backend.uploadTarget)
	}
}

func TestUploadFileForObjectPassesScopeToMultipartInitializer(t *testing.T) {
	backend := &uploadMetadataBackend{}
	withUploadBackend(t, backend)
	path := writeUploadTestFile(t, "payload")
	rt := &pushRuntime{
		Logger: slog.Default(),
		Scope: pushScope{
			Organization: "org",
			Project:      "project",
		},
		Tuning: pushTuning{MultiPartThreshold: 1},
	}

	if err := uploadFileForObject(rt, context.Background(), uploadTestObject(7), path); err != nil {
		t.Fatalf("uploadFileForObject: %v", err)
	}

	want := sycommon.FileMetadata{Authorizations: map[string][]string{"org": {"project"}}}
	if !backend.initCalled {
		t.Fatal("multipart initializer was not called")
	}
	if !reflect.DeepEqual(backend.initMetadata, want) {
		t.Fatalf("multipart metadata = %#v, want %#v", backend.initMetadata, want)
	}
}

func TestScopedUploadMetadataRequiresCompleteScope(t *testing.T) {
	tests := []struct {
		name  string
		scope pushScope
		want  sycommon.FileMetadata
	}{
		{name: "empty"},
		{name: "organization only", scope: pushScope{Organization: "org"}},
		{name: "project only", scope: pushScope{Project: "project"}},
		{name: "whitespace organization", scope: pushScope{Organization: "  ", Project: "project"}},
		{name: "whitespace project", scope: pushScope{Organization: "org", Project: "  "}},
		{
			name:  "full scope",
			scope: pushScope{Organization: " org ", Project: " project "},
			want:  sycommon.FileMetadata{Authorizations: map[string][]string{"org": {"project"}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := scopedUploadMetadata(&pushRuntime{Scope: test.scope}); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("scopedUploadMetadata = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestUploadFileForObjectRequiresBackend(t *testing.T) {
	withUploadBackend(t, nil)
	path := writeUploadTestFile(t, "payload")
	rt := &pushRuntime{Logger: slog.Default()}

	err := uploadFileForObject(rt, context.Background(), uploadTestObject(7), path)
	if err == nil || !strings.Contains(err.Error(), "upload backend is required") {
		t.Fatalf("uploadFileForObject error = %v, want missing backend error", err)
	}
}
