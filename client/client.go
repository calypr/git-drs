// Package client provides the reusable Git-DRS client API.
//
// It is independent of the git-drs CLI and does not require a Git checkout.
package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/internal/lookup"
	"github.com/calypr/git-drs/internal/remoteruntime"
	internaltransfer "github.com/calypr/git-drs/internal/transfer"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syclient "github.com/calypr/syfon/client"
	sydownload "github.com/calypr/syfon/client/transfer/download"
)

// Options configures a DRS client. Either AccessToken or Username and
// Password must be supplied for an authenticated remote.
type Options struct {
	Endpoint     string
	AccessToken  string
	Username     string
	Password     string
	Organization string
	Project      string
	HTTPClient   *http.Client
	Logger       *slog.Logger
}

// Client is a reusable connection to one DRS/Syfon project scope.
type Client struct {
	runtime *remoteruntime.GitContext
}

// New creates a reusable Git-DRS client without reading Git configuration or
// changing process-global state.
func New(opts Options) (*Client, error) {
	if strings.TrimSpace(opts.Endpoint) == "" {
		return nil, fmt.Errorf("endpoint is required")
	}
	if strings.TrimSpace(opts.Project) == "" {
		return nil, fmt.Errorf("project is required")
	}
	if strings.TrimSpace(opts.AccessToken) == "" && (strings.TrimSpace(opts.Username) == "" || strings.TrimSpace(opts.Password) == "") {
		return nil, fmt.Errorf("access token or username and password are required")
	}

	clientOpts := make([]syclient.Option, 0, 2)
	if opts.HTTPClient != nil {
		clientOpts = append(clientOpts, syclient.WithHTTPClient(opts.HTTPClient))
	}
	if strings.TrimSpace(opts.AccessToken) != "" {
		clientOpts = append(clientOpts, syclient.WithBearerToken(opts.AccessToken))
	} else {
		clientOpts = append(clientOpts, syclient.WithBasicAuth(opts.Username, opts.Password))
	}

	raw, err := syclient.New(opts.Endpoint, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("create DRS client: %w", err)
	}
	syfonClient, ok := raw.(*syclient.Client)
	if !ok {
		return nil, fmt.Errorf("unexpected Syfon client type %T", raw)
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Client{runtime: &remoteruntime.GitContext{
		Client:       syfonClient,
		Organization: opts.Organization,
		ProjectId:    opts.Project,
		Logger:       logger,
	}}, nil
}

// File identifies one payload to pull. Path is relative to PullOptions.Root.
type File struct {
	Path string
	OID  string
	Size int64
}

// PullOptions describes the destination and files for a pull operation.
type PullOptions struct {
	Root      string
	Files     []File
	Overwrite bool
}

// Pull downloads the requested DRS payloads directly into Root. It does not
// inspect Git state, read git-drs configuration, write the Git-LFS cache, or
// update the Git index.
func (c *Client) Pull(ctx context.Context, opts PullOptions) error {
	if c == nil || c.runtime == nil {
		return fmt.Errorf("client is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(opts.Root) == "" {
		return fmt.Errorf("pull root is required")
	}

	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return fmt.Errorf("resolve pull root: %w", err)
	}
	for _, file := range opts.Files {
		rel, err := safeRelativePath(file.Path)
		if err != nil {
			return err
		}
		if strings.TrimSpace(file.OID) == "" {
			return fmt.Errorf("OID is required for %q", file.Path)
		}
		if file.Size < 0 {
			return fmt.Errorf("size must not be negative for %q", file.Path)
		}

		dst := filepath.Join(root, rel)
		if !opts.Overwrite {
			if _, statErr := os.Stat(dst); statErr == nil {
				return fmt.Errorf("destination already exists: %s", dst)
			} else if !os.IsNotExist(statErr) {
				return fmt.Errorf("stat destination %s: %w", dst, statErr)
			}
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create destination directory for %s: %w", file.Path, err)
		}
		if err := c.downloadFile(ctx, file.OID, dst); err != nil {
			return fmt.Errorf("pull %s: %w", file.Path, err)
		}
		if err := verifyFile(dst, file.OID, file.Size); err != nil {
			_ = os.Remove(dst)
			return fmt.Errorf("verify %s: %w", file.Path, err)
		}
	}
	return nil
}

func (c *Client) downloadFile(ctx context.Context, oid, dst string) error {
	objects, err := lookup.ObjectsByHashForScope(ctx, c.runtime, oid)
	if err == nil && len(objects) > 0 {
		object := objects[0]
		accessURLs, bulkErr := internaltransfer.BulkAccessURLsForObjects(ctx, c.runtime, []drsapi.DrsObject{object})
		if bulkErr == nil {
			if accessURL, ok := accessURLs[object.Id]; ok {
				return internaltransfer.DownloadResolvedToPath(ctx, c.runtime, oid, dst, &object, &accessURL, sydownload.DownloadOptions{
					MultipartThreshold: 5 * 1024 * 1024,
					Concurrency:        2,
					ChunkSize:          64 * 1024 * 1024,
				})
			}
		}
	}

	return internaltransfer.DownloadToPath(ctx, c.runtime, oid, dst)
}

func safeRelativePath(path string) (string, error) {
	path = filepath.Clean(filepath.FromSlash(strings.TrimSpace(path)))
	if path == "." || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path must be relative to pull root: %q", path)
	}
	return path, nil
}

func verifyFile(path, expectedOID string, expectedSize int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() != expectedSize {
		return fmt.Errorf("size %d does not match expected size %d", info.Size(), expectedSize)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	expected := strings.TrimPrefix(strings.TrimSpace(expectedOID), "sha256:")
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("sha256 %s does not match expected %s", actual, expected)
	}
	return nil
}
