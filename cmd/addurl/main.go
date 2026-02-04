package addurl

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/git-drs/cloud"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	drslfs "github.com/calypr/git-drs/drsmap/lfs"
	"github.com/calypr/git-drs/lfs"
	"github.com/calypr/git-drs/precommit_cache"
	"github.com/calypr/git-drs/utils"
	"github.com/spf13/cobra"
)

var Cmd = NewCommand()

// NewCommand constructs the Cobra command for the `add-url` subcommand,
// wiring usage, argument validation and the RunE handler.
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

// addFlags registers command-line flags for AWS credentials, endpoint and an
// optional `sha256` expected checksum.
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

// runAddURL is the Cobra RunE wrapper that delegates execution to the
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

	//// Add the pointer file to Git
	//if err := gitrepo.AddFile(filePath); err != nil {
	//	return fmt.Errorf("failed to add pointer file to Git: %w", err)
	//}

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

// updatePrecommitCache updates the project's pre-commit cache with a mapping
// from a repository-relative `pathArg` to the given LFS `oid` and records the
// external source URL. It will:
//   - require a non-nil `logger`
//   - open the pre-commit cache (`precommit_cache.Open`)
//   - ensure cache directories exist
//   - convert the supplied worktree path to a repository-relative path
//   - create or update the per-path JSON entry with the current OID and timestamp
//   - create or update the per-OID JSON entry listing paths that reference it,
//     the external URL, and a content-change flag when the path's OID changed
//   - remove the path from the previous OID entry when the content changed
//
// Parameters:
//   - ctx: context for operations that may be cancellable
//   - logger: a non-nil `*slog.Logger` used for warnings; if nil the function
//     returns an error
//   - pathArg: the worktree path to record (absolute or relative); must not be empty
//   - oid: the LFS object id (string) to associate with the path
//   - externalURL: optional external source URL for the object; empty string is allowed
//
// Returns an error if any cache operation, path resolution, or I/O fails.
func updatePrecommitCache(ctx context.Context, logger *slog.Logger, pathArg, oid, externalURL string) error {
	if logger == nil {
		return errors.New("logger is required")
	}
	// Open pre-commit cache. Returns a configured Cache or error.
	cache, err := precommit_cache.Open(ctx)
	if err != nil {
		return err
	}

	// Ensure cache directories exist.
	if err := ensureCacheDirs(cache, logger); err != nil {
		return err
	}

	// Convert worktree path to repository-relative path.
	relPath, err := repoRelativePath(pathArg)
	if err != nil {
		return err
	}

	// Current timestamp in RFC3339 format (UTC).
	now := time.Now().UTC().Format(time.RFC3339)

	// Read previous path entry, if any.
	pathFile := cachePathEntryFile(cache, relPath)
	prevEntry, prevExists, err := readPathEntry(pathFile)
	if err != nil {
		return err
	}
	// track whether content changed for this path
	contentChanged := prevExists && prevEntry.LFSOID != "" && prevEntry.LFSOID != oid

	if err := writeJSONAtomic(pathFile, precommit_cache.PathEntry{
		Path:      relPath,
		LFSOID:    oid,
		UpdatedAt: now,
	}); err != nil {
		return err
	}

	if err := upsertOIDEntry(cache, oid, relPath, externalURL, now, contentChanged); err != nil {
		return err
	}

	if contentChanged {
		_ = removePathFromOID(cache, prevEntry.LFSOID, relPath, now)
	}

	return nil
}

// ensureCacheDirs verifies and creates the pre-commit cache directory layout
// (paths and oids directories). It logs a warning when creating a missing
// cache root.
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
	if err := os.MkdirAll(cache.PathsDir, 0o755); err != nil {
		return fmt.Errorf("create cache paths dir: %w", err)
	}
	if err := os.MkdirAll(cache.OIDsDir, 0o755); err != nil {
		return fmt.Errorf("create cache oids dir: %w", err)
	}
	return nil
}

