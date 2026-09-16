package transfer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/globusauth"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

const globusDestCollectionEnv = "GIT_DRS_GLOBUS_DESTINATION_COLLECTION"

type globusLocator struct {
	Collection string
	Path       string
}

type GlobusDownload struct {
	OID, CachePath string
	Object         *drsapi.DrsObject
	AccessURL      string
	Placeholder    bool
}

type globusClient interface {
	SubmitTransfer(context.Context, string, string, string, string, string) (string, error)
	SubmitTransferItems(context.Context, string, string, []globusauth.TransferItem, string) (string, error)
	WaitForTask(context.Context, string, time.Duration) error
	Close() error
}

var newGlobusClient = func(ctx context.Context) (globusClient, error) { return globusauth.NewClient(ctx) }

func isGlobusURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && strings.EqualFold(u.Scheme, "globus") && u.Host != ""
}

func parseGlobusURL(raw string) (globusLocator, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "globus") || u.Host == "" {
		return globusLocator{}, fmt.Errorf("invalid Globus access URL %q; expected globus://<collection-id>/<path>", raw)
	}
	p := path.Clean("/" + strings.TrimPrefix(u.EscapedPath(), "/"))
	if p == "/" || p == "." {
		return globusLocator{}, fmt.Errorf("invalid Globus access URL %q; source path is required", raw)
	}
	decodedPath, err := url.PathUnescape(p)
	if err != nil {
		return globusLocator{}, fmt.Errorf("invalid Globus access URL %q: %w", raw, err)
	}
	return globusLocator{Collection: u.Host, Path: decodedPath}, nil
}

func globusDestinationForCachePath(drsCtx *remoteruntime.GitContext, sourceCollection, cachePath string) (globusLocator, error) {
	collection, repositoryPath, err := resolveGlobusDestination(drsCtx, sourceCollection)
	if err != nil {
		return globusLocator{}, err
	}
	clean := filepath.Clean(cachePath)
	rel, err := filepath.Rel(gitrepo.LFSObjectsPath, clean)
	if err != nil || filepath.IsAbs(clean) || rel == "." || !filepath.IsLocal(rel) {
		return globusLocator{}, fmt.Errorf("Globus destination must be inside %s", gitrepo.LFSObjectsPath)
	}
	return globusLocator{Collection: collection, Path: path.Join(repositoryPath, filepath.ToSlash(clean))}, nil
}

func resolveGlobusDestination(drsCtx *remoteruntime.GitContext, sourceCollection string) (string, string, error) {
	if drsCtx != nil && drsCtx.AllowedGlobusSources != nil {
		source := strings.ToLower(strings.TrimSpace(sourceCollection))
		allowed := false
		for _, candidate := range drsCtx.AllowedGlobusSources {
			if source == candidate {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", "", fmt.Errorf("source_collection_disallowed: Globus source collection %q is not permitted by repository policy", sourceCollection)
		}
	}
	collection := strings.TrimSpace(os.Getenv(globusDestCollectionEnv))
	if collection == "" {
		if drsCtx != nil {
			collection = drsCtx.GlobusCollections[strings.ToLower(strings.TrimSpace(sourceCollection))]
			if collection == "" {
				collection = drsCtx.GlobusDefaultDestination
			}
		}
		if strings.TrimSpace(collection) == "" {
			return "", "", fmt.Errorf("destination_collection_unmapped: no Globus destination configured for source collection %q; set %s, globus-collection, or globus-default-destination", sourceCollection, globusDestCollectionEnv)
		}
	}
	collection = strings.TrimSpace(collection)
	repositoryPath := "/"
	if drsCtx != nil {
		if configured := strings.TrimSpace(drsCtx.GlobusDestinationPaths[strings.ToLower(collection)]); configured != "" {
			repositoryPath = configured
		}
	}
	repositoryPath, err := normalizeGlobusRepositoryPath(repositoryPath)
	if err != nil {
		return "", "", err
	}
	return collection, repositoryPath, nil
}

func normalizeGlobusRepositoryPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/", nil
	}
	if !strings.HasPrefix(raw, "/") || strings.Contains(raw, "\\") {
		return "", fmt.Errorf("destination_repository_path_invalid: Globus repository path %q must be collection-absolute", raw)
	}
	for _, part := range strings.Split(raw, "/") {
		if part == ".." {
			return "", fmt.Errorf("destination_repository_path_invalid: Globus repository path %q contains traversal", raw)
		}
	}
	return path.Clean(raw), nil
}

func transferGlobusToCachePath(ctx context.Context, drsCtx *remoteruntime.GitContext, accessURL, cachePath string) error {
	src, err := parseGlobusURL(accessURL)
	if err != nil {
		return err
	}
	dst, err := globusDestinationForCachePath(drsCtx, src.Collection, cachePath)
	if err != nil {
		return err
	}
	client, err := newGlobusClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("mkdir for cache path: %w", err)
	}
	taskID, err := client.SubmitTransfer(ctx, src.Collection, src.Path, dst.Collection, dst.Path, "git-drs pull")
	if err != nil {
		return fmt.Errorf("submit Globus transfer from %s to %s:%s: %w", accessURL, dst.Collection, dst.Path, err)
	}
	if err := client.WaitForTask(ctx, taskID, 0); err != nil {
		return err
	}
	return nil
}

func DownloadGlobusBatch(ctx context.Context, drsCtx *remoteruntime.GitContext, downloads []GlobusDownload) error {
	type groupKey struct{ source, destination string }
	type group struct {
		items     []globusauth.TransferItem
		downloads []GlobusDownload
	}
	groups := map[groupKey]*group{}
	seen := make(map[string]struct{}, len(downloads))
	for _, download := range downloads {
		if _, ok := seen[download.CachePath]; ok {
			continue
		}
		seen[download.CachePath] = struct{}{}
		src, err := parseGlobusURL(download.AccessURL)
		if err != nil {
			return err
		}
		dst, err := globusDestinationForCachePath(drsCtx, src.Collection, download.CachePath)
		if err != nil {
			return err
		}
		key := groupKey{source: src.Collection, destination: dst.Collection}
		if groups[key] == nil {
			groups[key] = &group{}
		}
		// Globus collection checksum capabilities are not advertised here, so rely
		// on Globus verification in transit and authoritative DRS checks locally.
		groups[key].items = append(groups[key].items, globusauth.TransferItem{SourcePath: src.Path, DestinationPath: dst.Path})
		groups[key].downloads = append(groups[key].downloads, download)
	}
	if len(groups) == 0 {
		return nil
	}
	client, err := newGlobusClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()
	for key, group := range groups {
		for _, download := range group.downloads {
			if err := os.MkdirAll(filepath.Dir(download.CachePath), 0o755); err != nil {
				return err
			}
		}
		taskID, err := client.SubmitTransferItems(ctx, key.source, key.destination, group.items, "git-drs pull")
		if err != nil {
			return errors.Join(err, removeGlobusDownloads(group.downloads))
		}
		if err := client.WaitForTask(ctx, taskID, 0); err != nil {
			return errors.Join(err, removeGlobusDownloads(group.downloads))
		}
		for _, download := range group.downloads {
			if err := verifyGlobusDownload(download.CachePath, download.OID, download.Object, download.Placeholder); err != nil {
				return errors.Join(err, removeGlobusDownloads(group.downloads))
			}
		}
	}
	return nil
}

func removeGlobusDownloads(downloads []GlobusDownload) error {
	var errs []error
	for _, download := range downloads {
		if err := os.Remove(download.CachePath); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove incomplete Globus download %s: %w", download.CachePath, err))
		}
	}
	return errors.Join(errs...)
}
