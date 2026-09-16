package filter

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	internalfilter "github.com/calypr/git-drs/internal/filter"
	"github.com/calypr/git-drs/internal/remoteruntime"
	"github.com/calypr/git-drs/internal/resolver"
)

type recordingResolver struct {
	accessURL string
	drsURI    string
}

func (r *recordingResolver) GetObject(_ context.Context, drsURI string) (*resolver.ResolvedObject, error) {
	r.drsURI = drsURI
	return &resolver.ResolvedObject{
		Size:          13,
		AccessMethods: []resolver.AccessMethod{{AccessID: "access-1"}},
	}, nil
}

func (r *recordingResolver) GetAccess(context.Context, string, string) (*resolver.ResolvedAccess, error) {
	return &resolver.ResolvedAccess{URL: r.accessURL}, nil
}

func TestSmudgeHandlerUsesTerraResolver(t *testing.T) {
	t.Setenv("GIT_DRS_SKIP_SMUDGE", "false")
	payload := []byte("terra payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "lfs", "objects"), 0o755); err != nil {
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

	r := &recordingResolver{accessURL: server.URL}
	ctx := &remoteruntime.GitContext{}
	handler := makeSmudgeHandler(ctx, r, slog.New(slog.NewTextHandler(io.Discard, nil)))
	pointer := "version https://calypr.github.io/spec/v1\noid drs://example.org/object-1\nsize 13\n"
	var output bytes.Buffer
	if err := handler(context.Background(), internalfilter.FilterRequest{Pathname: "data.bin"}, bytes.NewBufferString(pointer), &output); err != nil {
		t.Fatalf("smudge handler: %v", err)
	}
	if got, want := output.String(), string(payload); got != want {
		t.Fatalf("smudged content = %q, want %q", got, want)
	}
	if got, want := r.drsURI, "drs://example.org/object-1"; got != want {
		t.Fatalf("resolver DRS URI = %q, want %q", got, want)
	}
}
