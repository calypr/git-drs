package push

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/remoteruntime"
	internaltransfer "github.com/calypr/git-drs/internal/transfer"
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
var getReachablePointerFilesForRefFn = lfs.GetReachablePointerFilesForRef

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
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
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
		fmt.Fprintln(os.Stderr, "DEBUG: Push refs resolved. Discovering reachable LFS files...")
		lfsFiles, err := discoverLfsFilesForPush(pushRefs, myLogger)
		if err != nil {
			fmt.Fprintln(os.Stderr, "DEBUG: Failed to discover LFS files:", err)
			return fmt.Errorf("failed to discover LFS files to push: %w", err)
		}
		uniqueOIDs := countUniqueOIDs(lfsFiles)
		fmt.Fprintf(os.Stderr, "DEBUG: Reachable LFS files resolved. Total files: %d unique oids: %d\n", len(lfsFiles), uniqueOIDs)
		fmt.Fprintf(os.Stdout, "Discovered %d reachable DRS pointer file(s) (%d unique object(s)).\n", len(lfsFiles), uniqueOIDs)

		fmt.Fprintln(os.Stderr, "DEBUG: Reconciling committed deletes...")
		if _, err := internaltransfer.ReconcileCommittedDeletes(ctx, drsClient, pushRefs, myLogger); err != nil {
			fmt.Fprintln(os.Stderr, "DEBUG: Failed to reconcile deletes:", err)
			return fmt.Errorf("failed to reconcile deletes: %w", err)
		}
		fmt.Fprintln(os.Stderr, "DEBUG: Deletes reconciled. Starting BatchSyncForPush...")
		progress := internaltransfer.NewUploadProgressRenderer(os.Stderr)
		if err := internaltransfer.BatchSyncForPush(drsClient, ctx, lfsFiles, progress); err != nil {
			fmt.Fprintln(os.Stderr, "DEBUG: BatchSyncForPush failed:", err)
			if finishErr := progress.Finish(); finishErr != nil {
				return fmt.Errorf("failed batch register/upload workflow: %w (progress finalize error: %v)", err, finishErr)
			}
			return fmt.Errorf("failed batch register/upload workflow: %w", err)
		}
		if err := progress.Finish(); err != nil {
			return fmt.Errorf("finalize upload progress: %w", err)
		}
		fmt.Fprintln(os.Stderr, "DEBUG: BatchSyncForPush completed successfully")
		switch {
		case len(lfsFiles) == 0:
			fmt.Fprintln(os.Stdout, "No reachable DRS pointer files found; pushing Git refs only.")
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

func discoverLfsFilesForPush(refs []internaltransfer.RefUpdate, logger *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
	const zeroSHA = "0000000000000000000000000000000000000000"
	files := make(map[string]lfs.LfsFileInfo)
	seenRefs := make(map[string]struct{}, len(refs))
	for _, update := range refs {
		ref := strings.TrimSpace(update.NewSHA)
		if ref == "" || ref == zeroSHA {
			continue
		}
		if _, ok := seenRefs[ref]; ok {
			continue
		}
		seenRefs[ref] = struct{}{}
		refFiles, err := getReachablePointerFilesForRefFn(ref, logger)
		if err != nil {
			return nil, err
		}
		for path, info := range refFiles {
			files[path] = info
		}
	}
	return files, nil
}

func countUniqueOIDs(files map[string]lfs.LfsFileInfo) int {
	seen := make(map[string]struct{}, len(files))
	for _, info := range files {
		oid := strings.ToLower(strings.TrimSpace(info.Oid))
		if oid == "" {
			continue
		}
		seen[oid] = struct{}{}
	}
	return len(seen)
}

func currentPushRefUpdates(ctx context.Context, remote string) ([]internaltransfer.RefUpdate, error) {
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
	return []internaltransfer.RefUpdate{{
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

func gitOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
