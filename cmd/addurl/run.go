package addurl

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/remoteruntime"
	sycloud "github.com/calypr/syfon/client/cloud"
	"github.com/spf13/cobra"
)

// AddURLService groups injectable dependencies used to implement the add-url
// behavior (logger factory, object inspection, LFS helpers, config loader, etc.).
type AddURLService struct {
	newLogger           func(string, bool) (*slog.Logger, error)
	inspectRemoteObject func(ctx context.Context, drsCtx *remoteruntime.GitContext, input addURLInput) (*inspectedObject, error)
	getRemoteClient     func(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*remoteruntime.GitContext, error)
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
		inspectRemoteObject: inspectRemoteObjectViaServer,
		getRemoteClient: func(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*remoteruntime.GitContext, error) {
			return remoteruntime.New(cfg, remote, logger)
		},
		isLFSTracked: lfs.IsLFSTracked,
		getGitRoots:  lfs.GetGitRootDirectories,
		gitLFSTrack:  gitrepo.TrackReadOnly,
		loadConfig:   config.LoadConfig,
	}
}

// Run executes the add-url workflow: parse CLI input, inspect the provider
// object through the configured Syfon remote, ensure the LFS object exists in
// local storage, write a pointer file, update the pre-commit cache
// (best-effort), optionally add a tracking entry, and record the DRS mapping.
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
	if input.recursive {
		return s.runRecursiveGlobus(ctx, cmd, logger, input)
	}
	if input.dryRun || input.manifest != "" {
		return fmt.Errorf("--dry-run and --manifest require --recursive")
	}

	cfg, err := s.loadConfig()
	if err != nil {
		return fmt.Errorf("error getting config: %v", err)
	}

	remote, err := cfg.GetRemoteOrDefault(input.remote)
	if err != nil {
		return err
	}

	remoteConfig := cfg.GetRemote(remote)
	if remoteConfig == nil {
		return fmt.Errorf("error getting remote configuration for %s", remote)
	}

	drsCtx, err := s.getRemoteClient(cfg, remote, logger)
	if err != nil {
		return err
	}

	org, project, scope, err := resolveTargetScope(remoteConfig)
	if err != nil {
		return err
	}

	if drsCtx != nil {
		org = firstNonEmpty(strings.TrimSpace(drsCtx.Organization), org)
		project = firstNonEmpty(strings.TrimSpace(drsCtx.ProjectId), project)
		scope = gitrepo.ResolvedBucketScope{
			Bucket: firstNonEmpty(strings.TrimSpace(drsCtx.BucketName), scope.Bucket),
			Prefix: firstNonEmpty(strings.TrimSpace(drsCtx.StoragePrefix), scope.Prefix),
		}
	}

	inspected, err := s.inspectRemoteObject(ctx, drsCtx, input)
	if err != nil {
		return err
	}
	input.objectURL = inspected.objectURL
	objectInfo := inspected.info

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

	builder := drsobjectBuilder(scope.Bucket, org, project, scope.Prefix)
	file := addURLDrsFile{
		Name:          input.path,
		Size:          objectInfo.SizeBytes,
		Oid:           oid,
		ContentSHA256: input.sha256,
	}
	if _, err := writeAddURLDrsObject(builder, file, input.objectURL); err != nil {
		return fmt.Errorf("write local DRS object: %w", err)
	}

	return nil
}

func (s *AddURLService) ensureLFSObject(_ context.Context, objectInfo *sycloud.ObjectInfo, input addURLInput, _ string) (string, error) {
	if input.sha256 != "" {
		return input.sha256, nil
	}

	return placeholderOIDForUnknownSHA(objectInfo.ETag, input.objectURL)
}
