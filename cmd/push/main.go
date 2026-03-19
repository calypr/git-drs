package push

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/lfs"
	"github.com/spf13/cobra"
)

var runCommand = func(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

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
		myLogger := drslog.GetLogger()
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
		lfsFiles, err := lfs.GetAllLfsFiles(string(remote), "", []string{"HEAD"}, myLogger)
		if err != nil {
			return fmt.Errorf("failed to discover LFS files to push: %w", err)
		}

		ctx := context.Background()
		for _, file := range lfsFiles {
			if _, err := drsClient.RegisterFile(ctx, file.Oid, file.Name); err != nil {
				return fmt.Errorf("failed to register/upload %s (%s): %w", file.Name, file.Oid, err)
			}
		}

		out, err := runCommand("git", "push", string(remote))
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
