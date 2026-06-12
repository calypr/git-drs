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
	"github.com/spf13/cobra"
)

var pushWithHooks bool
var pushForceUpload bool

var runCommand = func(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

var gitOutputFn = gitOutput

var Cmd = &cobra.Command{
	Use:   "push [remote-name]",
	Short: "Upload/register DRS objects and push Git refs",
	Long:  "Performs git-drs managed upload/register flow (multipart for large files) and then runs git push (without pre-push hooks by default).",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts at most 1 argument (remote name), received %d\n\nUsage: %s\n\nSee 'git drs push --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		myLogger := drslog.GetLogger()
		ctx := context.Background()
		cfg, err := config.LoadConfig()
		if err != nil {
			myLogger.Debug(fmt.Sprintf("Error loading config: %v", err))
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

		drsClient, err := cfg.GetRemoteClient(remote, myLogger)
		if err != nil {
			myLogger.Debug(fmt.Sprintf("Error creating DRS client: %s", err))
			return err
		}
		drsClient.ForceUpload = pushForceUpload
		pushRefs, err := currentPushRefUpdates(ctx)
		if err != nil {
			return fmt.Errorf("failed to resolve pushed refs: %w", err)
		}
		pushedPaths, err := listRefUpdatePaths(ctx, pushRefs)
		if err != nil {
			return fmt.Errorf("failed to resolve pushed paths: %w", err)
		}
		lfsFiles, err := lfs.GetLfsFilesForRefPaths("HEAD", pushedPaths, myLogger)
		if err != nil {
			return fmt.Errorf("failed to discover LFS files to push: %w", err)
		}

		if _, err := drsdelete.ReconcileCommittedDeletes(ctx, drsClient, pushRefs, myLogger); err != nil {
			return fmt.Errorf("failed to reconcile deletes: %w", err)
		}
		progress := newUploadProgressRenderer(os.Stderr)
		if err := pushsync.BatchSyncForPush(drsClient, ctx, lfsFiles, progress); err != nil {
			progress.Finish()
			return fmt.Errorf("failed batch register/upload workflow: %w", err)
		}
		progress.Finish()
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
		out, err := runCommand("git", pushArgs...)
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("git push failed for remote %q: %s", remote, msg)
		}
		return nil
	},
}

func init() {
	Cmd.Flags().BoolVar(&pushWithHooks, "with-hooks", false, "Run git push with local hooks enabled (invokes pre-push)")
	Cmd.Flags().BoolVar(&pushForceUpload, "force-upload", false, "Upload payload bytes even when a matching downloadable object already exists remotely")
}

func currentDeleteRefUpdates(ctx context.Context) ([]drsdelete.RefUpdate, error) {
	head, err := gitOutputFn(ctx, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	upstream, err := gitOutputFn(ctx, "rev-parse", "--verify", "@{upstream}")
	if err != nil {
		return nil, nil
	}
	return []drsdelete.RefUpdate{{
		OldSHA: upstream,
		NewSHA: head,
	}}, nil
}

func currentPushRefUpdates(ctx context.Context) ([]drsdelete.RefUpdate, error) {
	const zeroSHA = "0000000000000000000000000000000000000000"
	head, err := gitOutputFn(ctx, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	upstream, err := gitOutputFn(ctx, "rev-parse", "--verify", "@{upstream}")
	if err != nil {
		return []drsdelete.RefUpdate{{
			OldSHA: zeroSHA,
			NewSHA: head,
		}}, nil
	}
	return []drsdelete.RefUpdate{{
		OldSHA: upstream,
		NewSHA: head,
	}}, nil
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
