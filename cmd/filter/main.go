// Package filter implements the git long-running filter-process protocol v2
// (https://git-scm.com/docs/gitattributes#_long_running_filter_process) for
// git-drs. It is configured as the filter.lfs.process handler and intercepts
// smudge (checkout) and clean (stage) operations, wiring them directly to the
// DRS transfer stack without spawning a separate git-lfs transfer agent.
//
// The command is hidden and invoked automatically by git when
//
//	filter.lfs.process = git-drs filter
//
// is set in the repository config (written by `git drs init`).
package filter

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

	"github.com/calypr/git-drs/client"
	clientdrs "github.com/calypr/git-drs/client/drs"
	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/lfs"
	datadrs "github.com/calypr/syfon/client/drs"
	sycommon "github.com/calypr/syfon/client/pkg/common"
	sylogs "github.com/calypr/syfon/client/pkg/logs"
	"github.com/calypr/syfon/client/transfer"
	"github.com/calypr/syfon/client/xfer/download"
	"github.com/spf13/cobra"
)

// Cmd is the hidden cobra command registered in cmd/root.go.
var Cmd = &cobra.Command{
	Use:    "filter",
	Short:  "Run git-drs as a git long-running filter process (invoked by git)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   runFilter,
}

func runFilter(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	logger := drslog.GetLogger()
	logger.Debug("Starting filter")
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Debug(fmt.Sprintf("We should probably fix this: %v", err))
		return fmt.Errorf("filter: load config: %w", err)
	}

	remote, err := cfg.GetDefaultRemote()
	if err != nil {
		return fmt.Errorf("filter: get default remote: %w", err)
	}

	drsCtx, err := cfg.GetRemoteClient(remote, logger)
	if err != nil {
		return fmt.Errorf("filter: create DRS client: %w", err)
	}

	_, lfsRoot, err := lfs.GetGitRootDirectories(ctx)
	if err != nil {
		return fmt.Errorf("filter: resolve LFS root: %w", err)
	}
	logger.Debug("Resolved LFS root directory", "lfsRoot", lfsRoot)
	// Build the filter and register handlers.
	f := lfs.NewGitFilter(os.Stdin, os.Stdout, logger).
		OnSmudge(makeSmudgeHandler(drsCtx, logger)).
		OnClean(makeCleanHandler(lfsRoot, logger))

	return f.Run(ctx)
}

// --------------------------------------------------------------------------
// Smudge handler — checkout: LFS pointer → real file content
// --------------------------------------------------------------------------

func makeSmudgeHandler(drsCtx *client.GitContext, logger *slog.Logger) lfs.SmudgeFunc {
	return func(ctx context.Context, req lfs.FilterRequest, ptr io.Reader, dst io.Writer) error {
		logger.Debug("smudge handler invoked", "pathname", req.Pathname)
		// Read the full pointer payload.
		ptrBytes, err := io.ReadAll(ptr)
		if err != nil {
			return fmt.Errorf("smudge: read pointer: %w", err)
		}

		oid, size, ok := parseLFSPointer(ptrBytes)
		if !ok {
			// Not an LFS pointer — pass content through unchanged.
			_, err := dst.Write(ptrBytes)
			return err
		}

		logger.Debug("smudge", "pathname", req.Pathname, "oid", oid, "size", size)

		// Check local LFS object cache first.
		cachePath, err := lfsObjectPath(oid)
		if err != nil {
			return fmt.Errorf("smudge: resolve cache path: %w", err)
		}

		if f, openErr := os.Open(cachePath); openErr == nil {
			defer f.Close()
			_, err = io.Copy(dst, f)
			return err
		}

		// Not cached: resolve and download from DRS.
		if err := downloadToCachePath(ctx, drsCtx, logger, oid, cachePath); err != nil {
			return fmt.Errorf("smudge: download oid %s: %w", oid, err)
		}

		f, err := os.Open(cachePath)
		if err != nil {
			return fmt.Errorf("smudge: open downloaded file: %w", err)
		}
		defer f.Close()
		_, err = io.Copy(dst, f)
		return err
	}
}

// parseLFSPointer extracts the oid and size from an LFS pointer.
// Returns ("", 0, false) if ptrBytes is not a valid LFS pointer.
func parseLFSPointer(data []byte) (oid string, size int64, ok bool) {
	text := string(data)
	if !strings.Contains(text, "version https://git-lfs.github.com/spec/v1") {
		return "", 0, false
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "oid sha256:") {
			oid = strings.TrimPrefix(line, "oid sha256:")
		}
		if strings.HasPrefix(line, "size ") {
			fmt.Sscanf(strings.TrimPrefix(line, "size "), "%d", &size)
		}
	}
	if oid == "" {
		return "", 0, false
	}
	return oid, size, true
}

// lfsObjectPath returns the standard git-lfs local object cache path for an OID.
// Layout: .git/lfs/objects/OID[:2]/OID[2:4]/OID
func lfsObjectPath(oid string) (string, error) {
	if len(oid) < 4 {
		return "", fmt.Errorf("invalid oid %q", oid)
	}
	return filepath.Join(common.LFS_OBJS_PATH, oid[:2], oid[2:4], oid), nil
}

