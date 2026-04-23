package config

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/client/drs"
	"github.com/calypr/git-drs/client/local"
	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/gitrepo"
	"github.com/go-git/go-git/v5"
)

// RemoteType represents the type of server being initialized
type RemoteType string
type Remote string

const (
	ORIGIN = "origin"

	Gen3ServerType  RemoteType = "gen3"
	LocalServerType RemoteType = "local"

	configSection          = "drs"
	remoteSubsectionPrefix = "remote."
)

func AllRemoteTypes() []RemoteType {
	return []RemoteType{Gen3ServerType, LocalServerType}
}

func IsValidRemoteType(mode string) error {
	modeOptions := make([]string, len(AllRemoteTypes()))
	for i, m := range AllRemoteTypes() {
		modeOptions[i] = string(m)
	}

	for _, validMode := range modeOptions {
		if mode == string(validMode) {
			return nil
		}
	}

	return fmt.Errorf("invalid mode '%s'. Valid options are: %s", mode, strings.Join(modeOptions, ", "))
}

type DRSRemote interface {
	GetProjectId() string
	GetOrganization() string
	GetEndpoint() string
	GetBucketName() string
	GetStoragePrefix() string
	GetClient(remoteName string, logger *slog.Logger) (*client.GitContext, error)
}

type RemoteSelect struct {
	Gen3  *drs.Gen3Remote
	Local *local.LocalRemote
}

// Config holds the overall config structure
type Config struct {
	DefaultRemote Remote
	Remotes       map[Remote]RemoteSelect
}

func (c Config) GetRemoteClient(remote Remote, logger *slog.Logger) (*client.GitContext, error) {
	x, ok := c.Remotes[remote]
	if !ok {
		return nil, fmt.Errorf("GetRemoteClient no remote configuration found for current remote: %s", remote)
	}
	if x.Local != nil {
		return x.Local.GetClient(string(remote), logger)
	}
	if x.Gen3 != nil {
		username, password, err := gitrepo.GetRemoteBasicAuth(string(remote))
		if err == nil && strings.TrimSpace(username) != "" && strings.TrimSpace(password) != "" {
			// If repo-local basic auth is configured, prefer the local/basic-auth client
			// path even when the remote entry was parsed as Gen3.
			localRemote := &local.LocalRemote{
				BaseURL:       x.Gen3.Endpoint,
				ProjectID:     x.Gen3.ProjectID,
				Bucket:        x.Gen3.Bucket,
				Organization:  x.Gen3.Organization,
				StoragePrefix: x.Gen3.StoragePrefix,
				BasicUsername: username,
				BasicPassword: password,
			}
			return localRemote.GetClient(string(remote), logger)
		}
		return x.Gen3.GetClient(string(remote), logger)
	}
	return nil, fmt.Errorf("no valid remote configuration found for current remote: %s", remote)
}

func (c Config) GetRemote(remote Remote) DRSRemote {
	x, ok := c.Remotes[remote]
	if !ok {
		return nil
	}
	if x.Gen3 != nil {
		return x.Gen3
	} else if x.Local != nil {
		return x.Local
	}
	return nil
}

// GetDefaultRemote returns the configured default remote with validation
func (c Config) GetDefaultRemote() (Remote, error) {
	if c.DefaultRemote == "" {
		return "", fmt.Errorf(
			"no default remote configured.\n"+
				"Set one with: git drs remote set <name>\n"+
				"Available remotes: %v\n"+
				"Config: %v\n",
			c.listRemoteNames(),
			c,
		)
	}

	if _, ok := c.Remotes[c.DefaultRemote]; !ok {
		return "", fmt.Errorf(
			"default remote '%s' not found in configuration.\n"+
				"Available remotes: %v",
			c.DefaultRemote,
			c.listRemoteNames(),
		)
	}

	return c.DefaultRemote, nil
}

// GetRemoteOrDefault returns the specified remote if provided, otherwise returns the default remote
func (c Config) GetRemoteOrDefault(remote string) (Remote, error) {
	if remote != "" {
		return Remote(remote), nil
	}
	return c.GetDefaultRemote()
}

// listRemoteNames returns a slice of all remote names for error messages
func (c Config) listRemoteNames() []string {
	names := make([]string, 0, len(c.Remotes))
	for name := range c.Remotes {
		names = append(names, string(name))
	}
	return names
}

// getRepo opens the current git repository
func getRepo() (*git.Repository, error) {
	return gitrepo.GetRepo()
}

func (c Config) ConfigPath() (string, error) {
	return getConfigPath()
}

