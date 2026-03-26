package addurl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/hash"
	"github.com/calypr/git-drs/cloud"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/gitrepo"
	"github.com/calypr/git-drs/lfs"
	"github.com/spf13/cobra"
)

// AddURLService groups injectable dependencies used to implement the add-url
// behavior (logger factory, cloud inspection, LFS helpers, config loader, etc.).
type AddURLService struct {
	newLogger           func(string, bool) (*slog.Logger, error)
	inspectBucketObject func(ctx context.Context, url string) (*cloud.ObjectMeta, error)
	isLFSTracked        func(path string) (bool, error)
	getGitRoots         func(ctx context.Context) (string, string, error)
	gitLFSTrack         func(ctx context.Context, path string) (bool, error)
	loadConfig          func() (*config.Config, error)
}

// NewAddURLService constructs an AddURLService populated with production
// implementations of its dependencies.
func NewAddURLService() *AddURLService {
	return &AddURLService{
		newLogger:           drslog.NewLogger,
		inspectBucketObject: cloud.HeadObject,
		isLFSTracked:        lfs.IsLFSTracked,
		getGitRoots:         lfs.GetGitRootDirectories,
		gitLFSTrack:         lfs.GitLFSTrackReadOnly,
		loadConfig:          config.LoadConfig,
	}
}

// Run executes the add-url workflow: parse CLI input, inspect the remote object,
// ensure the LFS sentinel exists in local storage, write a Git LFS pointer file,
// update the pre-commit cache (best-effort), optionally add a git-lfs track
// entry, and record the DRS mapping.
func (s *AddURLService) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	logger, err := s.newLogger("", false)
	if err != nil {
		return fmt.Errorf("error creating logger: %v", err)
	}

	rawURL := args[0]
	pathArg, err := resolvePathArg(rawURL, args)
	if err != nil {
		return err
	}

	sha256Param, err := cmd.Flags().GetString("sha256")
	if err != nil {
		return fmt.Errorf("read flag sha256: %w", err)
	}

	objMeta, err := s.inspectBucketObject(ctx, rawURL)
	if err != nil {
		return err
	}

	isTracked, err := s.isLFSTracked(pathArg)
	if err != nil {
		return fmt.Errorf("check LFS tracking for %s: %w", pathArg, err)
	}

	gitCommonDir, lfsRoot, err := s.getGitRoots(ctx)
	if err != nil {
		return fmt.Errorf("get git root directories: %w", err)
	}

	if err := printResolvedInfo(cmd, gitCommonDir, lfsRoot, objMeta, pathArg, isTracked, sha256Param); err != nil {
		return err
	}

	oid, err := s.ensureLFSObject(ctx, objMeta, pathArg, sha256Param, lfsRoot)
	if err != nil {
		return err
	}

	if err := writePointerFile(pathArg, oid, int64(len(objMeta.ETag))); err != nil {
		return err
	}

	if err := updatePrecommitCache(ctx, logger, pathArg, oid, rawURL); err != nil {
		logger.Warn("pre-commit cache update skipped", "error", err)
	}

	if err := maybeTrackLFS(ctx, s.gitLFSTrack, pathArg, isTracked); err != nil {
		return err
	}

	cfg, err := s.loadConfig()
	if err != nil {
		return fmt.Errorf("error getting config: %v", err)
	}

	remote, err := cfg.GetDefaultRemote()
	if err != nil {
		return err
	}

	remoteConfig := cfg.GetRemote(remote)
	if remoteConfig == nil {
		return fmt.Errorf("error getting remote configuration for %s", remote)
	}

	builder := drs.NewObjectBuilder(remoteConfig.GetBucketName(), remoteConfig.GetProjectId())

	file := lfs.LfsFileInfo{
		Name: pathArg,
		Size: int64(len(objMeta.ETag)),
		Oid:  oid,
	}

	aliases := []string{"git-drs-remote-url:true"}
	checksums := hash.HashInfo{ETag: objMeta.ETag}
	if _, err := drsmap.WriteDrsFile(builder, file, &rawURL, aliases, &checksums); err != nil {
		return fmt.Errorf("error WriteDrsFile: %v", err)
	}

	return nil
}

// ensureLFSObject ensures an LFS sentinel object exists for the remote URL.
// If sha256Param is provided it is used directly as the OID; otherwise the
// SHA256 of the object's ETag is computed and the ETag bytes are written as
// sentinel data into the LFS objects directory.
func (s *AddURLService) ensureLFSObject(ctx context.Context, objMeta *cloud.ObjectMeta, pathArg, sha256Param, lfsRoot string) (string, error) {
	if sha256Param != "" {
		return sha256Param, nil
	}

	if objMeta == nil || objMeta.ETag == "" {
		return "", fmt.Errorf("missing ETag for object; cannot compute SHA256")
	}

	computedSHA := getSHA256(objMeta.ETag)
	sentinelData := []byte(objMeta.ETag)

	oid := computedSHA
	dstDir := filepath.Join(lfsRoot, "objects", oid[0:2], oid[2:4])
	dstObj := filepath.Join(dstDir, oid)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dstDir, err)
	}

	if err := os.WriteFile(dstObj, sentinelData, 0644); err != nil {
		return "", fmt.Errorf("write sentinel to %s: %w", dstObj, err)
	}

	if _, err := fmt.Fprintf(os.Stderr, "Added data file at etag:%s computed sha256:%s path:%s\n", objMeta.ETag, computedSHA, pathArg); err != nil {
		return "", fmt.Errorf("stderr write: %w", err)
	}

	return computedSHA, nil
}

// getSHA256 computes the SHA256 hash of the input string and returns it as a hex-encoded string.
func getSHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ensureAllowIncompletePush checks if lfs.allowincompletepush is set to true in the local
// git config. This setting is required to allow pushing commits that reference LFS objects
// that don't exist in the local LFS store (sentinel data files created by addurl).
// If the setting is not true, it sets it automatically.
func ensureAllowIncompletePush() error {
	value, err := gitrepo.GetGitConfigString("lfs.allowincompletepush")
	if err != nil {
		return fmt.Errorf("failed to read lfs.allowincompletepush: %w", err)
	}

	if value != "true" {
		configs := map[string]string{
			"lfs.allowincompletepush": "true",
		}
		if err := gitrepo.SetGitConfigOptions(configs); err != nil {
			return fmt.Errorf("failed to set lfs.allowincompletepush: %w", err)
		}
	}

	return nil
}
