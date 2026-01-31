package cloud

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// progressReader wraps an io.ReadCloser and periodically writes progress to stderr.
type progressReader struct {
	rc     io.ReadCloser
	label  string
	start  time.Time
	total  int64 // accessed atomically
	quit   chan struct{}
	done   chan struct{}
	ticker time.Duration
}

func newProgressReader(rc io.ReadCloser, label string) io.ReadCloser {
	p := &progressReader{
		rc:     rc,
		label:  label,
		start:  time.Now(),
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
		ticker: 500 * time.Millisecond,
	}

	go func() {
		t := time.NewTicker(p.ticker)
		defer t.Stop()
		var last int64
		for {
			select {
			case <-t.C:
				total := atomic.LoadInt64(&p.total)
				elapsed := time.Since(p.start).Seconds()
				var rate float64
				if elapsed > 0 {
					rate = float64(total) / elapsed
				}
				// \r to overwrite the same line like git pull; no newline until done
				fmt.Fprintf(os.Stderr, "\r%s: %d bytes (%.1f KiB/s)", p.label, total, rate/1024)
				last = total
			case <-p.quit:
				// final line (replace same line, then newline)
				total := atomic.LoadInt64(&p.total)
				_ = last // in case we want to use last for something later
				fmt.Fprintf(os.Stderr, "\r%s: %d bytes\n", p.label, total)
				close(p.done)
				return
			}
		}
	}()

	return p
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.rc.Read(b)
	if n > 0 {
		atomic.AddInt64(&p.total, int64(n))
	}
	return n, err
}

func (p *progressReader) Close() error {
	// Close underlying reader first, then stop progress goroutine and wait for completion.
	err := p.rc.Close()
	close(p.quit)
	<-p.done
	return err
}

// AgentFetchReader fetches the object described by `input` and returns an io.ReadCloser.
// It accepts `s3://bucket/key` URLs and converts them to HTTPS URLs. If `input.AWSEndpoint`
// is set it will use that endpoint in path-style (endpoint/bucket/key); otherwise it
// uses the default virtual-hosted AWS form: https://{bucket}.s3.amazonaws.com/{key}.
func AgentFetchReader(ctx context.Context, input S3ObjectParameters) (io.ReadCloser, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	raw := strings.TrimSpace(input.S3URL)
	if raw == "" {
		return nil, fmt.Errorf("AgentFetchReader: S3ObjectParameters.S3URL is empty")
	}

	useSignedFetch := strings.TrimSpace(input.AWSAccessKey) != "" ||
		strings.TrimSpace(input.AWSSecretKey) != "" ||
		strings.TrimSpace(input.AWSRegion) != ""
	if useSignedFetch {
		if strings.TrimSpace(input.AWSAccessKey) == "" || strings.TrimSpace(input.AWSSecretKey) == "" || strings.TrimSpace(input.AWSRegion) == "" {
			return nil, fmt.Errorf("AgentFetchReader: AWSAccessKey, AWSSecretKey, and AWSRegion are required for signed fetch")
		}

		bucket, key, err := parseS3URL(raw)
		if err != nil {
			return nil, fmt.Errorf("AgentFetchReader: parse s3 url %q: %w", raw, err)
		}

		s3Client, err := newS3Client(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("AgentFetchReader: init s3 client: %w", err)
		}

		out, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return nil, fmt.Errorf("AgentFetchReader: s3 GetObject failed (bucket=%q key=%q): %w", bucket, key, err)
		}
		if out.Body == nil {
			return nil, fmt.Errorf("AgentFetchReader: response body is nil for s3://%s/%s", bucket, key)
		}

		label := fmt.Sprintf("fetch s3://%s/%s", bucket, key)
		return newProgressReader(out.Body, label), nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("AgentFetchReader: parse url %q: %w", raw, err)
	}

	var s3url string
	switch u.Scheme {
	case "s3":
		bucket := u.Host
		key := strings.TrimPrefix(u.Path, "/")
		if bucket == "" || key == "" {
			return nil, fmt.Errorf("AgentFetchReader: invalid s3 URL %q", raw)
		}
		if ep := strings.TrimSpace(input.AWSEndpoint); ep != "" {
			// ensure endpoint has a scheme
			if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
				ep = "https://" + ep
			}
			s3url = strings.TrimRight(ep, "/") + "/" + bucket + "/" + key
		} else {
			s3url = fmt.Sprintf("https://%s.s3.amazonaws.com/%s", bucket, key)
		}
	case "", "http", "https":
		// allow bare host/path (no scheme) by assuming https, otherwise use as-is
		if u.Scheme == "" {
			s3url = "https://" + raw
		} else {
			s3url = raw
		}
	default:
		return nil, fmt.Errorf("AgentFetchReader: unsupported URL scheme %q", u.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s3url, nil)
	if err != nil {
		return nil, fmt.Errorf("AgentFetchReader: create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AgentFetchReader: http get %s: %w", s3url, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("AgentFetchReader: unexpected status %d fetching %s; failed to drain body: %w", resp.StatusCode, s3url, copyErr)
		}
		if closeErr := resp.Body.Close(); closeErr != nil {
			return nil, fmt.Errorf("AgentFetchReader: unexpected status %d fetching %s; failed to close body: %w", resp.StatusCode, s3url, closeErr)
		}
		return nil, fmt.Errorf("AgentFetchReader: unexpected status %d fetching %s", resp.StatusCode, s3url)
	}

	if resp.Body == nil {
		return nil, fmt.Errorf("AgentFetchReader: response body is nil for %s", s3url)
	}
	// wrap response body with progress reporting that writes to stderr
	label := fmt.Sprintf("fetch %s", s3url)
	return newProgressReader(resp.Body, label), nil
}