// updates and git adds a Git DRS config file
// this should handle three cases:
// 1. create a new config file if it does not exist / is empty
// 2. return an error if the config file is invalid
// 3. update the existing config file, making sure to combine the new serversMap with the existing one
// UpdateRemote updates and saves configuration using go-git
func UpdateRemote(name Remote, remote RemoteSelect) (*Config, error) {
	repo, err := getRepo()
	if err != nil {
		return nil, err
	}

	conf, err := repo.Config()
	if err != nil {
		return nil, err
	}

	// Update drs.remote.<name> subsection
	remoteSubsectionName := fmt.Sprintf("%s%s", remoteSubsectionPrefix, name)
	remoteSubsection := conf.Raw.Section(configSection).Subsection(remoteSubsectionName)

	if remote.Gen3 != nil {
		remoteSubsection.SetOption("type", "gen3")
		remoteSubsection.SetOption("endpoint", remote.Gen3.Endpoint)
		remoteSubsection.SetOption("project", remote.Gen3.ProjectID)
		remoteSubsection.SetOption("bucket", remote.Gen3.Bucket)
		if remote.Gen3.Organization != "" {
			remoteSubsection.SetOption("organization", remote.Gen3.Organization)
		}
		if remote.Gen3.StoragePrefix != "" {
			remoteSubsection.SetOption("storage_prefix", remote.Gen3.StoragePrefix)
		}
	} else if remote.Local != nil {
		remoteSubsection.SetOption("type", "local")
		remoteSubsection.SetOption("endpoint", remote.Local.BaseURL)
		if remote.Local.ProjectID != "" {
			remoteSubsection.SetOption("project", remote.Local.ProjectID)
		}
		if remote.Local.Bucket != "" {
			remoteSubsection.SetOption("bucket", remote.Local.Bucket)
		}
		if remote.Local.Organization != "" {
			remoteSubsection.SetOption("organization", remote.Local.Organization)
		}
		if remote.Local.StoragePrefix != "" {
			remoteSubsection.SetOption("storage_prefix", remote.Local.StoragePrefix)
		}
	}

	// Set default remote if not set
	configRoot := conf.Raw.Section(configSection)
	defaultRemote := configRoot.Option("default-remote")
	if defaultRemote == "" {
		configRoot.SetOption("default-remote", string(name))
	}

	// Save config
	if err := repo.Storer.SetConfig(conf); err != nil {
		return nil, err
	}

	return LoadConfig()
}

func parseAndAddRemote(cfg *Config, subsectionName string, remoteType string, endpoint string, project string, bucket string, organization string, storagePrefix string) {
	if !strings.HasPrefix(subsectionName, remoteSubsectionPrefix) {
		return
	}

	remoteName := Remote(strings.TrimPrefix(subsectionName, remoteSubsectionPrefix))
	rs := RemoteSelect{}

	if remoteType == "gen3" || remoteType == "" {
		rs.Gen3 = &drs.Gen3Remote{
			Endpoint:      endpoint,
			ProjectID:     project,
			Bucket:        bucket,
			Organization:  organization,
			StoragePrefix: storagePrefix,
		}
	} else if remoteType == "local" {
		rs.Local = &local.LocalRemote{
			BaseURL:       endpoint,
			ProjectID:     project,
			Bucket:        bucket,
			Organization:  organization,
			StoragePrefix: storagePrefix,
		}
	}

	cfg.Remotes[remoteName] = rs
}

// LoadConfig loads configuration using go-git
func LoadConfig() (*Config, error) {
	repo, err := getRepo()
	if err != nil {
		return nil, err
	}

	conf, err := repo.Config()
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Remotes: make(map[Remote]RemoteSelect),
	}

	// Iterate over all sections to find 'drs' and its subsections
	for _, section := range conf.Raw.Sections {
		if section.Name != configSection {
			continue
		}

		// Check for default-remote in the section root.
		dr := section.Option("default-remote")
		if dr != "" {
			cfg.DefaultRemote = Remote(dr)
		}

		for _, subsection := range section.Subsections {
			if !strings.HasPrefix(subsection.Name, remoteSubsectionPrefix) {
				continue
			}
			parseAndAddRemote(
				cfg,
				subsection.Name,
				subsection.Option("type"),
				subsection.Option("endpoint"),
				subsection.Option("project"),
				subsection.Option("bucket"),
				subsection.Option("organization"),
				subsection.Option("storage_prefix"),
			)
		}
	}

	return cfg, nil
}

func CreateEmptyConfig() error {
	// With go-git, we just verify we are in a repo?
	// Existing behavior was ensuring file existence.
	// We can check if we can open the repo.
	_, err := getRepo()
	return err
}

func GetProjectId(remote Remote) (string, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return "", fmt.Errorf("error loading config: %v", err)
	}
	rmt := cfg.GetRemote(remote)
	if rmt == nil {
		return "", fmt.Errorf("no remote configuration found for current remote: %s", remote)
	}
	return rmt.GetProjectId(), nil
}

// SaveConfig writes the configuration using go-git
func SaveConfig(cfg *Config) error {
	repo, err := getRepo()
	if err != nil {
		return err
	}

	conf, err := repo.Config()
	if err != nil {
		return err
	}

	if cfg.DefaultRemote != "" {
		conf.Raw.Section(configSection).SetOption("default-remote", string(cfg.DefaultRemote))
	}

	return repo.Storer.SetConfig(conf)
}

// GetGitConfigInt reads an integer value from git config
// getGitConfigValue retrieves a value from git config by key
func getConfigPath() (string, error) {
	topLevel, err := gitrepo.GitTopLevel()
	if err != nil {
		return "", err
	}

	configPath := filepath.Join(topLevel, common.DRS_DIR, common.CONFIG_YAML)
	return configPath, nil
}
