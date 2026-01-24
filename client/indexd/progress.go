package indexd_client

import (
	"io"
	"net/http"
	"sync"
)

type progressReadCloser struct {
	io.ReadCloser
	report func(int64)
}

func (p *progressReadCloser) Read(buf []byte) (int, error) {
	n, err := p.ReadCloser.Read(buf)
	if n > 0 && p.report != nil {
		p.report(int64(n))
	}
	return n, err
}

type progressTransport struct {
	base   http.RoundTripper
	report func(int64)
}

func (p *progressTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil && p.report != nil {
		req.Body = &progressReadCloser{ReadCloser: req.Body, report: p.report}
	}
	return p.base.RoundTrip(req)
}

var defaultHTTPClientMu sync.Mutex

func withProgressDefaultClient(report func(int64)) func() {
	defaultHTTPClientMu.Lock()
	original := http.DefaultClient.Transport
	base := original
	if base == nil {
		base = http.DefaultTransport
	}
	http.DefaultClient.Transport = &progressTransport{base: base, report: report}
	return func() {
		http.DefaultClient.Transport = original
		defaultHTTPClientMu.Unlock()
	}
}
