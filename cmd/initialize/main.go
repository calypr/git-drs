package initialize

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/gitrepo"
	"github.com/spf13/cobra"
)

var transfers int

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize repo for git-drs",
	Long: "Description:" +
		"\n  Initialize repo for git-drs",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts no arguments, received %d\n\nUsage: %s\n\nSee 'git drs init --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logg := drslog.GetLogger()

		// check if .git dir exists to ensure you're in a git repository
		_, err := gitrepo.GitTopLevel()
		if err != nil {
			return fmt.Errorf("error: not in a git repository. Please run this command in the root of your git repository")
		}

		// create config file if it doesn't exist
		err = config.CreateEmptyConfig()
		if err != nil {
			return fmt.Errorf("error: unable to create config file: %v", err)
		}

		// load the config
		_, err = config.LoadConfig()
		if err != nil {
			logg.Debug(fmt.Sprintf("We should probably fix this: %v", err))
			return fmt.Errorf("error: unable to load config file: %v", err)
		}

		// setup lfs custom transfer
		// TODO: may need to generalize for anvil
		err = initGitConfig()
		if err != nil {
			return fmt.Errorf("error initializing custom transfer for DRS: %v", err)
		}

		// install pre-push hook
		err = installPrePushHook(logg)
		if err != nil {
			return fmt.Errorf("error installing pre-push hook: %v", err)
		}

		// final logs
		logg.Debug("Git DRS initialized")
		logg.Debug(fmt.Sprintf("Using %d concurrent transfers", transfers))
		return nil
	},
}

func initGitConfig() error {
	configs := map[string]string{
		"lfs.standalonetransferagent":                "drs",
		"lfs.customtransfer.drs.path":                "git-drs",
		"lfs.customtransfer.drs.args":                "transfer",
		"lfs.allowincompletepush":                    "false",
		"lfs.customtransfer.drs.concurrent":          strconv.FormatBool(transfers > 1),
		"lfs.customtransfer.drs.concurrenttransfers": strconv.Itoa(transfers),
	}

	if err := gitrepo.SetGitConfigOptions(configs); err != nil {
		return fmt.Errorf("unable to write git config: %w", err)
	}

	return nil
}

func init() {
	Cmd.Flags().IntVarP(&transfers, "transfers", "t", 4, "Number of concurrent transfers")
}

func installPrePushHook(logger *slog.Logger) error {
	hooksDir, err := gitrepo.GetGitHooksDir()
	if err != nil {
		return fmt.Errorf("unable to get hooks directory: %w", err)
	}

	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("unable to create hooks directory: %w", err)
	}

	hookPath := filepath.Join(hooksDir, "pre-push")
	hookBody := `
# . git/hooks/pre-push
remote="$1"
url="$2"

# Buffer stdin for both commands
TMPFILE="${TMPDIR:-/tmp}/git-drs-$$"
trap "rm -f $TMPFILE" EXIT
cat > "$TMPFILE"

# Run DRS preparation
git drs pre-push-prepare "$remote" "$url" < "$TMPFILE" || exit 1

# Run LFS push
exec git lfs pre-push "$remote" "$url" < "$TMPFILE"
`
	hookScript := "#!/bin/sh\n" + hookBody

	existingContent, err := os.ReadFile(hookPath)
	if err == nil {
		// there is an existing hook, rename it, and let the user know
		// Backup existing hook with timestamp
		timestamp := time.Now().Format("20060102T150405")
		backupPath := hookPath + "." + timestamp
		if err := os.WriteFile(backupPath, existingContent, 0644); err != nil {
			return fmt.Errorf("unable to back up existing pre-push hook: %w", err)
		}
		if err := os.Remove(hookPath); err != nil {
			return fmt.Errorf("unable to remove hook after backing up: %w", err)
		}
		logger.Debug(fmt.Sprintf("pre-push hook updated; backup written to %s", backupPath))
	}
	// If there was an error other than expected not existing, return it
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("unable to read pre-push hook: %w", err)
	}

	err = os.WriteFile(hookPath, []byte(hookScript), 0755)
	if err != nil {
		return fmt.Errorf("unable to write pre-push hook: %w", err)
	}
	logger.Debug("pre-push hook installed")
	return nil
}
