package addurl

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/lfss3"
	"github.com/spf13/cobra"

	"github.com/calypr/git-drs/s3_utils"
)

var (
	inspectS3ForLFS = lfss3.InspectS3ForLFS
	isLFSTracked    = lfss3.IsLFSTracked
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
		s3_utils.AWS_KEY_FLAG_NAME,
		os.Getenv(s3_utils.AWS_KEY_ENV_VAR),
		"AWS access key",
	)

	cmd.Flags().String(
		s3_utils.AWS_SECRET_FLAG_NAME,
		os.Getenv(s3_utils.AWS_SECRET_ENV_VAR),
		"AWS secret key",
	)

	cmd.Flags().String(
		s3_utils.AWS_REGION_FLAG_NAME,
		os.Getenv(s3_utils.AWS_REGION_ENV_VAR),
		"AWS S3 region",
	)

	cmd.Flags().String(
		s3_utils.AWS_ENDPOINT_URL_FLAG_NAME,
		os.Getenv(s3_utils.AWS_ENDPOINT_URL_ENV_VAR),
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
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	logger, err := drslog.NewLogger("", false)
	if err != nil {
		return fmt.Errorf("error creating logger: %v", err)
	}

	s3URL := args[0]

	// Determine path: use provided optional arg, otherwise derive from URL path
	var pathArg string
	if len(args) == 2 {
		pathArg = args[1]
	} else {
		u, perr := url.Parse(s3URL)
		if perr != nil {
			return perr
		}
		pathArg = strings.TrimPrefix(u.Path, "/")
	}

	sha256Param, ferr := cmd.Flags().GetString("sha256")
	if ferr != nil {
		return fmt.Errorf("read flag sha256: %w", ferr)
	}

	awsKey, ferr := cmd.Flags().GetString(s3_utils.AWS_KEY_FLAG_NAME)
	if ferr != nil {
		return fmt.Errorf("read flag %s: %w", s3_utils.AWS_KEY_FLAG_NAME, ferr)
	}
	awsSecret, ferr := cmd.Flags().GetString(s3_utils.AWS_SECRET_FLAG_NAME)
	if ferr != nil {
		return fmt.Errorf("read flag %s: %w", s3_utils.AWS_SECRET_FLAG_NAME, ferr)
	}
	awsRegion, ferr := cmd.Flags().GetString(s3_utils.AWS_REGION_FLAG_NAME)
	if ferr != nil {
		return fmt.Errorf("read flag %s: %w", s3_utils.AWS_REGION_FLAG_NAME, ferr)
	}
	awsEndpoint, ferr := cmd.Flags().GetString(s3_utils.AWS_ENDPOINT_URL_FLAG_NAME)
	if ferr != nil {
		return fmt.Errorf("read flag %s: %w", s3_utils.AWS_ENDPOINT_URL_FLAG_NAME, ferr)
	}

	if awsKey == "" || awsSecret == "" {
		return errors.New("AWS credentials must be provided via flags or environment variables")
	}
	if awsRegion == "" {
		return errors.New("AWS region must be provided via flag or environment variable")
	}

	s3Input := lfss3.S3ObjectParameters{
		S3URL:           s3URL,
		AWSAccessKey:    awsKey,
		AWSSecretKey:    awsSecret,
		AWSRegion:       awsRegion,
		AWSEndpoint:     awsEndpoint,
		SHA256:          sha256Param,
		DestinationPath: pathArg,
	}

	// 1) inspect S3 object, get metadata via HEAD
	s3Info, err := inspectS3ForLFS(ctx, s3Input)
	if err != nil {
		return err
	}

	// check if pathArg is already tracked by LFS
	isLFSTracked, err := isLFSTracked(pathArg)
	if err != nil {
		return fmt.Errorf("check LFS tracking for %s: %w", pathArg, err)
	}

	// get Git LFS directories
	gitCommonDir, lfsRoot, err := lfss3.GetGitRootDirectories(ctx)
	if err != nil {
		return fmt.Errorf("get git root directories: %w", err)
	}
	// print resolved info
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
		isLFSTracked,
		sha256Param,
	); err != nil {
		return fmt.Errorf("print resolved s3Info: %w", err)
	}

	if sha256Param == "" {

		computedSHA, tmpObj, err2 := download(ctx, s3Info, s3Input, lfsRoot)
		if err2 != nil {
			return err2
		}

		// 4) move tmp object to LFS storage
		oid := computedSHA
		dstDir := filepath.Join(lfsRoot, "objects", oid[0:2], oid[2:4])
		dstObj := filepath.Join(dstDir, oid)

		if err = os.MkdirAll(dstDir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dstDir, err)
		}

		if err = os.Rename(tmpObj, dstObj); err != nil {
			return fmt.Errorf("rename %s to %s: %w", tmpObj, dstObj, err)
		}

		if _, err := fmt.Fprintf(os.Stderr, "Added data file at %s\n", dstObj); err != nil {
			return fmt.Errorf("stderr write: %w", err)
		}
		sha256Param = computedSHA
	}

	oid := sha256Param
	// 5) write pointer file in working tree
	pointer := fmt.Sprintf(
		"version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n",
		oid, s3Info.SizeBytes,
	)
	// write pointer file to working tree pathArg
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
	if err = os.WriteFile(safePath, []byte(pointer), 0644); err != nil {
		return fmt.Errorf("write %s: %w", safePath, err)
	}

	if _, err := fmt.Fprintf(os.Stderr, "Added Git LFS pointer file at %s\n", safePath); err != nil {
		return fmt.Errorf("stderr write: %w", err)
	}
	if !isLFSTracked {
		// git lfs track
		_, err := lfss3.GitLFSTrackReadOnly(ctx, pathArg)
		if err != nil {
			return fmt.Errorf("git lfs track %s: %w", pathArg, err)
		}

		if _, err := fmt.Fprintf(os.Stderr, "Info: Added to Git LFS. Remember to `git add %s` and `git commit ...`", pathArg); err != nil {
			return fmt.Errorf("stderr write: %w", err)
		}
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("error getting config: %v", err)
	}

	remote, err := cfg.GetDefaultRemote()

	cli, err := cfg.GetRemoteClient(remote, logger)
	if err != nil {
		cwd, errCwd := os.Getwd()
		if errCwd != nil {
			_, _ = fmt.Fprintf(os.Stderr, "os.Getwd: %v\n", errCwd)
			os.Exit(1)
		}
		return fmt.Errorf("error GetRemoteClient: remote: %s cwd: %s err: %v", remote, cwd, err)
	}
	file := drsmap.LfsFileInfo{
		Name: pathArg,
		Size: s3Info.SizeBytes,
		Oid:  oid,
	}
	_, err = drsmap.WriteDrsFile(cli, file, cli.GetProjectId(), &s3URL)
	if err != nil {
		return fmt.Errorf("error WriteDrsFile: %v", err)
	}

	return nil
}

// download uses lfss3.AgentFetchReader to download the S3 object, returning
// the computed SHA256 and the path to the temporary downloaded file.
// The caller is responsible for moving/deleting the temporary file.
// we include this wrapper function to allow mocking in tests.
var download = func(ctx context.Context, info *lfss3.S3Object, input lfss3.S3ObjectParameters, lfsRoot string) (string, string, error) {
	return lfss3.Download(ctx, info, input, lfsRoot)
}
