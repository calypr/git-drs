package addurl

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/calypr/git-drs/cmd/addurl/lfss3"
	"github.com/calypr/git-drs/s3_utils"
)

var Cmd = &cobra.Command{
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

func init() {
	Cmd.Flags().String(
		s3_utils.AWS_KEY_FLAG_NAME,
		os.Getenv(s3_utils.AWS_KEY_ENV_VAR),
		"AWS access key",
	)

	Cmd.Flags().String(
		s3_utils.AWS_SECRET_FLAG_NAME,
		os.Getenv(s3_utils.AWS_SECRET_ENV_VAR),
		"AWS secret key",
	)

	Cmd.Flags().String(
		s3_utils.AWS_REGION_FLAG_NAME,
		os.Getenv(s3_utils.AWS_REGION_ENV_VAR),
		"AWS S3 region",
	)

	Cmd.Flags().String(
		s3_utils.AWS_ENDPOINT_URL_FLAG_NAME,
		os.Getenv(s3_utils.AWS_ENDPOINT_URL_ENV_VAR),
		"AWS S3 endpoint (optional, for Ceph/MinIO)",
	)

	// New flag: optional expected SHA256
	Cmd.Flags().String(
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

	s3Input := lfss3.InspectInput{
		S3URL:        s3URL,
		AWSAccessKey: awsKey,
		AWSSecretKey: awsSecret,
		AWSRegion:    awsRegion,
		AWSEndpoint:  awsEndpoint,
		SHA256:       sha256Param,
		WorktreeName: pathArg,
	}
	info, err := lfss3.InspectS3ForLFS(ctx, s3Input)
	if err != nil {
		return err
	}

	isLFSTracked, err := lfss3.IsLFSTracked(pathArg)
	if err != nil {
		return fmt.Errorf("check LFS tracking for %s: %w", pathArg, err)
	}

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), `
Resolved Git LFS info
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

`,
		info.GitCommonDir,
		info.LFSRoot,
		info.Bucket,
		info.Key,
		info.WorktreeName,
		info.SizeBytes,
		info.MetaSHA256,
		info.ETag,
		info.LastModTime.Format("2006-01-02T15:04:05Z07:00"),
		pathArg,
		isLFSTracked,
	); err != nil {
		return fmt.Errorf("print resolved info: %w", err)
	}

	// 2) object destination
	etag := info.ETag
	subdir1, subdir2 := "xx", "yy"
	if len(etag) >= 4 {
		subdir1 = etag[0:2]
		subdir2 = etag[2:4]
	}
	objName := etag
	if objName == "" {
		objName = "unknown-etag"
	}
	tmpDir := filepath.Join(info.LFSRoot, "tmp-objects", subdir1, subdir2)
	tmpObj := filepath.Join(tmpDir, objName)

	// 3) fetch bytes -> tmp, compute sha+count

	// replace the pseudocode with this real Go snippet (to be placed inside runAddURL)
	if err = os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", tmpDir, err)
	}

	f, err := os.Create(tmpObj)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmpObj, err)
	}
	// ensure any leftover file is closed and error propagated via named return
	defer func() {
		if f != nil {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("close tmp file: %w", cerr)
			}
		}
	}()

	h := sha256.New()

	var reader io.ReadCloser
	reader, err = lfss3.AgentFetchReader(ctx, s3Input)
	if err != nil {
		return fmt.Errorf("fetch reader: %w", err)
	}
	// ensure close on any early return; propagate close error via named return
	defer func() {
		if reader != nil {
			if cerr := reader.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("close reader: %w", cerr)
			}
		}
	}()

	n, err := io.Copy(io.MultiWriter(f, h), reader)
	if err != nil {
		return fmt.Errorf("copy bytes to %s: %w", tmpObj, err)
	}

	// explicitly close reader and handle error
	if cerr := reader.Close(); cerr != nil {
		return fmt.Errorf("close reader: %w", cerr)
	}
	reader = nil

	// ensure data is flushed to disk
	if err = f.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", tmpObj, err)
	}

	// explicitly close tmp file before rename
	if cerr := f.Close(); cerr != nil {
		return fmt.Errorf("close %s: %w", tmpObj, cerr)
	}
	f = nil

	// use n (bytes written) to avoid unused var warnings
	_ = n

	// compute hex SHA256 of the fetched content
	computedSHA := fmt.Sprintf("%x", h.Sum(nil))
	//optional: compare with provided `sha256` flag if desired
	if sha256Param != "" && sha256Param != computedSHA {
		return fmt.Errorf("sha256Param mismatch: expected %s got %s", sha256Param, computedSHA)
	}

	oid := computedSHA
	dstDir := filepath.Join(info.LFSRoot, "objects", oid[0:2], oid[2:4])
	dstObj := filepath.Join(dstDir, oid)

	if err = os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dstDir, err)
	}

	if err = os.Rename(tmpObj, dstObj); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpObj, dstObj, err)
	}

	// 5) write pointer file in working tree
	pointer := fmt.Sprintf(
		"version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n",
		oid, info.SizeBytes,
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

	if _, err := fmt.Fprintf(os.Stderr, "Added data file at %s\n", dstObj); err != nil {
		return fmt.Errorf("stderr write: %w", err)
	}
	if _, err := fmt.Fprintf(os.Stderr, "Added Git LFS pointer file at %s\n", safePath); err != nil {
		return fmt.Errorf("stderr write: %w", err)
	}
	return nil
}
