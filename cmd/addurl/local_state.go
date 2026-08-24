package addurl

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/precommit_cache"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	sycloud "github.com/calypr/syfon/client/cloud"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type addURLDrsFile struct {
	Name          string
	Size          int64
	Oid           string
	ContentSHA256 string
}

func drsobjectBuilder(bucket, organization, project, storagePrefix string) drsobject.Builder {
	builder := drsobject.NewBuilder(bucket, project)
	builder.Organization = organization
	builder.StoragePrefix = storagePrefix
	return builder
}

func writeAddURLDrsObject(builder drsobject.Builder, file addURLDrsFile, objectPath string) (*drsapi.DrsObject, error) {
	existing, err := drsobject.ReadObject(gitrepo.DRSObjectsPath, file.Oid)
	var drsObj *drsapi.DrsObject
	if err == nil && existing != nil {
		drsObj = existing
		name := file.Name
		drsObj.Name = &name
		drsObj.Size = file.Size
	} else {
		drsID := uuid.NewSHA1(drsobject.UUIDNamespace, []byte(fmt.Sprintf("%s:%s", builder.Project, drsobject.NormalizeOid(file.Oid)))).String()
		if file.ContentSHA256 != "" {
			drsObj, err = builder.Build(file.Name, file.ContentSHA256, file.Size, drsID)
			if err != nil {
				return nil, fmt.Errorf("error building DRS object for oid %s: %w", file.Oid, err)
			}
		} else {
			drsObj = &drsapi.DrsObject{
				Id:      drsID,
				SelfUri: objectPath,
				Size:    file.Size,
				Name:    &file.Name,
			}
		}
	}

	if objectPath != "" {
		methodType := drsapi.AccessMethodTypeS3
		if u, parseErr := url.Parse(objectPath); parseErr == nil && strings.EqualFold(u.Scheme, "globus") {
			methodType = drsapi.AccessMethodType("globus")
		}
		if drsObj.AccessMethods != nil && len(*drsObj.AccessMethods) > 0 {
			am := &(*drsObj.AccessMethods)[0]
			am.Type = methodType
			am.AccessUrl = &struct {
				Headers *[]string `json:"headers,omitempty"`
				Url     string    `json:"url"`
			}{Url: objectPath}
		} else {
			drsObj.AccessMethods = &[]drsapi.AccessMethod{{
				Type: methodType,
				AccessUrl: &struct {
					Headers *[]string `json:"headers,omitempty"`
					Url     string    `json:"url"`
				}{Url: objectPath},
			}}
		}
	}

	if err := drsobject.WriteObject(gitrepo.DRSObjectsPath, drsObj, file.Oid); err != nil {
		return nil, fmt.Errorf("error writing DRS object for oid %s: %w", file.Oid, err)
	}
	return drsObj, nil
}

func placeholderOIDForUnknownSHA(etag string, sourceURL string) (string, error) {
	e := strings.TrimSpace(strings.Trim(etag, `"`))
	src := strings.TrimSpace(sourceURL)
	if e == "" {
		return "", fmt.Errorf("etag is required for placeholder oid")
	}
	if src == "" {
		return "", fmt.Errorf("source URL is required for placeholder oid")
	}
	sum := sha256.Sum256([]byte("git-drs-add-url-placeholder:v2\netag=" + e + "\nsource=" + src + "\n"))
	return fmt.Sprintf("%x", sum[:]), nil
}

// updatePrecommitCache updates the project's pre-commit cache with a mapping
// from a repository-relative `pathArg` to the given LFS `oid` and records the
// external source URL.
func updatePrecommitCache(ctx context.Context, logger *slog.Logger, pathArg, oid, externalURL string) error {
	if logger == nil {
		return errors.New("logger is required")
	}
	cache, err := precommit_cache.Open(ctx)
	if err != nil {
		return err
	}
	if err := ensureCacheDirs(cache, logger); err != nil {
		return err
	}

	relPath, err := repoRelativePath(pathArg)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	prevEntry, prevExists, err := precommit_cache.ReadPathEntry(cache, relPath)
	if err != nil {
		return err
	}
	contentChanged := prevExists && prevEntry.LFSOID != "" && prevEntry.LFSOID != oid

	if err := precommit_cache.WritePathEntry(cache, precommit_cache.PathEntry{
		Path:      relPath,
		LFSOID:    oid,
		UpdatedAt: now,
	}); err != nil {
		return err
	}
	if err := precommit_cache.UpsertOIDPath(cache, oid, "", relPath, externalURL, now, contentChanged); err != nil {
		return err
	}
	if contentChanged {
		if err := precommit_cache.RemoveOIDPath(cache, prevEntry.LFSOID, relPath, now); err != nil {
			return fmt.Errorf("remove stale OID path mapping for %s: %w", relPath, err)
		}
	}
	return nil
}

