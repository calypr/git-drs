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
		myLogger := drslog.GetLogger()
		ctx := context.Background()
		cfg, err := config.LoadConfig()
		if err != nil {
			myLogger.DebugContext(ctx, "load config failed", "error", err)
			return err
		}

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
		drsClient, err := remoteruntime.New(cfg, remote, myLogger)
		if err != nil {
			myLogger.DebugContext(ctx, "create remote client failed", "error", err)
			return err
		}
		if drsClient.IsReadOnly() || !drsClient.CanUpload() || !drsClient.CanRegister() {
			return fmt.Errorf(
				"remote %q is read-only: git drs push cannot upload or register files\n"+
					"no files were uploaded, and you do not need to back out a commit that references existing Terra data\n"+
					"to publish the commit and its DRS references, use ordinary git push to a Git remote; this pushes only Git metadata and does not upload files to Terra",
				remote,
			)
		}

		drsClient.ForceUpload = pushForceUpload
		state, err := resolveSyncRefState(ctx, string(remote))
		if err != nil {
			return fmt.Errorf("failed to resolve pushed refs: %w", err)
		}
		exclusions := []string{}
		if state.AckOID != "" && !pushForceUpload {
			exclusions = append(exclusions, state.AckOID)
		}
		lfsFiles, err := lfs.PointerInventoryForObjects(ctx, []string{state.TargetOID}, exclusions)
		if err != nil {
			return fmt.Errorf("failed to discover LFS files to push: %w", err)
		}
		uniqueOIDs := countUniqueOIDs(lfsFiles)
		if state.AckOID == "" {
			fmt.Fprintf(os.Stdout, "DRS: no synchronization baseline for %s; bootstrapping full Git history and checking %d unique object(s)\n", state.RemoteRef, uniqueOIDs)
			fmt.Fprintln(os.Stdout, "DRS: this full metadata check is normally needed once per branch; it repeats only when the remote synchronization ref is missing or behind")
		} else {
			fmt.Fprintf(os.Stdout, "DRS: checking %d new reachable pointer(s) (%d unique object(s)) since synchronization %s\n", len(lfsFiles), uniqueOIDs, state.AckOID[:12])
		}
		progress := internaltransfer.NewUploadProgressRenderer(os.Stderr)
		syncSummary, err := internaltransfer.BatchSyncForPushWithSummary(drsClient, ctx, lfsFiles, progress)
		if err != nil {
			myLogger.DebugContext(ctx, "DRS push failed", "error", err)
			if finishErr := progress.Finish(); finishErr != nil {
				return fmt.Errorf("failed batch register/upload workflow: %w (progress finalize error: %v)", err, finishErr)
			}
			return fmt.Errorf("failed batch register/upload workflow: %w", err)
		}
		if err := progress.Finish(); err != nil {
			return fmt.Errorf("finalize upload progress: %w", err)
		}
		switch {
		case len(lfsFiles) == 0:
			fmt.Fprintln(os.Stdout, "DRS: no reachable LFS objects require synchronization")
		case !progress.HadUploads():
			fmt.Fprintln(os.Stdout, "DRS: no payload uploads required")
		}

		pushArgs := []string{"push"}
		if !pushWithHooks {
			pushArgs = append(pushArgs, "--no-verify")
		}
		pushArgs = append(pushArgs, string(remote), state.LocalRef+":"+state.RemoteRef)
		myLogger.DebugContext(ctx, "pushing Git ref", "remote", remote, "ref", state.RemoteRef, "oid", state.TargetOID)
		out, err := runCommand("git", pushArgs...)
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("git push failed for remote %q: %s", remote, msg)
		}
		if syncSummary.SkippedUnavailable > 0 {
			fmt.Fprintf(os.Stdout, "DRS: %d historical object(s) had no local payload and were skipped\n", syncSummary.SkippedUnavailable)
		}
		if err := pushSyncAcknowledgment(ctx, string(remote), state); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "DRS: synchronized %s at %s\n", state.RemoteRef, state.TargetOID)
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
