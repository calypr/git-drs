package config

import (
	"fmt"
	"log"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/calypr/data-client/g3client"
	"github.com/calypr/git-drs/client"
	anvil_client "github.com/calypr/git-drs/client/anvil"
	"github.com/calypr/git-drs/client/indexd"
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
	AnvilServerType RemoteType = "anvil"
	LocalServerType RemoteType = "local"

	newConfigSection           = "lfs"
	newConfigSubsectionRoot    = "customtransfer.drs"
	legacyConfigSection        = "drs"
	remoteSubsectionPrefix     = "remote."
	legacyDefaultRemoteKey     = "drs.default-remote"
	namespacedDefaultRemoteKey = "lfs.customtransfer.drs.default-remote"
)

func AllRemoteTypes() []RemoteType {
	return []RemoteType{Gen3ServerType, AnvilServerType, LocalServerType}
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
	Local *local.LocalRemote
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
	} else if x.Local != nil {
		// Local client doesn't support options or named profiles in the same way, but follows pattern
		return local.NewLocalClient(*x.Local, logger), nil
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

	// Update lfs.customtransfer.drs.remote.<name> subsection
	remoteSubsectionName := fmt.Sprintf("%s.%s%s", newConfigSubsectionRoot, remoteSubsectionPrefix, name)
	remoteSubsection := conf.Raw.Section(newConfigSection).Subsection(remoteSubsectionName)

	if remote.Gen3 != nil {
		remoteSubsection.SetOption("type", "gen3")
		remoteSubsection.SetOption("endpoint", remote.Gen3.Endpoint)
		remoteSubsection.SetOption("project", remote.Gen3.ProjectID)
		remoteSubsection.SetOption("bucket", remote.Gen3.Bucket)
	} else if remote.Anvil != nil {
		remoteSubsection.SetOption("type", "anvil")
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
	}

	// Set default remote if not set
	configRoot := conf.Raw.Section(newConfigSection).Subsection(newConfigSubsectionRoot)
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

func parseAndAddRemote(cfg *Config, subsectionName string, remoteType string, endpoint string, project string, bucket string, organization string) {
	if !strings.HasPrefix(subsectionName, remoteSubsectionPrefix) {
		return
	}

	remoteName := Remote(strings.TrimPrefix(subsectionName, remoteSubsectionPrefix))
	rs := RemoteSelect{}

	if remoteType == "gen3" || remoteType == "" {
		rs.Gen3 = &indexd.Gen3Remote{
			Endpoint:  endpoint,
			ProjectID: project,
			Bucket:    bucket,
		}
	} else if remoteType == "anvil" {
		rs.Anvil = &anvil_client.AnvilRemote{}
	} else if remoteType == "local" {
		rs.Local = &local.LocalRemote{
			BaseURL:      endpoint,
			ProjectID:    project,
			Bucket:       bucket,
			Organization: organization,
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

	// Iterate over all sections to find 'lfs' and its subsections
	for _, section := range conf.Raw.Sections {
		if section.Name != newConfigSection {
			continue
		}

		// Check for default-remote in the root subsection
		dr := section.Subsection(newConfigSubsectionRoot).Option("default-remote")
		if dr != "" {
			cfg.DefaultRemote = Remote(dr)
		}

		for _, subsection := range section.Subsections {
			if !strings.HasPrefix(subsection.Name, newConfigSubsectionRoot+".") {
				continue
			}
			relativeName := strings.TrimPrefix(subsection.Name, newConfigSubsectionRoot+".")
			parseAndAddRemote(
				cfg,
				relativeName,
				subsection.Option("type"),
				subsection.Option("endpoint"),
				subsection.Option("project"),
				subsection.Option("bucket"),
				subsection.Option("organization"),
			)
		}
	}

	if cfg.DefaultRemote == "" {
		legacyRoot := conf.Raw.Section(legacyConfigSection)
		legacyDefault := legacyRoot.Option("default-remote")
		if legacyDefault != "" {
			log.Printf("Warning: git-drs config key '%s' is deprecated; use '%s'", legacyDefaultRemoteKey, namespacedDefaultRemoteKey)
			cfg.DefaultRemote = Remote(legacyDefault)
		}
	}

	// Also check legacy drs section for remotes
	legacyRoot := conf.Raw.Section(legacyConfigSection)
	for _, subsection := range legacyRoot.Subsections {
		if !strings.HasPrefix(subsection.Name, remoteSubsectionPrefix) {
			continue
		}
		remoteName := Remote(strings.TrimPrefix(subsection.Name, remoteSubsectionPrefix))
		if _, exists := cfg.Remotes[remoteName]; exists {
			continue
		}
		log.Printf("Warning: git-drs config key prefix 'drs.%s' is deprecated; use 'lfs.customtransfer.drs.%s'", subsection.Name, subsection.Name)
		parseAndAddRemote(
			cfg,
			subsection.Name,
			subsection.Option("type"),
			subsection.Option("endpoint"),
			subsection.Option("project"),
			subsection.Option("bucket"),
			subsection.Option("organization"),
		)
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
		conf.Raw.Section(newConfigSection).Subsection(newConfigSubsectionRoot).SetOption("default-remote", string(cfg.DefaultRemote))
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
