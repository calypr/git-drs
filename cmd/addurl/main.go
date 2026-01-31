package addurl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/cloud"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	drslfs "github.com/calypr/git-drs/drsmap/lfs"
	"github.com/calypr/git-drs/lfs"
	"github.com/spf13/cobra"
)

var Cmd = NewCommand()

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-url <s3-url> [path]",
		Short: "Add a file to the Git DRS repo using an S3 URL",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return errors.New("usage: add-url <s3-url> [path]")
			}
			return nil
		},
		RunE: runAddURL,
	}
	addFlags(cmd)
	return cmd
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().String(
		cloud.AWS_KEY_FLAG_NAME,
		os.Getenv(cloud.AWS_KEY_ENV_VAR),
		"AWS access key",
	)

	cmd.Flags().String(
		cloud.AWS_SECRET_FLAG_NAME,
		os.Getenv(cloud.AWS_SECRET_ENV_VAR),
		"AWS secret key",
	)

	cmd.Flags().String(
		cloud.AWS_REGION_FLAG_NAME,
		os.Getenv(cloud.AWS_REGION_ENV_VAR),
		"AWS S3 region",
	)

	cmd.Flags().String(
		cloud.AWS_ENDPOINT_URL_FLAG_NAME,
		os.Getenv(cloud.AWS_ENDPOINT_URL_ENV_VAR),
		"AWS S3 endpoint (optional, for Ceph/MinIO)",
	)

	// New flag: optional expected SHA256
	cmd.Flags().String(
		"sha256",
		"",
		"Expected SHA256 checksum (optional)",
	)
}

func runAddURL(cmd *cobra.Command, args []string) (err error) {
	return NewAddURLService().Run(cmd, args)
}

// download uses cloud.AgentFetchReader to download the S3 object, returning
// the computed SHA256 and the path to the temporary downloaded file.
// The caller is responsible for moving/deleting the temporary file.
// we include this wrapper function to allow mocking in tests.
var download = func(ctx context.Context, info *cloud.S3Object, input cloud.S3ObjectParameters, lfsRoot string) (string, string, error) {
	return cloud.Download(ctx, info, input, lfsRoot)
}

type AddURLService struct {
	newLogger    func(string, bool) (*slog.Logger, error)
	inspectS3    func(ctx context.Context, input cloud.S3ObjectParameters) (*cloud.S3Object, error)
	isLFSTracked func(path string) (bool, error)
	getGitRoots  func(ctx context.Context) (string, string, error)
	gitLFSTrack  func(ctx context.Context, path string) (bool, error)
	loadConfig   func() (*config.Config, error)
	download     func(ctx context.Context, info *cloud.S3Object, input cloud.S3ObjectParameters, lfsRoot string) (string, string, error)
}

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

func (s *AddURLService) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if _, err := s.newLogger("", false); err != nil {
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

	file := drslfs.LfsFileInfo{
		Name: input.path,
		Size: s3Info.SizeBytes,
		Oid:  oid,
	}
	if _, err := drsmap.WriteDrsFile(builder, file, &input.s3URL); err != nil {
		return fmt.Errorf("error WriteDrsFile: %v", err)
	}

	return nil
}

type addURLInput struct {
	s3URL    string
	path     string
	sha256   string
	s3Params cloud.S3ObjectParameters
}

func parseAddURLInput(cmd *cobra.Command, args []string) (addURLInput, error) {
	s3URL := args[0]

	pathArg, err := resolvePathArg(s3URL, args)
	if err != nil {
		return addURLInput{}, err
	}

	sha256Param, err := cmd.Flags().GetString("sha256")
	if err != nil {
		return addURLInput{}, fmt.Errorf("read flag sha256: %w", err)
	}

	awsKey, err := cmd.Flags().GetString(cloud.AWS_KEY_FLAG_NAME)
	if err != nil {
		return addURLInput{}, fmt.Errorf("read flag %s: %w", cloud.AWS_KEY_FLAG_NAME, err)
	}
	awsSecret, err := cmd.Flags().GetString(cloud.AWS_SECRET_FLAG_NAME)
	if err != nil {
		return addURLInput{}, fmt.Errorf("read flag %s: %w", cloud.AWS_SECRET_FLAG_NAME, err)
	}
	awsRegion, err := cmd.Flags().GetString(cloud.AWS_REGION_FLAG_NAME)
	if err != nil {
		return addURLInput{}, fmt.Errorf("read flag %s: %w", cloud.AWS_REGION_FLAG_NAME, err)
	}
	awsEndpoint, err := cmd.Flags().GetString(cloud.AWS_ENDPOINT_URL_FLAG_NAME)
	if err != nil {
		return addURLInput{}, fmt.Errorf("read flag %s: %w", cloud.AWS_ENDPOINT_URL_FLAG_NAME, err)
	}

	if awsKey == "" || awsSecret == "" {
		return addURLInput{}, errors.New("AWS credentials must be provided via flags or environment variables")
	}
	if awsRegion == "" {
		return addURLInput{}, errors.New("AWS region must be provided via flag or environment variable")
	}

	s3Input := cloud.S3ObjectParameters{
		S3URL:           s3URL,
		AWSAccessKey:    awsKey,
		AWSSecretKey:    awsSecret,
		AWSRegion:       awsRegion,
		AWSEndpoint:     awsEndpoint,
		SHA256:          sha256Param,
		DestinationPath: pathArg,
	}

	return addURLInput{
		s3URL:    s3URL,
		path:     pathArg,
		sha256:   sha256Param,
		s3Params: s3Input,
	}, nil
}

func resolvePathArg(s3URL string, args []string) (string, error) {
	if len(args) == 2 {
		return args[1], nil
	}
	u, err := url.Parse(s3URL)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(u.Path, "/"), nil
}

func printResolvedInfo(cmd *cobra.Command, gitCommonDir, lfsRoot string, s3Info *cloud.S3Object, pathArg string, isTracked bool, sha256 string) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), `
Resolved Git LFS s3Info
---------------------
Git common dir : %s
LFS storage    : %s

S3 object
---------
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
		s3Info.Bucket,
		s3Info.Key,
		s3Info.Path,
		s3Info.SizeBytes,
		s3Info.MetaSHA256,
		s3Info.ETag,
		s3Info.LastModTime.Format("2006-01-02T15:04:05Z07:00"),
		pathArg,
		isTracked,
		sha256,
	); err != nil {
		return fmt.Errorf("print resolved s3Info: %w", err)
	}
	return nil
}

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

func writePointerFile(pathArg, oid string, sizeBytes int64) error {
	pointer := fmt.Sprintf(
		"version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n",
		oid, sizeBytes,
	)
	if pathArg == "" {
		return fmt.Errorf("empty worktree path")
	}
	safePath := filepath.Clean(pathArg)
	dir := filepath.Dir(safePath)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(safePath, []byte(pointer), 0644); err != nil {
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
