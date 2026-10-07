package gitrepo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5"
)

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
	out, err := gitConfigOutput("--get", key)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// GetGitConfigStrings reads every value from all Git configuration scopes.
func GetGitConfigStrings(key string) ([]string, error) {
	out, err := gitConfigOutput("--get-all", key)
	if err != nil {
		return nil, err
	}
	var values []string
	for _, value := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values, nil
}

func gitConfigOutput(args ...string) ([]byte, error) {
	cmdArgs := append([]string{"config"}, args...)
	cmd := exec.Command("git", cmdArgs...)
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && strings.TrimSpace(string(exitErr.Stderr)) == "" {
		// git config uses status 1 when no matching key exists.
		return nil, nil
	}
	if errors.As(err, &exitErr) && len(exitErr.Stderr) != 0 {
		return nil, fmt.Errorf("git config %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(exitErr.Stderr)))
	}
	return nil, fmt.Errorf("git config %s: %w", strings.Join(args, " "), err)
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
			// Handle subsections e.g. lfs.customtransfer.drs.path or drs.remote.origin.type
			section := parts[0]
			subsection := strings.Join(parts[1:len(parts)-1], ".")
			key := parts[len(parts)-1]
			conf.Raw.Section(section).Subsection(subsection).SetOption(key, value)
		}
	}

	return repo.Storer.SetConfig(conf)
}

// UnsetGitConfigOptions removes git config keys from local repo config.
// Missing keys are ignored.
func UnsetGitConfigOptions(keys []string) error {
	for _, key := range keys {
		cmd := exec.Command("git", "config", "--unset-all", key)
		if err := cmd.Run(); err != nil {
			// git exits non-zero when the key doesn't exist; this is safe to ignore.
			continue
		}
	}
	return nil
}

// GetGitHooksDir returns the absolute path Git uses for hooks.
func GetGitHooksDir() (string, error) {
	paths, err := ResolveRepositoryPaths(context.Background())
	if err != nil {
		return "", err
	}
	return paths.HooksDir, nil
}
