package initialize

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/spf13/cobra"
)

var (
	transfers            = 1
	upsert               bool
	multiPartThreshold   = 5120
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
		if err := InitializeRepo(logg); err != nil {
			return err
		}
		logg.Debug(fmt.Sprintf("Using %d concurrent transfers", transfers))
		return nil
	},
}

// InitializeRepo applies git-drs repository-local setup to the current git repository.
// It is safe to call repeatedly.
func InitializeRepo(logg *slog.Logger) error {
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

	// create drs directories
	drsDir := common.DRS_DIR
	drsLfsObjsDir := common.DRS_OBJS_PATH
	if err := os.MkdirAll(drsDir, 0755); err != nil {
		return fmt.Errorf("error: unable to create drs directory: %v", err)
	}
	if err := os.MkdirAll(drsLfsObjsDir, 0755); err != nil {
		return fmt.Errorf("error: unable to create drs lfs objects directory: %v", err)
	}

	err = initGitConfig()
	if err != nil {
		return fmt.Errorf("error initializing git-drs repository config: %v", err)
	}

	// install pre-push hook
	err = installPrePushHook(logg)
	if err != nil {
		return fmt.Errorf("error installing pre-push hook: %v", err)
	}
	// install pre-commit hook
	err = installPreCommitHook(logg)
	if err != nil {
		return fmt.Errorf("error installing pre-commit hook: %v", err)
	}

	logg.Debug("Git DRS initialized")
	return nil
}

// EnsureInitialized applies initialization only when the repository does not
// already appear to have git-drs local setup installed.
func EnsureInitialized(logg *slog.Logger) error {
	initialized, err := isInitialized()
	if err != nil {
		return err
	}
	if initialized {
		return nil
	}
	return InitializeRepo(logg)
}

func isInitialized() (bool, error) {
	if _, err := gitrepo.GitTopLevel(); err != nil {
		return false, fmt.Errorf("error: not in a git repository. Please run this command in the root of your git repository")
	}

	if _, err := os.Stat(common.DRS_DIR); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("error checking git-drs directory: %v", err)
	}

	if val, err := gitrepo.GetGitConfigString("filter.drs.process"); err != nil || strings.TrimSpace(val) != "git-drs filter" {
		return false, err
	}
	if val, err := gitrepo.GetGitConfigString("filter.drs.clean"); err != nil || strings.TrimSpace(val) != "git-drs clean -- %f" {
		return false, err
	}
	if val, err := gitrepo.GetGitConfigString("filter.drs.smudge"); err != nil || strings.TrimSpace(val) != "git-drs smudge -- %f" {
		return false, err
	}
	if val, err := gitrepo.GetGitConfigString("filter.drs.required"); err != nil || strings.TrimSpace(val) != "true" {
		return false, err
	}

	preCommitInstalled, err := hookContains("pre-commit", "git drs precommit")
	if err != nil {
		return false, err
	}
	if !preCommitInstalled {
		return false, nil
	}

	prePushInstalled, err := hookContains("pre-push", "git drs pre-push-prepare")
	if err != nil {
		return false, err
	}
	return prePushInstalled, nil
}

func hookContains(name, marker string) (bool, error) {
	hooksDir, err := gitrepo.GetGitHooksDir()
	if err != nil {
		return false, fmt.Errorf("unable to get hooks directory: %w", err)
	}
	content, err := os.ReadFile(filepath.Join(hooksDir, name))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return strings.Contains(string(content), marker), nil
}

func initGitConfig() error {
	configs := map[string]string{
		"lfs.allowincompletepush": "false",
		"lfs.concurrenttransfers": strconv.Itoa(transfers),
		// Use git-drs as the long-running filter-process handler.
		// This replaces the default git-lfs smudge/clean per-invocation commands
		// with a single persistent process that calls the DRS transfer stack directly.
		"filter.drs.clean":    "git-drs clean -- %f",
		"filter.drs.smudge":   "git-drs smudge -- %f",
		"filter.drs.process":  "git-drs filter",
		"filter.drs.required": "true",
		// Canonical git-drs config keys consumed by clients.
		"drs.upsert":                  strconv.FormatBool(upsert),
		"drs.multipart-threshold":     strconv.Itoa(multiPartThreshold),
		"drs.enable-data-client-logs": strconv.FormatBool(enableDataClientLogs),
	}

	if err := gitrepo.SetGitConfigOptions(configs); err != nil {
		return fmt.Errorf("unable to write git config: %w", err)
	}
	return nil
}

func init() {
	Cmd.Flags().IntVarP(&transfers, "transfers", "t", 1, "Number of concurrent transfers")
	Cmd.Flags().BoolVarP(&upsert, "upsert", "u", false, "Enable upsert for DRS objects")
	Cmd.Flags().IntVarP(&multiPartThreshold, "multipart-threshold", "m", 5120, "Multipart threshold in MB")
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

# The managed git-drs push command handles upload/register directly.
# The hook only stages metadata before the Git push proceeds.
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
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmdOut, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("unable to locate git directory: %w", err)
	}
	gitDir := strings.TrimSpace(string(cmdOut))
	hooksDir := filepath.Join(gitDir, "hooks")
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