// repoRelativePath converts a worktree path (absolute or relative) to a
// repository-relative path. It resolves symlinks and ensures the path is
// contained within the repository root.
func repoRelativePath(pathArg string) (string, error) {
	if pathArg == "" {
		return "", errors.New("empty worktree path")
	}
	root, err := utils.GitTopLevel()
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

// cachePathEntryFile returns the filesystem path to the JSON path-entry file
// for the given repository-relative path within the provided Cache.
func cachePathEntryFile(cache *precommit_cache.Cache, path string) string {
	return filepath.Join(cache.PathsDir, precommit_cache.EncodePath(path)+".json")
}

// cacheOIDEntryFile returns the filesystem path to the JSON OID-entry file
// for the given LFS OID. The file is named by sha256(oid) to avoid filesystem
// restrictions and collisions.
func cacheOIDEntryFile(cache *precommit_cache.Cache, oid string) string {
	sum := sha256.Sum256([]byte(oid))
	return filepath.Join(cache.OIDsDir, fmt.Sprintf("%x.json", sum[:]))
}

// readPathEntry reads and parses a JSON PathEntry from disk. It returns the
// parsed entry, a boolean indicating existence, or an error on I/O/parse
// failure.
func readPathEntry(path string) (*precommit_cache.PathEntry, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var entry precommit_cache.PathEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false, err
	}
	return &entry, true, nil
}

// readOIDEntry reads and parses a JSON OIDEntry from disk. If the file is
// missing it returns a freshly initialized entry (with LFSOID set to the
// supplied oid and UpdatedAt set to now).
func readOIDEntry(path string, oid string, now string) (*precommit_cache.OIDEntry, error) {
	entry := &precommit_cache.OIDEntry{
		LFSOID:    oid,
		Paths:     []string{},
		UpdatedAt: now,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return entry, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, entry); err != nil {
		return nil, err
	}
	entry.LFSOID = oid
	return entry, nil
}

// upsertOIDEntry creates or updates the OID entry for `oid`, ensuring `path`
// is listed among its Paths, updating ExternalURL when provided, and setting
// content-change/state fields. The updated entry is written atomically.
func upsertOIDEntry(cache *precommit_cache.Cache, oid, path, externalURL, now string, contentChanged bool) error {
	if cache == nil {
		return errors.New("cache is nil")
	}
	oidFile := cacheOIDEntryFile(cache, oid)
	entry, err := readOIDEntry(oidFile, oid, now)
	if err != nil {
		return err
	}

	pathSet := make(map[string]struct{}, len(entry.Paths)+1)
	for _, p := range entry.Paths {
		pathSet[p] = struct{}{}
	}
	if path != "" {
		pathSet[path] = struct{}{}
	}
	entry.Paths = sortedKeys(pathSet)
	entry.UpdatedAt = now
	entry.ContentChange = entry.ContentChange || contentChanged
	if strings.TrimSpace(externalURL) != "" {
		entry.ExternalURL = externalURL
	}

	return writeJSONAtomic(oidFile, entry)
}

// removePathFromOID removes `path` from the OID entry for `oid` and writes
// the updated entry atomically. Missing entries are treated as empty.
// sortedKeys returns a sorted slice of keys from the provided string-set map.
func removePathFromOID(cache *precommit_cache.Cache, oid, path, now string) error {
	if cache == nil {
		return errors.New("cache is nil")
	}
	oidFile := cacheOIDEntryFile(cache, oid)
	entry, err := readOIDEntry(oidFile, oid, now)
	if err != nil {
		return err
	}
	pathSet := make(map[string]struct{}, len(entry.Paths))
	for _, p := range entry.Paths {
		if p == path {
			continue
		}
		pathSet[p] = struct{}{}
	}
	entry.Paths = sortedKeys(pathSet)
	entry.UpdatedAt = now

	return writeJSONAtomic(oidFile, entry)
}

// sortedKeys returns a sorted slice of keys from the provided string-set map.
func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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

// parseAddURLInput parses CLI args and flags into an addURLInput, validates
// required AWS credentials and region, and constructs cloud.S3ObjectParameters.
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

// resolvePathArg returns the explicit destination path argument when provided,
// otherwise derives the worktree path from the given S3 URL path component.
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
