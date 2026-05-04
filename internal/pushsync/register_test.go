package pushsync

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestProbeDownloadURLUsesProvidedHTTPClient(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodHead {
			t.Fatalf("expected HEAD request, got %s", r.Method)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}

	if err := probeDownloadURL(context.Background(), client, "https://signed.example/object.bin"); err != nil {
		t.Fatalf("probeDownloadURL returned error: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
