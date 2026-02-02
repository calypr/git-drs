package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/calypr/git-drs/common"
	"github.com/go-git/go-git/v5"
)

func DrsTopLevel() (string, error) {
	base, err := GitTopLevel()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, common.DRS_DIR), nil
}

// GetRepo opens the current git repository
func GetRepo() (*git.Repository, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return git.PlainOpenWithOptions(cwd, &git.PlainOpenOptions{DetectDotGit: true})
}

// GitTopLevel returns the absolute path of the git repository root
func GitTopLevel() (string, error) {
	repo, err := GetRepo()
	if err != nil {
		return "", err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	return wt.Filesystem.Root(), nil
}

// GetGitConfigString reads a string value from git config using the git command
// to ensure we pick up values from all scopes (system, global, local).
func GetGitConfigString(key string) (string, error) {
	cmd := exec.Command("git", "config", "--get", key)
	out, err := cmd.Output()
	if err != nil {
		// git config returns exit code 1 if the key is not found
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

// GetGitConfigInt reads an integer value from git config
func GetGitConfigInt(key string, defaultValue int64) int64 {
	valStr, err := GetGitConfigString(key)
	if err != nil || valStr == "" {
		return defaultValue
	}
	val, err := strconv.ParseInt(valStr, 10, 64)
	if err != nil {
		return defaultValue
	}
	return val
}

// GetGitConfigBool reads a boolean value from git config
func GetGitConfigBool(key string, defaultValue bool) bool {
	valStr, err := GetGitConfigString(key)
	if err != nil || valStr == "" {
		return defaultValue
	}
	val, err := strconv.ParseBool(valStr)
	if err != nil {
		return defaultValue
	}
	return val
}

func SetGitConfigOptions(configs map[string]string) error {
	repo, err := GetRepo()
	if err != nil {
		return err
	}
	conf, err := repo.Config()
	if err != nil {
		return err
	}

	for key, value := range configs {
		parts := strings.Split(key, ".")
		if len(parts) == 2 {
			conf.Raw.Section(parts[0]).SetOption(parts[1], value)
		} else if len(parts) > 2 {
			// Handle subsections e.g. lfs.customtransfer.drs.path
			section := parts[0]
			subsection := strings.Join(parts[1:len(parts)-1], ".")
			key := parts[len(parts)-1]
			conf.Raw.Section(section).Subsection(subsection).SetOption(key, value)
		}
	}

	return repo.Storer.SetConfig(conf)
}

// GetGitHooksDir returns the absolute path to the .git/hooks directory
func GetGitHooksDir() (string, error) {
	repo, err := GetRepo()
	if err != nil {
		return "", err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	// This is a simplification; for complex setups (submodules, worktrees),
	// we might need more robust logic, but this matches previous behavior.
	return filepath.Join(wt.Filesystem.Root(), ".git", "hooks"), nil
}

// AddFile adds a file to the git staging area (index)
func AddFile(path string) error {
	repo, err := GetRepo()
	if err != nil {
		return err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	_, err = wt.Add(path)
	return err
}
