package config

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/go-git/go-git/v5"
	"gopkg.in/yaml.v3"
)

// RemoteType represents the type of server being initialized
type RemoteType string
type Remote string

const (
	ORIGIN = "origin"

	Gen3ServerType  RemoteType = "gen3"
	LocalServerType RemoteType = "local"
	TerraServerType RemoteType = "terra"

	configSection          = "drs"
	remoteSubsectionPrefix = "remote."
)

var ErrNoDefaultRemote = errors.New("no default remote configured")

// Config holds the overall config structure
type Config struct {
	DefaultRemote Remote
	Remotes       map[Remote]RemoteSelect
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
	} else if x.Terra != nil {
		return x.Terra
	}
	return nil
}

// GetDefaultRemote returns the configured default remote with validation
func (c Config) GetDefaultRemote() (Remote, error) {
	if c.DefaultRemote == "" {
		return "", fmt.Errorf(
			"%w.\n"+
				"Set one with: git drs remote set <name>\n"+
				"Available remotes: %v\n"+
				"Config: %v\n",
			ErrNoDefaultRemote,
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
	} else if remote.Terra != nil {
		remoteSubsection.SetOption("type", "terra")
		remoteSubsection.SetOption("endpoint", remote.Terra.Endpoint)
		if remote.Terra.Auth != "" {
			remoteSubsection.SetOption("auth", remote.Terra.Auth)
		}
		if remote.Terra.Mode != "" {
			remoteSubsection.SetOption("mode", remote.Terra.Mode)
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

func parseAndAddRemote(cfg *Config, subsectionName string, remoteType string, endpoint string, project string, bucket string, organization string, storagePrefix string, auth string, mode string) {
	if !strings.HasPrefix(subsectionName, remoteSubsectionPrefix) {
		return
	}

	remoteName := Remote(strings.TrimPrefix(subsectionName, remoteSubsectionPrefix))
	rs := RemoteSelect{}

	if remoteType == "gen3" || remoteType == "" {
		rs.Gen3 = &Gen3Remote{
			Endpoint:      endpoint,
			ProjectID:     project,
			Bucket:        bucket,
			Organization:  organization,
			StoragePrefix: storagePrefix,
		}
	} else if remoteType == "terra" {
		rs.Terra = &TerraRemote{
			Endpoint: endpoint,
			Auth:     auth,
			Mode:     mode,
		}
	} else if remoteType == "local" {
		rs.Local = &LocalRemote{
			BaseURL:       endpoint,
			ProjectID:     project,
			Bucket:        bucket,
			Organization:  organization,
			StoragePrefix: storagePrefix,
		}
	}

	cfg.Remotes[remoteName] = rs
}

func loadGitConfigOverrides(cfg *Config) error {
	cmd := exec.Command("git", "config", "--local", "--get-regexp", `^drs\.`)
	out, err := cmd.Output()
	if err != nil {
		// git config exits non-zero when no matching keys exist. In that case,
		// the go-git result above is still the complete repository-local config.
		return nil
	}

	remoteOptions := make(map[Remote]map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if key == "drs.default-remote" {
			cfg.DefaultRemote = Remote(value)
			continue
		}
		const prefix = "drs.remote."
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		idx := strings.LastIndex(rest, ".")
		if idx <= 0 || idx == len(rest)-1 {
			continue
		}
		name := Remote(rest[:idx])
		option := rest[idx+1:]
		if remoteOptions[name] == nil {
			remoteOptions[name] = make(map[string]string)
		}
		remoteOptions[name][option] = value
	}

	for name, opts := range remoteOptions {
		parseAndAddRemote(
			cfg,
			remoteSubsectionPrefix+string(name),
			opts["type"],
			opts["endpoint"],
			opts["project"],
			opts["bucket"],
			opts["organization"],
			opts["storage_prefix"],
			opts["auth"],
			opts["mode"],
		)
	}
	return nil
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
	if err := loadRepositoryConfig(cfg); err != nil {
		return nil, err
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
				subsection.Option("auth"),
				subsection.Option("mode"),
			)
		}
	}

	if err := loadGitConfigOverrides(cfg); err != nil {
		return nil, err
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

func RemoveRemote(name Remote) (*Config, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	if _, ok := cfg.Remotes[name]; !ok {
		return nil, fmt.Errorf("remote '%s' not found", name)
	}

	delete(cfg.Remotes, name)

	keys := []string{
		fmt.Sprintf("drs.remote.%s.type", name),
		fmt.Sprintf("drs.remote.%s.endpoint", name),
		fmt.Sprintf("drs.remote.%s.project", name),
		fmt.Sprintf("drs.remote.%s.bucket", name),
		fmt.Sprintf("drs.remote.%s.organization", name),
		fmt.Sprintf("drs.remote.%s.storage_prefix", name),
		fmt.Sprintf("drs.remote.%s.token", name),
		fmt.Sprintf("drs.remote.%s.username", name),
		fmt.Sprintf("drs.remote.%s.password", name),
		fmt.Sprintf("remote.%s.lfsurl", name),
	}
	if err := gitrepo.UnsetGitConfigOptions(keys); err != nil {
		return nil, err
	}

	if cfg.DefaultRemote == name {
		cfg.DefaultRemote = firstRemote(cfg)
	}

	if err := SaveConfig(cfg); err != nil {
		return nil, err
	}

	if cfg.DefaultRemote == "" {
		if err := gitrepo.UnsetGitConfigOptions([]string{"drs.default-remote"}); err != nil {
			return nil, err
		}
	}

	return LoadConfig()
}

func firstRemote(cfg *Config) Remote {
	if cfg == nil || len(cfg.Remotes) == 0 {
		return ""
	}

	names := make([]string, 0, len(cfg.Remotes))
	for name := range cfg.Remotes {
		names = append(names, string(name))
	}
	sort.Strings(names)
	return Remote(names[0])
}

// GetGitConfigInt reads an integer value from git config
// getGitConfigValue retrieves a value from git config by key
func getConfigPath() (string, error) {
	topLevel, err := gitrepo.GitTopLevel()
	if err != nil {
		return "", err
	}

	configPath := filepath.Join(topLevel, gitrepo.RepoDRSDir, gitrepo.ConfigYAML)
	return configPath, nil
}

type repositoryConfig struct {
	Version       int                               `yaml:"version"`
	DefaultRemote string                            `yaml:"default_remote"`
	Remotes       map[string]repositoryRemoteConfig `yaml:"remotes"`
}

type repositoryRemoteConfig struct {
	Type     string `yaml:"type"`
	Endpoint string `yaml:"endpoint"`
	Auth     string `yaml:"auth"`
	Mode     string `yaml:"mode"`
}

// loadRepositoryConfig loads only the public, allowlisted clone-portable
// configuration. KnownFields deliberately fails closed for tokens, headers,
// credential paths, and future fields until they receive a security review.
func loadRepositoryConfig(cfg *Config) error {
	path, err := getConfigPath()
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open repository git-drs config: %w", err)
	}
	defer f.Close()
	var raw repositoryConfig
	dec := yaml.NewDecoder(io.LimitReader(f, 1<<20))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("invalid repository config %s: %w", path, err)
	}
	if raw.Version != 1 {
		return fmt.Errorf("invalid repository config %s: unsupported version %d", path, raw.Version)
	}
	if strings.TrimSpace(raw.DefaultRemote) != "" {
		cfg.DefaultRemote = Remote(strings.TrimSpace(raw.DefaultRemote))
	}
	for name, r := range raw.Remotes {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("invalid repository config %s: remote name cannot be empty", path)
		}
		if r.Type != "terra" {
			return fmt.Errorf("invalid repository config %s: remote %q type must be terra", path, name)
		}
		if r.Auth != "google-adc" {
			return fmt.Errorf("invalid repository config %s: remote %q auth must be google-adc", path, name)
		}
		if r.Mode != "read-only" {
			return fmt.Errorf("invalid repository config %s: remote %q mode must be read-only", path, name)
		}
		u, err := url.ParseRequestURI(strings.TrimSpace(r.Endpoint))
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return fmt.Errorf("invalid repository config %s: remote %q endpoint must be an HTTPS URL without credentials", path, name)
		}
		cfg.Remotes[Remote(name)] = RemoteSelect{Terra: &TerraRemote{Endpoint: u.String(), Auth: r.Auth, Mode: r.Mode}}
	}
	return nil
}
