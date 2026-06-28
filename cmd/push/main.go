package push

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drsdelete"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/pushsync"
	"github.com/calypr/git-drs/internal/remoteruntime"
	"github.com/spf13/cobra"
)

var pushWithHooks bool
var pushForceUpload bool

var runCommand = func(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

var gitOutputFn = gitOutput
var getRemoteMergeBaseFn = getRemoteMergeBase

var Cmd = &cobra.Command{
	Use:   "push [remote-name]",
	Short: "Upload/register DRS objects and push Git refs",
	Long:  "Performs git-drs managed upload/register flow (multipart for large files) and then runs git push.",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts at most 1 argument (remote name), received %d\n\nUsage: %s\n\nSee 'git drs push --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(os.Stderr, "DEBUG: ENTERING RunE for push")
		myLogger := drslog.GetLogger()
		ctx := context.Background()
		fmt.Fprintln(os.Stderr, "DEBUG: Loading config...")
		cfg, err := config.LoadConfig()
		if err != nil {
			fmt.Fprintln(os.Stderr, "DEBUG: Failed to load config:", err)
			myLogger.Debug(fmt.Sprintf("Error loading config: %v", err))
			return err
		}
		fmt.Fprintln(os.Stderr, "DEBUG: Config loaded successfully")

		var remote config.Remote
		if len(args) > 0 {
			remote = config.Remote(args[0])
		} else {
			remote, err = cfg.GetDefaultRemote()
			if err != nil {
				myLogger.Debug(fmt.Sprintf("Error getting default remote: %v", err))
				return err
			}
		}

		fmt.Fprintln(os.Stderr, "DEBUG: Getting remote client for remote:", remote)
		drsClient, err := remoteruntime.New(cfg, remote, myLogger)
		if err != nil {
			fmt.Fprintln(os.Stderr, "DEBUG: Failed to get remote client:", err)
			myLogger.Debug(fmt.Sprintf("Error creating DRS client: %s", err))
			return err
		}
		fmt.Fprintln(os.Stderr, "DEBUG: Remote client retrieved. Resolving push refs...")
		drsClient.ForceUpload = pushForceUpload
		pushRefs, err := currentPushRefUpdates(ctx, string(remote))
		if err != nil {
			fmt.Fprintln(os.Stderr, "DEBUG: Failed to resolve push refs:", err)
			return fmt.Errorf("failed to resolve pushed refs: %w", err)
		}
		fmt.Fprintln(os.Stderr, "DEBUG: Push refs resolved. Resolving pushed paths...")
		pushedPaths, err := listRefUpdatePaths(ctx, pushRefs)
		if err != nil {
			fmt.Fprintln(os.Stderr, "DEBUG: Failed to resolve pushed paths:", err)
			return fmt.Errorf("failed to resolve pushed paths: %w", err)
		}
		fmt.Fprintln(os.Stderr, "DEBUG: Pushed paths resolved. Discovering LFS files...")
		lfsFiles, err := lfs.GetLfsFilesForRefPaths("HEAD", pushedPaths, myLogger)
		if err != nil {
			fmt.Fprintln(os.Stderr, "DEBUG: Failed to discover LFS files:", err)
			return fmt.Errorf("failed to discover LFS files to push: %w", err)
		}
		fmt.Fprintln(os.Stderr, "DEBUG: LFS files to push resolved. Total files:", len(lfsFiles))

		fmt.Fprintln(os.Stderr, "DEBUG: Reconciling committed deletes...")
		if _, err := drsdelete.ReconcileCommittedDeletes(ctx, drsClient, pushRefs, myLogger); err != nil {
			fmt.Fprintln(os.Stderr, "DEBUG: Failed to reconcile deletes:", err)
			return fmt.Errorf("failed to reconcile deletes: %w", err)
		}
		fmt.Fprintln(os.Stderr, "DEBUG: Deletes reconciled. Starting BatchSyncForPush...")
		progress := newUploadProgressRenderer(os.Stderr)
		if err := pushsync.BatchSyncForPush(drsClient, ctx, lfsFiles, progress); err != nil {
			fmt.Fprintln(os.Stderr, "DEBUG: BatchSyncForPush failed:", err)
			progress.Finish()
			return fmt.Errorf("failed batch register/upload workflow: %w", err)
		}
		progress.Finish()
		fmt.Fprintln(os.Stderr, "DEBUG: BatchSyncForPush completed successfully")
		switch {
		case len(lfsFiles) == 0:
			fmt.Fprintln(os.Stdout, "No git-drs tracked files found; pushing Git refs only.")
		case !progress.HadUploads():
			fmt.Fprintln(os.Stdout, "No DRS payload uploads needed; all tracked objects are already available remotely.")
		}

		pushArgs := []string{"push"}
		if !pushWithHooks {
			pushArgs = append(pushArgs, "--no-verify")
		}
		pushArgs = append(pushArgs, string(remote))
		fmt.Fprintln(os.Stderr, "DEBUG: Invoking git push with args:", pushArgs)
		out, err := runCommand("git", pushArgs...)
		if err != nil {
			fmt.Fprintln(os.Stderr, "DEBUG: Git push failed:", err)
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("git push failed for remote %q: %s", remote, msg)
		}
		fmt.Fprintln(os.Stderr, "DEBUG: Git push completed successfully")
		return nil
	},
}

func init() {
	Cmd.Flags().BoolVar(&pushWithHooks, "with-hooks", false, "Run git push with local hooks enabled")
	Cmd.Flags().BoolVar(&pushForceUpload, "force-upload", false, "Upload payload bytes even when a matching downloadable object already exists remotely")
}

func currentPushRefUpdates(ctx context.Context, remote string) ([]drsdelete.RefUpdate, error) {
	const zeroSHA = "0000000000000000000000000000000000000000"
	head, err := gitOutputFn(ctx, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	var oldSHA string
	upstream, err := gitOutputFn(ctx, "rev-parse", "--verify", "@{upstream}")
	if err == nil {
		oldSHA = upstream
	} else {
		mb, err := getRemoteMergeBaseFn(ctx, remote, head)
		if err == nil && mb != "" {
			oldSHA = mb
		} else {
			oldSHA = zeroSHA
		}
	}
	return []drsdelete.RefUpdate{{
		OldSHA: oldSHA,
		NewSHA: head,
	}}, nil
}

func getRemoteMergeBase(ctx context.Context, remote string, head string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "for-each-ref", "--format=%(refname)", "refs/remotes/"+remote+"/")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var refs []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasSuffix(line, "/HEAD") {
			refs = append(refs, line)
		}
	}
	if len(refs) == 0 {
		return "", nil
	}
	args := append([]string{"merge-base", head}, refs...)
	cmdMerge := exec.CommandContext(ctx, "git", args...)
	outMerge, err := cmdMerge.CombinedOutput()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(outMerge)), nil
}

func listRefUpdatePaths(ctx context.Context, refs []drsdelete.RefUpdate) ([]string, error) {
	const zeroSHA = "0000000000000000000000000000000000000000"
	set := make(map[string]struct{})
	for _, ref := range refs {
		newSHA := strings.TrimSpace(ref.NewSHA)
		oldSHA := strings.TrimSpace(ref.OldSHA)
		if newSHA == "" || newSHA == zeroSHA {
			continue
		}
		var args []string
		if oldSHA == "" || oldSHA == zeroSHA {
			args = []string{"ls-tree", "-r", "--name-only", newSHA}
		} else {
			args = []string{"diff", "--name-only", oldSHA, newSHA}
		}
		out, err := gitOutputFn(ctx, args...)
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
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
