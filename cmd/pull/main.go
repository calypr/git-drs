package pull

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/lfs"
	"github.com/spf13/cobra"
)

var runCommand = func(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

var Cmd = &cobra.Command{
	Use:   "pull [remote-name]",
	Short: "Pull using the standard Git + Git LFS flow",
	Long:  "Pull using the standard Git + Git LFS flow (git pull, git lfs pull, git lfs checkout).",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts at most 1 argument (remote name), received %d\n\nUsage: %s\n\nSee 'git drs pull --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logg := drslog.GetLogger()

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		var remote config.Remote
		if len(args) > 0 {
			remote = config.Remote(args[0])
		} else {
			remote, err = cfg.GetDefaultRemote()
			if err != nil {
				logg.Error(fmt.Sprintf("Error getting remote: %v", err))
				return err
			}
		}

		drsClient, err := cfg.GetRemoteClient(remote, logg)
		if err != nil {
			logg.Error(fmt.Sprintf("error creating DRS client: %s", err))
			return err
		}
		_ = drsClient // Remote validation only.

		if out, err := runCommand("git", "pull", string(remote)); err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("git pull failed for remote %q: %s", remote, msg)
		}

		out, err := runCommand("git", "lfs", "ls-files", "--json")
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("git lfs ls-files failed: %s", msg)
		}
		var parsed struct {
			Files []lfs.LfsFileInfo `json:"files"`
		}
		if err := lfsjsonUnmarshal(out, &parsed); err != nil {
			return fmt.Errorf("failed to parse git lfs ls-files output: %w", err)
		}

		ctx := context.Background()
		for _, f := range parsed.Files {
			if f.Downloaded {
				continue
			}
			dstPath, err := drsmap.GetObjectPath(common.LFS_OBJS_PATH, f.Oid)
			if err != nil {
				return fmt.Errorf("failed to resolve LFS object path for %s: %w", f.Oid, err)
			}
			if err := drsClient.DownloadFile(ctx, f.Oid, dstPath); err != nil {
				return fmt.Errorf("failed to download oid %s to %s: %w", f.Oid, dstPath, err)
			}
		}

		if out, err := runCommand("git", "lfs", "checkout"); err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			return fmt.Errorf("git lfs checkout failed: %s", msg)
		}

		return nil
	},
}

var lfsjsonUnmarshal = func(data []byte, v any) error {
	return sonic.ConfigFastest.Unmarshal(data, v)
}
