// Package lfss3 provides a small helper for Git-LFS + S3 object introspection.
//
// It:
//  1. determines the effective Git LFS storage root (.git/lfs vs git config lfs.storage)
//  2. derives a working-tree filename from the S3 object key (basename of key)
//  3. performs an S3 HEAD Object to retrieve size and user metadata (sha256 if present)
package cloud

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3ObjectParameters container for S3 object identification and access.
type S3ObjectParameters struct {
	S3URL           string
	AWSAccessKey    string
	AWSSecretKey    string
	AWSRegion       string
	AWSEndpoint     string // optional: custom endpoint (Ceph/MinIO/etc.)
	SHA256          string // optional expected hex (64 chars). Can be "sha256:<hex>" or "<hex>"
	DestinationPath string // optional override URL path (worktree filename)
}

// S3Object is what we return.
type S3Object struct {

	// Object identity
	Bucket string
	Key    string
	Path   string // basename of Key (filename), or override from input

	// HEAD-derived info
	SizeBytes   int64
	MetaSHA256  string // from user-defined object metadata (if present)
	ETag        string
	LastModTime time.Time
}

// InspectS3ForLFS does all 3 requested tasks.
func InspectS3ForLFS(ctx context.Context, in S3ObjectParameters) (*S3Object, error) {
	if strings.TrimSpace(in.S3URL) == "" {
		return nil, errors.New("S3URL is required")
	}
	if strings.TrimSpace(in.AWSRegion) == "" {
		return nil, errors.New("AWSRegion is required")
	}
	if in.AWSAccessKey == "" || in.AWSSecretKey == "" {
		return nil, errors.New("AWSAccessKey and AWSSecretKey are required")
	}

	// 2) Parse S3 URL + derive working tree filename.
	bucket, key, err := parseS3URL(in.S3URL)
	if err != nil {
		return nil, err
	}
	worktreeName := strings.TrimSpace(in.DestinationPath)
	if worktreeName == "" {
		worktreeName = path.Base(key)
		if worktreeName == "." || worktreeName == "/" || worktreeName == "" {
			return nil, fmt.Errorf("could not derive worktree name from key %q", key)
		}
	} else if worktreeName == "." || worktreeName == "/" {
		return nil, fmt.Errorf("invalid worktree name override %q", worktreeName)
	}

	// 3) HEAD on S3 to determine size and meta.SHA256.
	s3Client, err := newS3Client(ctx, in)
	if err != nil {
		return nil, err
	}
	head, err := s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 HeadObject failed (bucket=%q key=%q): %w", bucket, key, err)
	}

	metaSHA := extractSHA256FromMetadata(head.Metadata)

	// Optional: validate provided SHA256 against metadata if both exist.
	expected := normalizeSHA256(in.SHA256)
	if expected != "" && metaSHA != "" && !strings.EqualFold(expected, metaSHA) {
		return nil, fmt.Errorf("sha256 mismatch: expected=%s head.meta=%s", expected, metaSHA)
	}

	var lm time.Time
	if head.LastModified != nil {
		lm = *head.LastModified
	}

	if head.ContentLength == nil {
		return nil, fmt.Errorf("s3 HeadObject missing ContentLength (bucket=%q key=%q)", bucket, key)
	}
	sizeBytes := *head.ContentLength

	var etag string
	if head.ETag != nil {
		etag = strings.Trim(*head.ETag, `"`)
	}

	out := &S3Object{
		Bucket:      bucket,
		Key:         key,
		Path:        worktreeName,
		SizeBytes:   sizeBytes,
		MetaSHA256:  metaSHA,
		ETag:        etag,
		LastModTime: lm,
	}
	return out, nil
}

//
// --- S3 parsing + client ---
//

var virtualHostedRE = regexp.MustCompile(`^(.+?)\.s3(?:[.-]|$)`)

// parseS3URL parses s3://bucket/key, virtual-hosted HTTPS (bucket.s3.../key)
// and path-style HTTPS (s3.../bucket/key). Returns bucket and key.
func parseS3URL(raw string) (string, string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", err
	}

	switch u.Scheme {
	case "s3":
		bucket := u.Host
		key := strings.TrimPrefix(u.Path, "/")
		return bucket, key, nil
	case "http", "https":
		host := u.Hostname()

		// virtual-hosted: bucket.s3.amazonaws.com or bucket.s3-region.amazonaws.com
		if m := virtualHostedRE.FindStringSubmatch(host); m != nil {
			bucket := m[1]
			key := strings.TrimPrefix(u.Path, "/")
			return bucket, key, nil
		}

		// path-style: s3.../bucket/key
		path := strings.TrimPrefix(u.Path, "/")
		if path == "" {
			return "", "", fmt.Errorf("no bucket in URL: %s", raw)
		}
		parts := strings.SplitN(path, "/", 2)
		bucket := parts[0]
		key := ""
		if len(parts) == 2 {
			key = parts[1]
		}
		return bucket, key, nil
	default:
		return "", "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
}

func newS3Client(ctx context.Context, in S3ObjectParameters) (*s3.Client, error) {
	creds := credentials.NewStaticCredentialsProvider(in.AWSAccessKey, in.AWSSecretKey, "")

	// Custom HTTP client is useful for S3-compatible endpoints.
	httpClient := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(in.AWSRegion),
		config.WithCredentialsProvider(creds),
		config.WithHTTPClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("aws config init failed: %w", err)
	}

	opts := []func(*s3.Options){}
	if strings.TrimSpace(in.AWSEndpoint) != "" {
		ep := strings.TrimRight(in.AWSEndpoint, "/")
		opts = append(opts, func(o *s3.Options) {
			o.UsePathStyle = true // usually required for Ceph/MinIO/custom endpoints
			o.BaseEndpoint = aws.String(ep)
		})
	}

	return s3.NewFromConfig(cfg, opts...), nil
}

//
// --- SHA256 metadata extraction ---
//

var sha256HexRe = regexp.MustCompile(`(?i)^[0-9a-f]{64}$`)

func normalizeSHA256(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.ToLower(s), "sha256:")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !sha256HexRe.MatchString(s) {
		// If caller provided something malformed, treat as empty.
		// Change this to a hard error if you prefer.
		return ""
	}
	return strings.ToLower(s)
}

func extractSHA256FromMetadata(md map[string]string) string {
	if md == nil {
		return ""
	}

	// AWS SDK v2 exposes user-defined metadata WITHOUT the "x-amz-meta-" prefix,
	// and normalizes keys to lower-case.
	candidates := []string{
		"sha256",
		"checksum-sha256",
		"content-sha256",
		"oid-sha256",
		"git-lfs-sha256",
	}

	for _, k := range candidates {
		if v, ok := md[k]; ok {
			n := normalizeSHA256(v)
			if n != "" {
				return n
			}
		}
	}

	// Sometimes people stash "sha256:<hex>"
	for _, v := range md {
		if n := normalizeSHA256(v); n != "" {
			return n
		}
	}

	return ""
}
