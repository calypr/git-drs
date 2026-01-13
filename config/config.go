package config

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/client"
	anvil_client "github.com/calypr/git-drs/client/anvil"
	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/projectdir"
	"github.com/calypr/git-drs/utils"
	"gopkg.in/yaml.v3"
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
	GetClient(params map[string]string, logger *log.Logger) (client.DRSClient, error)
}

type RemoteSelect struct {
	Gen3  *indexd_client.Gen3Remote `yaml:"gen3,omitempty"`
	Anvil *anvil_client.AnvilRemote `yaml:"anvil,omitempty"`
}

// Config holds the overall config structure
type Config struct {
	DefaultRemote Remote                  `yaml:"default_remote,omitempty"`
	Remotes       map[Remote]RemoteSelect `yaml:"remotes"`
}

func (c Config) GetRemoteClient(remote Remote, logger *log.Logger) (client.DRSClient, error) {
	x, ok := c.Remotes[remote]
	if !ok {
		return nil, fmt.Errorf("no remote configuration found for current remote: %s", remote)
	}
	if x.Gen3 != nil {
		configText, _ := yaml.Marshal(x.Gen3)
		configParams := make(map[string]string)
		yaml.Unmarshal(configText, configParams)
		configParams["remote_name"] = string(remote)
		return x.Gen3.GetClient(configParams, logger)
	} else if x.Anvil != nil {
		return x.Anvil.GetClient(nil, logger)
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
// This is a common pattern used across many commands that accept an optional --remote flag
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

func getConfigPath() (string, error) {
	topLevel, err := utils.GitTopLevel()
	if err != nil {
		return "", err
	}

	configPath := filepath.Join(topLevel, projectdir.DRS_DIR, projectdir.CONFIG_YAML)
	return configPath, nil
}

// updates and git adds a Git DRS config file
// this should handle three cases:
// 1. create a new config file if it does not exist / is empty
// 2. return an error if the config file is invalid
// 3. update the existing config file, making sure to combine the new serversMap with the existing one
func UpdateRemote(name Remote, remote RemoteSelect) (*Config, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	// check if file exists, if not create parent directory
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return nil, err
		}
	}

	// if file doesn't exist, create file. Otherwise, open the file for writing
	file, err := os.OpenFile(configPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// if file is not empty, unmarshal into Config
	var cfg Config
	if err := yaml.NewDecoder(file).Decode(&cfg); err != nil {
		// if the file is empty, we can just create a new config
		cfg = Config{
			Remotes: map[Remote]RemoteSelect{},
		}
	}

	if cfg.Remotes == nil {
		cfg.Remotes = make(map[Remote]RemoteSelect)
	}

	// Set as default if this is the first remote
	if len(cfg.Remotes) == 0 && cfg.DefaultRemote == "" {
		cfg.DefaultRemote = name
	}

	cfg.Remotes[name] = remote

	// overwrite the file using config
	file.Seek(0, 0)
	file.Truncate(0)
	if err := yaml.NewEncoder(file).Encode(cfg); err != nil {
		return nil, fmt.Errorf("failed to write config file: %w", err)
	}
	return &cfg, nil
}

// load an existing config
func LoadConfig() (*Config, error) {
	configPath, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file does not exist. Please run 'git drs init', see 'git drs init --help' for more details")
	}

	reader, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file at %s", configPath)
	}
	defer reader.Close()

	b, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("unable to read config file at %s", configPath)
	}

	conf := Config{}
	err = yaml.Unmarshal(b, &conf)
	if err != nil {
		return nil, fmt.Errorf("config file at %s is invalid: %w", configPath, err)
	}

	// Validate: if remotes exist but no default, error with migration instructions
	// FIXME: can be deleted after internal dev team ports over
	if len(conf.Remotes) > 0 && conf.DefaultRemote == "" {
		remoteNames := make([]string, 0, len(conf.Remotes))
		for name := range conf.Remotes {
			remoteNames = append(remoteNames, string(name))
		}
		return nil, fmt.Errorf(
			"configuration migration required.\n\n" +
				"Your config has remotes but no default_remote field.\n" +
				"Add this line to .drs/config.yaml:\n\n" +
				"  default_remote: <remote-name>\n\n" +
				"or delete and recreate the config file by re-running\n\n" +
				"  git drs remote add \n\n",
		)
	}

	return &conf, nil
}

func CreateEmptyConfig() error {
	configPath, err := getConfigPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return err
		}
	}

	// create empty config file
	file, err := os.OpenFile(configPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	return nil
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

// SaveConfig writes the configuration to disk
func SaveConfig(cfg *Config) error {
	configPath, err := getConfigPath()
	if err != nil {
		return err
	}

	file, err := os.OpenFile(configPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	if err := yaml.NewEncoder(file).Encode(cfg); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
