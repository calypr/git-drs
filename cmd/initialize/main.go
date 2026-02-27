package initialize

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/gitrepo"
	"github.com/spf13/cobra"
)

var (
	transfers            int
	upsert               bool
	multiPartThreshold   int
	enableDataClientLogs bool
)

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
		if initialized, _ := IsRepoInitialized(); initialized {
			logg.Debug("Git DRS already initialized, skipping...")
			return nil
		}
		_, err := InitializeRepo(logg, transfers, upsert, multiPartThreshold, enableDataClientLogs)
		return err
	},
}

// IsRepoInitialized checks if the repository has already been configured for git-drs
func IsRepoInitialized() (bool, error) {
	// 1. Check if LFS transfer agent is set to drs
	agent, err := gitrepo.GetGitConfigString("lfs.standalonetransferagent")
	if err != nil || agent != "drs" {
		return false, nil
	}

	// 2. Check for git hooks directory
	hooksDir, err := gitrepo.GetGitHooksDir()
	if err != nil {
		return false, nil
	}

	// 3. Check for pre-push hook containing git-drs
	prePushPath := filepath.Join(hooksDir, "pre-push")
	contentPush, err := os.ReadFile(prePushPath)
	if err != nil || !strings.Contains(string(contentPush), "git drs pre-push-prepare") {
		return false, nil
	}

	// 4. Check for pre-commit hook containing git-drs
	preCommitPath := filepath.Join(hooksDir, "pre-commit")
	contentCommit, err := os.ReadFile(preCommitPath)
	if err != nil || !strings.Contains(string(contentCommit), "git drs precommit") {
		return false, nil
	}

	// 5. Check if DRS directory exists (.git/drs)
	drsDir, err := gitrepo.DrsTopLevel()
	if err != nil {
		return false, nil
	}
	if _, err := os.Stat(drsDir); os.IsNotExist(err) {
		return false, nil
	}

	return true, nil
}

// InitializeRepo performs the full repository initialization for git-drs.
// Returns (true, nil) if initialization was performed, (false, nil) if already initialized.
func InitializeRepo(logger *slog.Logger, transfers int, upsert bool, multiPartThreshold int, enableDataClientLogs bool) (bool, error) {
	// check if already initialized to avoid redundant work
	initialized, err := IsRepoInitialized()
	if err != nil {
		return false, err
	}
	if initialized {
		return false, nil
	}

	// Ensure DRS directory (.git/drs) exists
	drsDir, err := gitrepo.DrsTopLevel()
	if err != nil {
		return false, fmt.Errorf("error: not in a git repository. Please run this command in the root of your git repository")
	}
	if err := os.MkdirAll(drsDir, 0755); err != nil {
		return false, fmt.Errorf("error: unable to create DRS directory: %v", err)
	}

	// create config file if it doesn't exist
	err = config.CreateEmptyConfig()
	if err != nil {
		return false, fmt.Errorf("error: unable to create config file: %v", err)
	}

	// load the config
	_, err = config.LoadConfig()
	if err != nil {
		logger.Debug(fmt.Sprintf("We should probably fix this: %v", err))
		return false, fmt.Errorf("error: unable to load config file: %v", err)
	}

	err = gitrepo.InitializeLfsConfig(transfers, upsert, multiPartThreshold, enableDataClientLogs)
	if err != nil {
		return false, fmt.Errorf("error initializing custom transfer for DRS: %v", err)
	}

	// install pre-push hook
	err = installPrePushHook(logger)
	if err != nil {
		return false, fmt.Errorf("error installing pre-push hook: %v", err)
	}
	// install pre-commit hook
	err = installPreCommitHook(logger)
	if err != nil {
		return false, fmt.Errorf("error installing pre-commit hook: %v", err)
	}

	// final logs
	logger.Debug("Git DRS initialized")
	logger.Debug(fmt.Sprintf("Using %d concurrent transfers", transfers))
	return true, nil
}

func init() {
	Cmd.Flags().IntVarP(&transfers, "transfers", "t", 1, "Number of concurrent transfers")
	Cmd.Flags().BoolVarP(&upsert, "upsert", "u", false, "Enable upsert for indexd records")
	Cmd.Flags().IntVarP(&multiPartThreshold, "multipart-threshold", "m", 500, "Multipart threshold in MB")
	Cmd.Flags().BoolVar(&enableDataClientLogs, "enable-data-client-logs", false, "Enable data-client internal logs")
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

func installPreCommitHook(logger *slog.Logger) error {
	hooksDir, err := gitrepo.GetGitHooksDir()
	if err != nil {
		return fmt.Errorf("unable to get hooks directory: %w", err)
	}

	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("unable to create hooks directory: %w", err)
	}

	hookPath := filepath.Join(hooksDir, "pre-commit")
	hookBody := `
# .git/hooks/pre-commit
exec git drs precommit
`
	hookScript := "#!/bin/sh\n" + hookBody

	existingContent, err := os.ReadFile(hookPath)
	if err == nil {
		// there is an existing hook, rename it, and let the user know
		// Backup existing hook with timestamp
		timestamp := time.Now().Format("20060102T150405")
		backupPath := hookPath + "." + timestamp
		if err := os.WriteFile(backupPath, existingContent, 0644); err != nil {
			return fmt.Errorf("unable to back up existing pre-commit hook: %w", err)
		}
		if err := os.Remove(hookPath); err != nil {
			return fmt.Errorf("unable to remove hook after backing up: %w", err)
		}
		logger.Debug(fmt.Sprintf("pre-commit hook updated; backup written to %s", backupPath))
	}
	// If there was an error other than expected not existing, return it
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("unable to read pre-commit hook: %w", err)
	}

	err = os.WriteFile(hookPath, []byte(hookScript), 0755)
	if err != nil {
		return fmt.Errorf("unable to write pre-commit hook: %w", err)
	}
	logger.Debug("pre-commit hook installed")
	return nil
}
