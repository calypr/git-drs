package addurl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/calypr/git-drs/cloud"
	"github.com/spf13/cobra"
)

// writePointerFile writes a Git LFS pointer file at the given worktree path
// referencing the supplied oid and recording sizeBytes. It creates parent
// directories as needed and validates the path is non-empty.
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

// maybeTrackLFS ensures the supplied path is tracked by Git LFS by invoking
// the provided gitLFSTrack callback when the path is not already tracked.
// It reports the addition to stderr for user guidance.
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

// printResolvedInfo writes a human-readable summary of resolved Git/LFS and
// S3 object information to the command's stdout for user confirmation.
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

// writeJSONAtomic marshals `value` to JSON and writes it to `path` atomically
// by writing to a temporary file in the same directory and renaming it. It
// ensures parent directories exist.
func writeJSONAtomic(path string, value any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
