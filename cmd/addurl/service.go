package addurl

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/git-drs/cloud"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/lfs"
	"github.com/spf13/cobra"
)

// AddURLService groups injectable dependencies used to implement the add-url
// behavior (logger factory, S3 inspection, LFS helpers, config loader, etc.).
type AddURLService struct {
	newLogger    func(string, bool) (*slog.Logger, error)
	inspectS3    func(ctx context.Context, input cloud.S3ObjectParameters) (*cloud.S3Object, error)
	isLFSTracked func(path string) (bool, error)
	getGitRoots  func(ctx context.Context) (string, string, error)
	gitLFSTrack  func(ctx context.Context, path string) (bool, error)
	loadConfig   func() (*config.Config, error)
	download     func(ctx context.Context, info *cloud.S3Object, input cloud.S3ObjectParameters, lfsRoot string) (string, string, error)
}

// NewAddURLService constructs an AddURLService populated with production
// implementations of its dependencies.
func NewAddURLService() *AddURLService {
	return &AddURLService{
		newLogger:    drslog.NewLogger,
		inspectS3:    cloud.InspectS3ForLFS,
		isLFSTracked: lfs.IsLFSTracked,
		getGitRoots:  lfs.GetGitRootDirectories,
		gitLFSTrack:  lfs.GitLFSTrackReadOnly,
		loadConfig:   config.LoadConfig,
		download:     download,
	}
}

// Run executes the add-url workflow: parse CLI input, inspect the S3 object,
// ensure the LFS object exists in local storage, write a Git LFS pointer file,
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

	input, err := parseAddURLInput(cmd, args)
	if err != nil {
		return err
	}

	s3Info, err := s.inspectS3(ctx, input.s3Params)
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

	if err := printResolvedInfo(cmd, gitCommonDir, lfsRoot, s3Info, input.path, isTracked, input.sha256); err != nil {
		return err
	}

	oid, err := s.ensureLFSObject(ctx, s3Info, input, lfsRoot)
	if err != nil {
		return err
	}

	if err := writePointerFile(input.path, oid, s3Info.SizeBytes); err != nil {
		return err
	}

	if err := updatePrecommitCache(ctx, logger, input.path, oid, input.s3URL); err != nil {
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

	builder := drs.NewObjectBuilder(remoteConfig.GetBucketName(), remoteConfig.GetProjectId())

	file := lfs.LfsFileInfo{
		Name: input.path,
		Size: s3Info.SizeBytes,
		Oid:  oid,
	}
	if _, err := drsmap.WriteDrsFile(builder, file, &input.s3URL); err != nil {
		return fmt.Errorf("error WriteDrsFile: %v", err)
	}

	return nil
}

// ensureLFSObject ensures the LFS object identified by s3Info exists in the
// repository's LFS storage. If the input includes an explicit SHA256 that is
// returned immediately; otherwise the object is downloaded into a temporary
// file and moved into the LFS `objects` storage, returning the object's oid.
func (s *AddURLService) ensureLFSObject(ctx context.Context, s3Info *cloud.S3Object, input addURLInput, lfsRoot string) (string, error) {
	if input.sha256 != "" {
		return input.sha256, nil
	}

	computedSHA, tmpObj, err := s.download(ctx, s3Info, input.s3Params, lfsRoot)
	if err != nil {
		return "", err
	}

	oid := computedSHA
	dstDir := filepath.Join(lfsRoot, "objects", oid[0:2], oid[2:4])
	dstObj := filepath.Join(dstDir, oid)

	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dstDir, err)
	}

	if err := os.Rename(tmpObj, dstObj); err != nil {
		return "", fmt.Errorf("rename %s to %s: %w", tmpObj, dstObj, err)
	}

	if _, err := fmt.Fprintf(os.Stderr, "Added data file at %s\n", dstObj); err != nil {
		return "", fmt.Errorf("stderr write: %w", err)
	}

	return computedSHA, nil
}
