// Package lfss3 provides a small helper for Git-LFS + S3 object introspection.
//
// It:
//  1. determines the effective Git LFS storage root (.git/lfs vs git config lfs.storage)
//  2. derives a working-tree filename from the S3 object key (basename of key)
//  3. performs an S3 HEAD Object to retrieve size and user metadata (sha256 if present)
package lfss3

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/calypr/git-drs/utils"
)

// InspectInput is the drop-in input you requested.
type InspectInput struct {
	S3URL        string
	AWSAccessKey string
	AWSSecretKey string
	AWSRegion    string
	AWSEndpoint  string // optional: custom endpoint (Ceph/MinIO/etc.)
	SHA256       string // optional expected hex (64 chars). Can be "sha256:<hex>" or "<hex>"
	WorktreeName string // optional override of derived worktree name
}

// InspectResult is what we return.
type InspectResult struct {
	// Git/LFS paths
	GitCommonDir string // result of: git rev-parse --git-common-dir
	LFSRoot      string // either lfs.storage (resolved) or <gitCommonDir>/lfs

	// Object identity
	Bucket       string
	Key          string
	WorktreeName string // basename of Key (filename), or override from input

	// HEAD-derived info
	SizeBytes   int64
	MetaSHA256  string // from user-defined object metadata (if present)
	ETag        string
	LastModTime time.Time
}

// InspectS3ForLFS does all 3 requested tasks.
func InspectS3ForLFS(ctx context.Context, in InspectInput) (*InspectResult, error) {
	if strings.TrimSpace(in.S3URL) == "" {
		return nil, errors.New("S3URL is required")
	}
	if strings.TrimSpace(in.AWSRegion) == "" {
		return nil, errors.New("AWSRegion is required")
	}
	if in.AWSAccessKey == "" || in.AWSSecretKey == "" {
		return nil, errors.New("AWSAccessKey and AWSSecretKey are required")
	}

	// 1) Determine Git LFS storage root.
	gitCommonDir, lfsRoot, err := GetGitRootDirectories(ctx)
	if err != nil {
		return nil, err
	}

	// 2) Parse S3 URL + derive working tree filename.
	bucket, key, err := parseS3URL(in.S3URL)
	if err != nil {
		return nil, err
	}
	worktreeName := strings.TrimSpace(in.WorktreeName)
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

	out := &InspectResult{
		GitCommonDir: gitCommonDir,
		LFSRoot:      lfsRoot,
		Bucket:       bucket,
		Key:          key,
		WorktreeName: worktreeName,
		SizeBytes:    sizeBytes,
		MetaSHA256:   metaSHA,
		ETag:         etag,
		LastModTime:  lm,
	}
	return out, nil
}

// GetGitRootDirectories returns (gitCommonDir, lfsRoot, error).
func GetGitRootDirectories(ctx context.Context) (string, string, error) {
	gitCommonDir, err := gitRevParseGitCommonDir(ctx)
	if err != nil {
		return "", "", err
	}
	lfsRoot, err := resolveLFSRoot(ctx, gitCommonDir)
	if err != nil {
		return "", "", err
	}
	if lfsRoot == "" {
		lfsRoot = filepath.Join(gitCommonDir, "lfs")
	}
	return gitCommonDir, lfsRoot, nil
}

//
// --- Git helpers ---
//

func GitLFSTrack(ctx context.Context, path string) (bool, error) {
	out, err := runGit(ctx, "lfs", "track", path)
	if err != nil {
		return false, fmt.Errorf("git lfs track failed: %w", err)
	}
	return strings.Contains(out, path), nil
}

func GitLFSTrackReadOnly(ctx context.Context, path string) (bool, error) {
	_, err := GitLFSTrack(ctx, path)
	if err != nil {
		return false, fmt.Errorf("git lfs track failed: %w", err)
	}

	repoRoot, err := utils.GitTopLevel()
	if err != nil {
		return false, err
	}

	attrPath := filepath.Join(repoRoot, ".gitattributes")
	changed, err := UpsertDRSRouteLines(attrPath, "ro", []string{path})
	if err != nil {
		return false, err
	}

	return changed, nil
}

