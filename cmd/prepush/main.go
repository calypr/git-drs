package prepush

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/lfs"
	"github.com/calypr/git-drs/precommit_cache"
	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "pre-push-prepare",
	Short: "pre-push hook to update DRS objects",
	Long:  "Pre-push hook that updates DRS objects before transfer",
	Args:  cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return NewPrePushService().Run(args, os.Stdin)
	},
}

type PrePushService struct {
	newLogger        func(string, bool) (*slog.Logger, error)
	loadConfig       func() (*config.Config, error)
	updateDrsObjects func(drs.ObjectBuilder, map[string]lfs.LfsFileInfo, drsmap.UpdateOptions) error
	createTempFile   func(dir, pattern string) (*os.File, error)
}

func NewPrePushService() *PrePushService {
	return &PrePushService{
		newLogger:        drslog.NewLogger,
		loadConfig:       config.LoadConfig,
		updateDrsObjects: drsmap.UpdateDrsObjectsWithFiles,
		createTempFile:   os.CreateTemp,
	}
}

func (s *PrePushService) Run(args []string, stdin io.Reader) error {
	ctx := context.Background()
	myLogger, err := s.newLogger("", false)
	if err != nil {
		return fmt.Errorf("error creating logger: %v", err)
	}

	myLogger.Info("~~~~~~~~~~~~~ START: pre-push ~~~~~~~~~~~~~")

	cfg, err := s.loadConfig()
	if err != nil {
		return fmt.Errorf("error getting config: %v", err)
	}

	gitRemoteName, gitRemoteLocation := parseRemoteArgs(args)
	myLogger.Debug(fmt.Sprintf("git remote name: %s, git remote location: %s", gitRemoteName, gitRemoteLocation))

	remote, err := cfg.GetDefaultRemote()
	if err != nil {
		myLogger.Debug(fmt.Sprintf("Warning. Error getting default remote: %v", err))
		fmt.Fprintln(os.Stderr, "Warning. Skipping DRS preparation. Error getting default remote:", err)
		return nil
	}

	// get the remote client
	cli, err := cfg.GetRemoteClient(remote, myLogger)
	if err != nil {
		// Print warning to stderr and return success (exit 0)
		fmt.Fprintln(os.Stderr, "Warning. Skipping DRS preparation. Error getting remote client:", err)
		myLogger.Debug(fmt.Sprintf("Warning. Skipping DRS preparation. Error getting remote client: %v", err))
		// Check for GitDrsIdxdClient
	}
	dc, ok := cli.(*indexd.GitDrsIdxdClient)
	if !ok {
		return fmt.Errorf("cli is not IndexdClient: %T", cli)
	}
	myLogger.Debug(fmt.Sprintf("Current server: %s", dc.Config.ProjectId))
	remoteConfig := cfg.GetRemote(remote)
	if remoteConfig == nil {
		fmt.Fprintln(os.Stderr, "Warning. Skipping DRS preparation. Error getting remote configuration.")
		myLogger.Debug("Warning. Skipping DRS preparation. Error getting remote configuration.")
		return nil
	}

	builder := drs.NewObjectBuilder(remoteConfig.GetBucketName(), remoteConfig.GetProjectId())
	myLogger.Debug(fmt.Sprintf("Current server project: %s", builder.ProjectID))

	tmp, err := bufferStdin(stdin, s.createTempFile)
	if err != nil {
		myLogger.Error(fmt.Sprintf("error buffering stdin: %v", err))
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	refs, err := readPushedRefs(tmp)
	if err != nil {
		myLogger.Error(fmt.Sprintf("error reading pushed refs: %v", err))
		return err
	}
	branches := branchesFromRefs(refs)

	cache, cacheReady := openCache(ctx, myLogger)
	lfsFiles, usedCache, err := collectLfsFiles(ctx, cache, cacheReady, gitRemoteName, gitRemoteLocation, branches, refs, myLogger)
	if err != nil {
		myLogger.Error(fmt.Sprintf("error collecting LFS files: %v", err))
		return err
	}

	myLogger.Debug(fmt.Sprintf("Preparing DRS objects for push branches: %v (cache=%v)", branches, usedCache))
	err = s.updateDrsObjects(builder, lfsFiles, drsmap.UpdateOptions{
		Cache:          cache,
		PreferCacheURL: usedCache,
		Logger:         myLogger,
	})
	if err != nil {
		myLogger.Error(fmt.Sprintf("UpdateDrsObjects failed: %v", err))
		return err
	}
	myLogger.Info("~~~~~~~~~~~~~ COMPLETED: pre-push ~~~~~~~~~~~~~")
	return nil
}

func parseRemoteArgs(args []string) (string, string) {
	var gitRemoteName, gitRemoteLocation string
	if len(args) >= 1 {
		gitRemoteName = args[0]
	}
	if len(args) >= 2 {
		gitRemoteLocation = args[1]
	}
	if gitRemoteName == "" {
		gitRemoteName = "origin"
	}
	return gitRemoteName, gitRemoteLocation
}

type pushedRef struct {
	LocalRef  string
	LocalSHA  string
	RemoteRef string
	RemoteSHA string
}

func bufferStdin(stdin io.Reader, createTempFile func(dir, pattern string) (*os.File, error)) (*os.File, error) {
	tmp, err := createTempFile("", "prepush-stdin-*")
	if err != nil {
		return nil, fmt.Errorf("error creating temp file for stdin: %w", err)
	}

	if _, err := io.Copy(tmp, stdin); err != nil {
		return nil, fmt.Errorf("error buffering stdin: %w", err)
	}

	if _, err := tmp.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("error seeking temp stdin: %w", err)
	}
	return tmp, nil
}