// downloadToCachePath downloads the DRS object identified by oid to cachePath.
func downloadToCachePath(ctx context.Context, drsCtx *client.GitContext, logger *slog.Logger, oid string, cachePath string) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for cache path: %w", err)
	}

	downloader, ok := drsCtx.API.(transfer.Downloader)
	if !ok {
		return fmt.Errorf("DRS client does not implement transfer.Downloader")
	}

	scoped := &gitScopedDownloader{
		base:    downloader,
		api:     drsCtx.API,
		org:     drsCtx.Organization,
		project: drsCtx.ProjectId,
		logger:  logger,
	}

	return download.DownloadFile(ctx, drsCtx.API, scoped, oid, cachePath)
}

// gitScopedDownloader is a transfer.Downloader adapter that prefers the
// project-scoped URL resolution used by git-drs (mirrors cmd/pull internals).
type gitScopedDownloader struct {
	base    transfer.Downloader
	api     datadrs.Client
	org     string
	project string
	logger  *slog.Logger
}

func (d *gitScopedDownloader) Name() string               { return d.base.Name() }
func (d *gitScopedDownloader) Logger() *sylogs.Gen3Logger { return d.base.Logger() }

func (d *gitScopedDownloader) ResolveDownloadURL(ctx context.Context, guid string, accessID string) (string, error) {
	if strings.TrimSpace(accessID) != "" {
		return d.base.ResolveDownloadURL(ctx, guid, accessID)
	}
	accessURL, err := clientdrs.ResolveGitScopedURL(ctx, d.api, guid, d.org, d.project, d.logger)
	if err != nil {
		return "", err
	}
	if accessURL == nil || strings.TrimSpace(accessURL.Url) == "" {
		return "", fmt.Errorf("empty download URL for oid %s", guid)
	}
	return accessURL.Url, nil
}

func (d *gitScopedDownloader) Download(ctx context.Context, fdr *sycommon.FileDownloadResponseObject) (*http.Response, error) {
	return d.base.Download(ctx, fdr)
}

// --------------------------------------------------------------------------
// Clean handler — stage: real file content → LFS pointer
// --------------------------------------------------------------------------

func makeCleanHandler(lfsRoot string, logger *slog.Logger) lfs.CleanFunc {
	return func(ctx context.Context, req lfs.FilterRequest, content io.Reader, dst io.Writer) error {
		logger.Debug("clean", "pathname", req.Pathname)

		// Hash and buffer the content into a temp file in the LFS objects dir.
		objDir := filepath.Join(lfsRoot, "objects")
		if err := os.MkdirAll(objDir, 0o755); err != nil {
			return fmt.Errorf("clean: mkdir LFS objects: %w", err)
		}

		tmp, err := os.CreateTemp(objDir, "git-drs-clean-*")
		if err != nil {
			return fmt.Errorf("clean: create temp file: %w", err)
		}
		tmpPath := tmp.Name()
		defer func() {
			// Best-effort cleanup of temp file on failure paths.
			if _, statErr := os.Stat(tmpPath); statErr == nil {
				_ = os.Remove(tmpPath)
			}
		}()

		h := sha256.New()
		written, err := io.Copy(tmp, io.TeeReader(content, h))
		if err != nil {
			tmp.Close()
			return fmt.Errorf("clean: write temp file: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return fmt.Errorf("clean: close temp file: %w", err)
		}
		size := written

		oid := hex.EncodeToString(h.Sum(nil))

		// Move temp file to the final content-addressed location.
		cachePath, err := lfsObjectPath(oid)
		if err != nil {
			return fmt.Errorf("clean: resolve cache path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
			return fmt.Errorf("clean: mkdir for cache path: %w", err)
		}
		if err := os.Rename(tmpPath, cachePath); err != nil {
			return fmt.Errorf("clean: move to cache: %w", err)
		}

		// Write the LFS pointer to dst.
		pointer := fmt.Sprintf(
			"version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n",
			oid, size,
		)
		_, err = io.WriteString(dst, pointer)

		// Also record a DRS map entry so `git drs push` can find the file.
		if mapErr := writeDrsMap(req.Pathname, oid, size); mapErr != nil {
			logger.Warn("clean: failed to write DRS map entry", "pathname", req.Pathname, "error", mapErr)
		}

		return err
	}
}

// writeDrsMap records a local DRS object entry in .git/drs/lfs/objects so that
// the pre-push workflow can discover and upload the file. This mirrors the
// ObjectStore.WriteObject pattern.
func writeDrsMap(pathname string, oid string, size int64) error {
	drsObj := &datadrs.DRSObject{
		Name: filepath.Base(pathname),
		Size: size,
		Checksums: []datadrs.Checksum{
			{Type: "sha256", Checksum: oid},
		},
	}
	return lfs.WriteObject(common.DRS_OBJS_PATH, drsObj, oid)
}

// --------------------------------------------------------------------------
// GitContext alias — expose the client package type without a circular import.
// --------------------------------------------------------------------------

// Ensure drsmap is used (used inside writeDrsMap via lfs.WriteObject; we keep
// the import for the blank identifier so the compiler does not trim it).
var _ = drsmap.GetObjectPath
