package transfer

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/globusauth"
)

const globusDestCollectionEnv = "GIT_DRS_GLOBUS_DESTINATION_COLLECTION"

type globusLocator struct {
	Collection string
	Path       string
}

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

func globusDestinationForCachePath(cachePath string) (globusLocator, error) {
	collection := strings.TrimSpace(os.Getenv(globusDestCollectionEnv))
	if collection == "" {
		return globusLocator{}, fmt.Errorf("Globus destination collection is required for globus:// access URLs; set %s and authenticate with `git drs auth globus`", globusDestCollectionEnv)
	}
	clean := filepath.Clean(cachePath)
	rel, err := filepath.Rel(gitrepo.LFSObjectsPath, clean)
	if err != nil || filepath.IsAbs(clean) || rel == "." || !filepath.IsLocal(rel) {
		return globusLocator{}, fmt.Errorf("Globus destination must be inside %s", gitrepo.LFSObjectsPath)
	}
	return globusLocator{Collection: collection, Path: path.Join("/", filepath.ToSlash(clean))}, nil
}

func transferGlobusToCachePath(ctx context.Context, accessURL, cachePath string) error {
	src, err := parseGlobusURL(accessURL)
	if err != nil {
		return err
	}
	dst, err := globusDestinationForCachePath(cachePath)
	if err != nil {
		return err
	}
	client, err := globusauth.NewClientFromEnv()
	if err != nil {
		return err
	}
	if _, err := globusauth.Check(ctx, client); err != nil {
		return err
	}
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
