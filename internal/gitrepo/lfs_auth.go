package gitrepo

import (
	"fmt"
	"strings"

	gitconfig "github.com/go-git/go-git/v5/plumbing/format/config"
)

func remoteTokenKey(remoteName string) string {
	return fmt.Sprintf("drs.remote.%s.token", remoteName)
}

func remoteUsernameKey(remoteName string) string {
	return fmt.Sprintf("drs.remote.%s.username", remoteName)
}

func remotePasswordKey(remoteName string) string {
	return fmt.Sprintf("drs.remote.%s.password", remoteName)
}

func remoteLFSURL(endpoint string) string {
	base := strings.TrimSpace(strings.TrimRight(endpoint, "/"))
	if base == "" {
		return ""
	}
	return base + "/info/lfs"
}

// GetRemoteToken reads a remote-specific bearer token from repo-local git config.
func GetRemoteToken(remoteName string) (string, error) {
	return GetGitConfigString(remoteTokenKey(remoteName))
}

// SetRemoteToken stores a remote-specific bearer token in repo-local git config.
func SetRemoteToken(remoteName, token string) error {
	if strings.TrimSpace(remoteName) == "" {
		return fmt.Errorf("remote name is required")
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("token is required")
	}
	configs := map[string]string{remoteTokenKey(remoteName): token}
	return SetGitConfigOptions(configs)
}

func GetRemoteBasicAuth(remoteName string) (string, string, error) {
	username, err := GetGitConfigString(remoteUsernameKey(remoteName))
	if err != nil {
		return "", "", err
	}
	password, err := GetGitConfigString(remotePasswordKey(remoteName))
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(username), strings.TrimSpace(password), nil
}

func SetRemoteBasicAuth(remoteName, username, password string) error {
	if strings.TrimSpace(remoteName) == "" {
		return fmt.Errorf("remote name is required")
	}
	if strings.TrimSpace(username) == "" {
		return fmt.Errorf("username is required")
	}
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("password is required")
	}
	configs := map[string]string{
		remoteUsernameKey(remoteName): username,
		remotePasswordKey(remoteName): password,
	}
	return SetGitConfigOptions(configs)
}

// ConfigureCredentialHelperForRepo installs repo-local git credential helper wiring
// so git-lfs uses standard git credential resolution.
//
// credential.helper is a multi-valued git config key: using SetOption would
// overwrite any pre-existing helpers (e.g. a credential store with other remote
// credentials).  We use AddOption instead, guarded by a presence check to avoid
// accumulating duplicate entries on repeated calls.
func ConfigureCredentialHelperForRepo() error {
	repo, err := GetRepo()
	if err != nil {
		return err
	}
	conf, err := repo.Config()
	if err != nil {
		return err
	}

	const helperValue = "!git drs credential-helper"
	credSection := conf.Raw.Section("credential")

	if !containsOption(credSection.Options, "helper", helperValue) {
		credSection.AddOption("helper", helperValue)
	}
	credSection.SetOption("useHttpPath", "true")

	return repo.Storer.SetConfig(conf)
}

func containsOption(opts gitconfig.Options, key, value string) bool {
	for _, v := range opts.GetAll(key) {
		if v == value {
			return true
		}
	}
	return false
}

// SetRemoteLFSURL stores the LFS API endpoint for the provided git remote.
func SetRemoteLFSURL(remoteName, endpoint string) error {
	lfsURL := remoteLFSURL(endpoint)
	if lfsURL == "" {
		return fmt.Errorf("endpoint is required")
	}
	if strings.TrimSpace(remoteName) == "" {
		return fmt.Errorf("remote name is required")
	}
	configs := map[string]string{
		fmt.Sprintf("remote.%s.lfsurl", remoteName): lfsURL,
	}
	return SetGitConfigOptions(configs)
}
