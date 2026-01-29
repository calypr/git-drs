package gitrepo

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/calypr/git-drs/common"
	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
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

// GetGitConfigValue retrieves a value from git config by key (string)
func GetGitConfigValue(conf *gitconfig.Config, key string) string {
	parts := strings.Split(key, ".")
	if len(parts) < 2 {
		return ""
	}

	// Check for section.key
	if len(parts) == 2 {
		return conf.Raw.Section(parts[0]).Option(parts[1])
	}

	// Check for section.subsection.key
	section := parts[0]
	subsection := strings.Join(parts[1:len(parts)-1], ".")
	option := parts[len(parts)-1]

	return conf.Raw.Section(section).Subsection(subsection).Option(option)
}

// GetGitConfigString reads a string value from git config
func GetGitConfigString(key string) (string, error) {
	repo, err := GetRepo()
	if err != nil {
		return "", nil
	}
	conf, err := repo.Config()
	if err != nil {
		return "", nil
	}
	return GetGitConfigValue(conf, key), nil
}

// GetGitConfigInt reads an integer value from git config
func GetGitConfigInt(key string, defaultValue int64) (int64, error) {
	strVal, err := GetGitConfigString(key)
	if err != nil || strVal == "" {
		return defaultValue, err
	}

	parsed, err := strconv.ParseInt(strVal, 10, 64)
	if err != nil {
		return defaultValue, fmt.Errorf("invalid int value for %s: %q", key, strVal)
	}

	return parsed, nil
}

// GetGitConfigBool reads a boolean value from git config
func GetGitConfigBool(key string, defaultValue bool) (bool, error) {
	strVal, err := GetGitConfigString(key)
	if err != nil || strVal == "" {
		return defaultValue, err
	}

	parsed, err := strconv.ParseBool(strVal)
	if err != nil {
		return defaultValue, fmt.Errorf("invalid boolean value for %s: %q", key, strVal)
	}

	return parsed, nil
}

// SetGitConfigOptions updates git configuration with the provided key-value pairs
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
