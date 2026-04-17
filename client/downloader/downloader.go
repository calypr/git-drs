package downloader

// Package drsdownload provides shared DRS download functionality for git-drs filter commands.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/client"
	clientdrs "github.com/calypr/git-drs/client/drs"
	datadrs "github.com/calypr/syfon/client/drs"
	sycommon "github.com/calypr/syfon/client/pkg/common"
	sylogs "github.com/calypr/syfon/client/pkg/logs"
	"github.com/calypr/syfon/client/transfer"
	"github.com/calypr/syfon/client/xfer/download"
)

// DownloadToCachePath downloads the DRS object identified by oid to cachePath
// using the project-scoped URL resolution preferred by git-drs.
func DownloadToCachePath(ctx context.Context, drsCtx *client.GitContext, logger *slog.Logger, oid, cachePath string) error {
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

func (d *gitScopedDownloader) ResolveDownloadURL(ctx context.Context, guid, accessID string) (string, error) {
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
