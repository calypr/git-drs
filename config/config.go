package config

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/calypr/data-client/g3client"
	"github.com/calypr/git-drs/client"
	anvil_client "github.com/calypr/git-drs/client/anvil"
	"github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/gitrepo"
	"github.com/go-git/go-git/v5"
)

// RemoteType represents the type of server being initialized
type RemoteType string
type Remote string

const (
	ORIGIN = "origin"

	Gen3ServerType  RemoteType = "gen3"
	AnvilServerType RemoteType = "anvil"
)

func AllRemoteTypes() []RemoteType {
	return []RemoteType{Gen3ServerType, AnvilServerType}
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

// DRSRemote holds pointers to remote types
type DRSRemote interface {
	GetProjectId() string
	GetEndpoint() string
	GetBucketName() string
	GetClient(remoteName string, logger *slog.Logger, opts ...g3client.Option) (client.DRSClient, error)
}

type RemoteSelect struct {
	Gen3  *indexd.Gen3Remote
	Anvil *anvil_client.AnvilRemote
}

// Config holds the overall config structure
type Config struct {
	DefaultRemote Remote
	Remotes       map[Remote]RemoteSelect
}

func (c Config) GetRemoteClient(remote Remote, logger *slog.Logger, opts ...g3client.Option) (client.DRSClient, error) {
	x, ok := c.Remotes[remote]
	if !ok {
		return nil, fmt.Errorf("GetRemoteClient no remote configuration found for current remote: %s", remote)
	}
	if x.Gen3 != nil {
		return x.Gen3.GetClient(string(remote), logger, opts...)
	} else if x.Anvil != nil {
		return x.Anvil.GetClient(string(remote), logger, opts...)
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
	} else if x.Anvil != nil {
		return x.Anvil
	}
	return nil
}

// GetDefaultRemote returns the configured default remote with validation
func (c Config) GetDefaultRemote() (Remote, error) {
	if c.DefaultRemote == "" {
		return "", fmt.Errorf(
			"no default remote configured.\n"+
				"Set one with: git drs remote set <name>\n"+
				"Available remotes: %v",
			c.listRemoteNames(),
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
	subsection := fmt.Sprintf("remote.%s", name)

	if remote.Gen3 != nil {
		conf.Raw.Section("drs").Subsection(subsection).SetOption("type", "gen3")
		conf.Raw.Section("drs").Subsection(subsection).SetOption("endpoint", remote.Gen3.Endpoint)
		conf.Raw.Section("drs").Subsection(subsection).SetOption("project", remote.Gen3.ProjectID)
		conf.Raw.Section("drs").Subsection(subsection).SetOption("bucket", remote.Gen3.Bucket)
	} else if remote.Anvil != nil {
		conf.Raw.Section("drs").Subsection(subsection).SetOption("type", "anvil")
	}

	// Set default remote if not set
	defaultRemote := conf.Raw.Section("drs").Option("default-remote")
	if defaultRemote == "" {
		conf.Raw.Section("drs").SetOption("default-remote", string(name))
	}

	// Save config
	if err := repo.Storer.SetConfig(conf); err != nil {
		return nil, err
	}

	return LoadConfig()
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

	drsSection := conf.Raw.Section("drs")
	cfg.DefaultRemote = Remote(drsSection.Option("default-remote"))

	for _, subsection := range drsSection.Subsections {
		// Expect subsection name "remote.<name>"
		parts := strings.SplitN(subsection.Name, ".", 2)
		if len(parts) != 2 || parts[0] != "remote" {
			continue
		}
		remoteName := Remote(parts[1])
		rs := RemoteSelect{}

		remoteType := subsection.Option("type")
		if remoteType == "gen3" || remoteType == "" { // Default to gen3 for compatibility/inference
			rs.Gen3 = &indexd.Gen3Remote{
				Endpoint:  subsection.Option("endpoint"),
				ProjectID: subsection.Option("project"),
				Bucket:    subsection.Option("bucket"),
			}
		} else if remoteType == "anvil" {
			rs.Anvil = &anvil_client.AnvilRemote{}
		}

		cfg.Remotes[remoteName] = rs
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
		conf.Raw.Section("drs").SetOption("default-remote", string(cfg.DefaultRemote))
	}

	return repo.Storer.SetConfig(conf)
}

// GetGitConfigInt reads an integer value from git config
// getGitConfigValue retrieves a value from git config by key