// readPushedBranches reads git push lines from the provided temp file,
// extracts unique local branch names for refs under `refs/heads/` and
// returns them sorted. The file is rewound to the start before returning.
func readPushedRefs(f io.ReadSeeker) ([]pushedRef, error) {
	// Ensure we read from start
	// example:
	// refs/heads/main 67890abcdef1234567890abcdef1234567890abcd refs/heads/main 12345abcdef67890abcdef1234567890abcdef12
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(f)
	refs := make([]pushedRef, 0)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		refs = append(refs, pushedRef{
			LocalRef:  fields[0],
			LocalSHA:  fields[1],
			RemoteRef: fields[2],
			RemoteSHA: fields[3],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	// Rewind so caller can reuse the file
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	return refs, nil
}

func branchesFromRefs(refs []pushedRef) []string {
	const prefix = "refs/heads/"
	set := make(map[string]struct{})
	for _, ref := range refs {
		if strings.HasPrefix(ref.LocalRef, prefix) {
			branch := strings.TrimPrefix(ref.LocalRef, prefix)
			if branch != "" {
				set[branch] = struct{}{}
			}
		}
	}
	branches := make([]string, 0, len(set))
	for b := range set {
		branches = append(branches, b)
	}
	sort.Strings(branches)
	return branches
}

func openCache(ctx context.Context, logger *slog.Logger) (*precommit_cache.Cache, bool) {
	cache, err := precommit_cache.Open(ctx)
	if err != nil {
		logger.Debug(fmt.Sprintf("pre-commit cache unavailable: %v", err))
		return nil, false
	}
	if _, err := os.Stat(cache.Root); err != nil {
		if os.IsNotExist(err) {
			logger.Debug("pre-commit cache missing; continuing without cache")
		} else {
			logger.Debug(fmt.Sprintf("pre-commit cache access error: %v", err))
		}
		return nil, false
	}
	return cache, true
}

func collectLfsFiles(ctx context.Context, cache *precommit_cache.Cache, cacheReady bool, gitRemoteName, gitRemoteLocation string, branches []string, refs []pushedRef, logger *slog.Logger) (map[string]lfs.LfsFileInfo, bool, error) {
	if cacheReady {
		lfsFiles, ok, err := lfsFilesFromCache(ctx, cache, refs, logger)
		if err != nil {
			logger.Debug(fmt.Sprintf("pre-commit cache read failed: %v", err))
		} else if ok {
			return lfsFiles, true, nil
		}
		logger.Debug("pre-commit cache incomplete or stale; falling back to LFS discovery")
	}
	lfsFiles, err := lfs.GetAllLfsFiles(gitRemoteName, gitRemoteLocation, branches, logger)
	if err != nil {
		return nil, false, err
	}
	return lfsFiles, false, nil
}

const cacheMaxAge = 24 * time.Hour

func lfsFilesFromCache(ctx context.Context, cache *precommit_cache.Cache, refs []pushedRef, logger *slog.Logger) (map[string]lfs.LfsFileInfo, bool, error) {
	if cache == nil {
		return nil, false, nil
	}
	paths, err := listPushedPaths(ctx, refs)
	if err != nil {
		return nil, false, err
	}
	lfsFiles := make(map[string]lfs.LfsFileInfo, len(paths))
	for _, path := range paths {
		entry, ok, err := cache.ReadPathEntry(path)
		if err != nil {
			return nil, false, err
		}
		if !ok || entry.LFSOID == "" {
			return nil, false, nil
		}
		if entry.UpdatedAt == "" || precommit_cache.StaleAfter(entry.UpdatedAt, cacheMaxAge) {
			return nil, false, nil
		}
		stat, err := os.Stat(path)
		if err != nil {
			logger.Debug(fmt.Sprintf("cache path stat failed for %s: %v", path, err))
			return nil, false, nil
		}
		lfsFiles[path] = lfs.LfsFileInfo{
			Name:    path,
			Size:    stat.Size(),
			OidType: "sha256",
			Oid:     entry.LFSOID,
			Version: "https://git-lfs.github.com/spec/v1",
		}
	}
	return lfsFiles, true, nil
}

func listPushedPaths(ctx context.Context, refs []pushedRef) ([]string, error) {
	const zeroSHA = "0000000000000000000000000000000000000000"
	set := make(map[string]struct{})
	for _, ref := range refs {
		if ref.LocalSHA == "" || ref.LocalSHA == zeroSHA {
			continue
		}
		var args []string
		if ref.RemoteSHA == "" || ref.RemoteSHA == zeroSHA {
			args = []string{"ls-tree", "-r", "--name-only", ref.LocalSHA}
		} else {
			args = []string{"diff", "--name-only", ref.RemoteSHA, ref.LocalSHA}
		}
		out, err := gitOutput(ctx, args...)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			set[line] = struct{}{}
		}
	}
	paths := make([]string, 0, len(set))
	for path := range set {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func gitOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// readPushedBranches reads git push lines from the provided temp file,
// extracts unique local branch names for refs under `refs/heads/` and
// returns them sorted. The file is rewound to the start before returning.
func readPushedBranches(f *os.File) ([]string, error) {
	// Ensure we read from start
	// example:
	// refs/heads/main 67890abcdef1234567890abcdef1234567890abcd refs/heads/main 12345abcdef67890abcdef1234567890abcdef12
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(f)
	set := make(map[string]struct{})
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		localRef := fields[0]
		const prefix = "refs/heads/"
		if strings.HasPrefix(localRef, prefix) {
			branch := strings.TrimPrefix(localRef, prefix)
			if branch != "" {
				set[branch] = struct{}{}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	branches := make([]string, 0, len(set))
	for b := range set {
		branches = append(branches, b)
	}
	sort.Strings(branches)
	// Rewind so caller can reuse the file
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	return branches, nil
}