func GetGitAttribute(ctx context.Context, attr string, path string) (string, error) {
	out, err := runGit(ctx, "check-attr", attr, "--", path)
	if err != nil {
		return "", fmt.Errorf("git check-attr failed: %w", err)
	}
	return out, nil
}

func gitRevParseGitCommonDir(ctx context.Context) (string, error) {
	out, err := runGit(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-common-dir failed: %w", err)
	}
	dir := strings.TrimSpace(out)
	if dir == "" {
		return "", errors.New("git rev-parse returned empty --git-common-dir")
	}
	// If relative, resolve it against current working directory.
	if !filepath.IsAbs(dir) {
		abs, err := filepath.Abs(dir)
		if err == nil {
			dir = abs
		}
	}
	return dir, nil
}

// resolveLFSRoot implements:
// - if `git config --get lfs.storage` is set: use it
//   - if relative: resolve relative to GitCommonDir (this is how git-lfs treats it in practice)
//
// - else: <GitCommonDir>/lfs
func resolveLFSRoot(ctx context.Context, gitCommonDir string) (string, error) {
	// NOTE: git config --get returns exit status 1 if key not found.
	out, err := runGitAllowMissing(ctx, "config", "--get", "lfs.storage")
	if err != nil {
		return "", fmt.Errorf("git config --get lfs.storage failed: %w", err)
	}
	val := strings.TrimSpace(out)

	if val == "" {
		return filepath.Clean(filepath.Join(gitCommonDir, "lfs")), nil
	}

	// Expand ~ if present (nice-to-have).
	if strings.HasPrefix(val, "~") && (len(val) == 1 || val[1] == '/' || val[1] == '\\') {
		home, herr := userHomeDir()
		if herr == nil && home != "" {
			val = filepath.Join(home, strings.TrimPrefix(val, "~"))
		}
	}

	if !filepath.IsAbs(val) {
		val = filepath.Join(gitCommonDir, val)
	}
	return filepath.Clean(val), nil
}

func runGit(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(b)))
	}
	return string(b), nil
}

// runGitAllowMissing treats "key not found" as empty output, not an error.
func runGitAllowMissing(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		// "git config --get missing.key" exits 1 with empty output.
		s := strings.TrimSpace(string(b))
		if s == "" {
			return "", nil
		}
		return "", fmt.Errorf("%v: %s", err, s)
	}
	return string(b), nil
}

func userHomeDir() (string, error) {
	// Avoid os/user on some cross-compile scenarios; keep it simple.
	if runtime.GOOS == "windows" {
		// Not your target, but safe fallback.
		return "", errors.New("home expansion not supported on windows in this helper")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return home, nil
	}
	// macOS/Linux
	out, err := exec.Command("sh", "-lc", "printf %s \"$HOME\"").CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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

func newS3Client(ctx context.Context, in InspectInput) (*s3.Client, error) {
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

// IsLFSTracked returns true if the given path is tracked by Git LFS
// (i.e. has `filter=lfs` via git attributes).
func IsLFSTracked(path string) (bool, error) {
	if path == "" {
		return false, fmt.Errorf("path is empty")
	}

	// Git prefers forward slashes, even on macOS/Linux
	cleanPath := filepath.ToSlash(path)

	cmd := exec.Command(
		"git",
		"check-attr",
		"filter",
		"--",
		cleanPath,
	)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("git check-attr failed: %w (%s)", err, out.String())
	}

	// Expected output:
	// path: filter: lfs
	// path: filter: unspecified
	//
	// Format is stable and documented.
	fields := strings.Split(out.String(), ":")
	if len(fields) < 3 {
		return false, nil
	}

	value := strings.TrimSpace(fields[2])
	return value == "lfs", nil
}