func ensureCacheDirs(cache *precommit_cache.Cache, logger *slog.Logger) error {
	if cache == nil {
		return errors.New("cache is nil")
	}
	if _, err := os.Stat(cache.Root); err != nil {
		if os.IsNotExist(err) {
			logger.Warn("pre-commit cache directory missing; creating", "path", cache.Root)
		} else {
			return err
		}
	}
	if err := precommit_cache.EnsureLayout(cache); err != nil {
		return fmt.Errorf("create cache layout: %w", err)
	}
	return nil
}

func repoRelativePath(pathArg string) (string, error) {
	if pathArg == "" {
		return "", errors.New("empty worktree path")
	}
	root, err := gitrepo.GitTopLevel()
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(pathArg)
	if filepath.IsAbs(clean) {
		clean, err = filepath.EvalSymlinks(clean)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, clean)
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("path %s is outside repo root %s", clean, root)
		}
		return filepath.ToSlash(rel), nil
	}
	return filepath.ToSlash(clean), nil
}

func writePointerFile(pathArg, oid string, sizeBytes int64, placeholder bool) error {
	pointer := "version https://git-lfs.github.com/spec/v1\n"
	if placeholder {
		pointer += fmt.Sprintf("ext-0-gitdrsplaceholder sha256:%s\n", oid)
	}
	pointer += fmt.Sprintf("oid sha256:%s\nsize %d\n", oid, sizeBytes)
	if pathArg == "" {
		return fmt.Errorf("empty worktree path")
	}
	safePath := filepath.Clean(pathArg)
	dir := filepath.Dir(safePath)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(safePath, []byte(pointer), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", safePath, err)
	}

	if _, err := fmt.Fprintf(os.Stderr, "Added Git LFS pointer file at %s\n", safePath); err != nil {
		return fmt.Errorf("stderr write: %w", err)
	}
	return nil
}

func maybeTrackLFS(ctx context.Context, gitLFSTrack func(context.Context, string) (bool, error), pathArg string, isTracked bool) error {
	if isTracked {
		return nil
	}
	if _, err := gitLFSTrack(ctx, pathArg); err != nil {
		return fmt.Errorf("git lfs track %s: %w", pathArg, err)
	}

	if _, err := fmt.Fprintf(os.Stderr, "Info: Added to Git LFS. Remember to `git add %s` and `git commit ...`", pathArg); err != nil {
		return fmt.Errorf("stderr write: %w", err)
	}
	return nil
}

func printResolvedInfo(cmd *cobra.Command, gitCommonDir, lfsRoot string, objectInfo *sycloud.ObjectInfo, pathArg string, isTracked bool, sha256 string) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), `
Resolved Git LFS Object Info
----------------------------
Git common dir : %s
LFS storage    : %s

Cloud object
------------
Bucket         : %s
Key            : %s
Worktree name  : %s
Size (bytes)   : %d
SHA256 (meta)  : %s
ETag           : %s
Last modified  : %s

Worktree
-------------
path           : %s
tracked by LFS : %v
sha256 param  : %s

`,
		gitCommonDir,
		lfsRoot,
		objectInfo.Bucket,
		objectInfo.Key,
		objectInfo.Path,
		objectInfo.SizeBytes,
		objectInfo.MetaSHA256,
		objectInfo.ETag,
		objectInfo.LastModTime.Format("2006-01-02T15:04:05Z07:00"),
		pathArg,
		isTracked,
		sha256,
	); err != nil {
		return fmt.Errorf("print resolved object info: %w", err)
	}
	return nil
}
