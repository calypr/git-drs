package initialize

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"log"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/projectdir"
	"github.com/calypr/git-drs/utils"
	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize repo for git-drs",
	Long: "Description:" +
		"\n  Initialize repo for git-drs",
	Args: cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		logg := drslog.GetLogger()

		// check if .git dir exists to ensure you're in a git repository
		_, err := utils.GitTopLevel()
		if err != nil {
			return fmt.Errorf("Error: not in a git repository. Please run this command in the root of your git repository.\n")
		}

		// create config file if it doesn't exist
		err = config.CreateEmptyConfig()
		if err != nil {
			return fmt.Errorf("Error: unable to create config file: %v\n", err)
		}

		// load the config
		_, err = config.LoadConfig()
		if err != nil {
			logg.Printf("We should probably fix this: %v", err)
			return fmt.Errorf("Error: unable to load config file: %v\n", err)
		}

		// add some patterns to the .gitignore if not already present
		configStr := "!" + filepath.Join(projectdir.DRS_DIR, projectdir.CONFIG_YAML)
		drsDirStr := fmt.Sprintf("%s/**", projectdir.DRS_DIR)

		gitignorePatterns := []string{drsDirStr, configStr, "drs_downloader.log"}
		for _, pattern := range gitignorePatterns {
			if err := ensureDrsObjectsIgnore(pattern, logg); err != nil {
				return fmt.Errorf("Init Error: %v\n", err)
			}
		}

		// log message based on if .gitignore is untracked or modified (i.e. if we actually made changes something)
		statusCmd := exec.Command("git", "status", "--porcelain", ".gitignore")
		output, err := statusCmd.Output()
		if err != nil {
			return fmt.Errorf("Error checking git status of .gitignore file: %v", err)
		}
		if len(output) > 0 {
			logg.Print(".gitignore has been updated and staged")
		} else {
			logg.Print(".gitignore already up to date")
		}

		// git add .gitignore
		gitCmd := exec.Command("git", "add", ".gitignore")
		if cmdOut, err := gitCmd.Output(); err != nil {
			return fmt.Errorf("Error adding .gitignore to git: %s", cmdOut)
		}

		// setup lfs custom transfer
		// TODO: may need to generalize for anvil
		err = initGitConfig()
		if err != nil {
			return fmt.Errorf("Error initializing custom transfer for DRS: %v\n", err)
		}

		// add pre-commit hook
		err = addPreCommitHook()
		if err != nil {
			return fmt.Errorf("Error adding pre-commit hook: %v\n", err)
		}

		// final logs
		logg.Print("Git DRS initialized")
		return nil
	},
}

func addPreCommitHook() error {
	// Create .git/hooks/pre-commit file
	hooksDir := filepath.Join(".git", "hooks")
	preCommitPath := filepath.Join(hooksDir, "pre-commit")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("[ERROR] unable to create pre-commit hook file: %v", err)
	}
	hookContent := "#!/bin/sh\ngit drs precommit\n"
	if err := os.WriteFile(preCommitPath, []byte(hookContent), 0755); err != nil {
		return fmt.Errorf("[ERROR] unable to write to pre-commit hook: %v", err)
	}
	return nil
}

func initGitConfig() error {
	configs := [][]string{
		{"lfs.standalonetransferagent", "drs"},
		{"lfs.customtransfer.drs.path", "git-drs"},
		{"lfs.customtransfer.drs.concurrent", "false"},
		{"lfs.customtransfer.drs.args", "transfer"},
		// TODO: different for anvil / read-only?
		{"lfs.allowincompletepush", "false"},
	}

	for _, args := range configs {
		cmd := exec.Command("git", "config", args[0], args[1])
		if cmdOut, err := cmd.Output(); err != nil {
			return fmt.Errorf("Unable to set git config %s: %s", args[0], cmdOut)
		}
	}

	return nil
}

// ensureDrsObjectsIgnore ensures that ".drs/objects" is ignored in .gitignore.
// It creates the file if it doesn't exist, and adds the line if not present.
func ensureDrsObjectsIgnore(ignorePattern string, logger *log.Logger) error {
	const (
		gitignorePath = ".gitignore"
	)

	var found bool

	// Check if .gitignore exists
	var lines []string
	file, err := os.Open(gitignorePath)
	if err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			// Normalize slashes for comparison, trim spaces
			if strings.TrimSpace(line) == ignorePattern {
				found = true
			}
			lines = append(lines, line)
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("error reading %s: %w", gitignorePath, err)
		}
	} else if os.IsNotExist(err) {
		// .gitignore doesn't exist, will create it
		lines = []string{}
	} else {
		return fmt.Errorf("could not open %s: %w", gitignorePath, err)
	}

	if found {
		return nil
	}

	// Add the ignore pattern (ensure a blank line before if file is not empty)
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}
	lines = append(lines, ignorePattern)

	// Write back the file
	f, err := os.OpenFile(gitignorePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return fmt.Errorf("could not write to %s: %w", gitignorePath, err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for i, l := range lines {
		if i > 0 {
			_, _ = w.WriteString("\n")
		}
		_, _ = w.WriteString(l)
	}
	// Always end with a trailing newline
	_, _ = w.WriteString("\n")
	if err := w.Flush(); err != nil {
		return fmt.Errorf("error writing %s: %w", gitignorePath, err)
	}

	return nil
}
