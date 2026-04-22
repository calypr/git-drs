package addurl

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/calypr/git-drs/cloud"
	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/gitrepo"
	"github.com/calypr/git-drs/lfs"
	"github.com/spf13/cobra"
)

// AddURLService groups injectable dependencies used to implement the add-url
// behavior (logger factory, object inspection, LFS helpers, config loader, etc.).
type AddURLService struct {
	newLogger     func(string, bool) (*slog.Logger, error)
	inspectObject func(ctx context.Context, input cloud.ObjectParameters) (*cloud.ObjectInfo, error)
	isLFSTracked  func(path string) (bool, error)
	getGitRoots   func(ctx context.Context) (string, string, error)
	gitLFSTrack   func(ctx context.Context, path string) (bool, error)
	loadConfig    func() (*config.Config, error)
}

// NewAddURLService constructs an AddURLService populated with production
// implementations of its dependencies.
func NewAddURLService() *AddURLService {
	return &AddURLService{
		newLogger:     drslog.NewLogger,
		inspectObject: cloud.InspectObjectForLFS,
		isLFSTracked:  lfs.IsLFSTracked,
		getGitRoots:   lfs.GetGitRootDirectories,
		gitLFSTrack:   lfs.GitLFSTrackReadOnly,
		loadConfig:    config.LoadConfig,
	}
}

// Run executes the add-url workflow: parse CLI input, inspect the cloud object,
// ensure the LFS object exists in local storage, write a pointer file, update
// the pre-commit cache (best-effort), optionally add a tracking entry, and
// record the DRS mapping.
func (s *AddURLService) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	logger, err := s.newLogger("", false)
	if err != nil {
		return fmt.Errorf("error creating logger: %v", err)
	}

	input, err := parseAddURLInput(cmd, args)
	if err != nil {
		return err
	}

	objectInfo, err := s.inspectObject(ctx, input.objectParams)
	if err != nil {
		return err
	}

	isTracked, err := s.isLFSTracked(input.path)
	if err != nil {
		return fmt.Errorf("check LFS tracking for %s: %w", input.path, err)
	}

	gitCommonDir, lfsRoot, err := s.getGitRoots(ctx)
	if err != nil {
		return fmt.Errorf("get git root directories: %w", err)
	}

	if err := printResolvedInfo(cmd, gitCommonDir, lfsRoot, objectInfo, input.path, isTracked, input.sha256); err != nil {
		return err
	}

	oid, err := s.ensureLFSObject(ctx, objectInfo, input, lfsRoot)
	if err != nil {
		return err
	}

	if err := writePointerFile(input.path, oid, objectInfo.SizeBytes); err != nil {
		return err
	}

	if err := updatePrecommitCache(ctx, logger, input.path, oid, input.objectURL); err != nil {
		logger.Warn("pre-commit cache update skipped", "error", err)
	}

	if err := maybeTrackLFS(ctx, s.gitLFSTrack, input.path, isTracked); err != nil {
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

	org, project, scope, err := resolveTargetScope(remoteConfig)
	if err != nil {
		return err
	}

	builder := common.NewObjectBuilder(scope.Bucket, project)
	builder.Organization = org
	builder.StoragePrefix = scope.Prefix

	file := lfs.LfsFileInfo{
		Name: input.path,
		Size: objectInfo.SizeBytes,
		Oid:  oid,
	}
	if _, err := drsmap.WriteDrsFile(builder, file, &input.objectURL); err != nil {
		return fmt.Errorf("error WriteDrsFile: %v", err)
	}

	return nil
}

func resolveTargetScope(remoteConfig config.DRSRemote) (organization string, project string, scope gitrepo.ResolvedBucketScope, err error) {
	organization = remoteConfig.GetOrganization()
	project = remoteConfig.GetProjectId()
	if project == "" {
		return "", "", gitrepo.ResolvedBucketScope{}, fmt.Errorf("target project is required (set remote project)")
	}

	scope, err = gitrepo.ResolveBucketScope(
		organization,
		project,
		remoteConfig.GetBucketName(),
		remoteConfig.GetStoragePrefix(),
	)
	if err != nil {
		return "", "", gitrepo.ResolvedBucketScope{}, err
	}
	return organization, project, scope, nil
}

// ensureLFSObject ensures the LFS object identified by objectInfo exists in the
// repository's LFS storage. If SHA256 is provided, it is trusted and returned.
// Otherwise we create a sentinel object and synthetic OID derived from ETag,
// deferring true checksum validation to first real data use.
func (s *AddURLService) ensureLFSObject(ctx context.Context, objectInfo *cloud.ObjectInfo, input addURLInput, lfsRoot string) (string, error) {
	_ = ctx
	if input.sha256 != "" {
		return input.sha256, nil
	}

	oid, err := lfs.SyntheticOIDFromETag(objectInfo.ETag)
	if err != nil {
		return "", err
	}
	objPath, err := lfs.WriteAddURLSentinelObject(lfsRoot, oid, objectInfo.ETag, input.objectURL)
	if err != nil {
		return "", err
	}
	if _, err := fmt.Fprintf(os.Stderr, "Added add-url sentinel object at %s\n", objPath); err != nil {
		return "", fmt.Errorf("stderr write: %w", err)
	}
	return oid, nil
}
