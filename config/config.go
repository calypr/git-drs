package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/utils"
	"gopkg.in/yaml.v3"
)

// Gen3Auth holds authentication info for Gen3
type Gen3Auth struct {
	Profile   string `yaml:"profile"`
	ProjectID string `yaml:"project_id"`
	Bucket    string `yaml:"bucket"`
}

// AnvilAuth holds authentication info for Anvil
type AnvilAuth struct {
	TerraProject string `yaml:"terra_project"`
}

// ServerType represents the type of server being initialized
type ServerType string

const (
	Gen3ServerType  ServerType = "gen3"
	AnvilServerType ServerType = "anvil"
)

func AllServerTypes() []ServerType {
	return []ServerType{Gen3ServerType, AnvilServerType}
}

func IsValidServerType(mode string) error {
	modeOptions := make([]string, len(AllServerTypes()))
	for i, m := range AllServerTypes() {
		modeOptions[i] = string(m)
	}

	for _, validMode := range modeOptions {
		if mode == string(validMode) {
			return nil
		}
	}

	return fmt.Errorf("invalid mode '%s'. Valid options are: %s", mode, strings.Join(modeOptions, ", "))
}

// Gen3Server holds Gen3 server config
type Gen3Server struct {
	Endpoint string   `yaml:"endpoint"`
	Auth     Gen3Auth `yaml:",inline"`
}

// AnvilServer holds Anvil server config
type AnvilServer struct {
	Endpoint string    `yaml:"endpoint"`
	Auth     AnvilAuth `yaml:",inline"`
}

// ServersMap holds all possible server configs
type ServersMap struct {
	Gen3  *Gen3Server  `yaml:"gen3,omitempty"`
	Anvil *AnvilServer `yaml:"anvil,omitempty"`
}

// Config holds the overall config structure
type Config struct {
	CurrentServer ServerType `yaml:"current_server"`
	Servers       ServersMap `yaml:"servers"`
}

const (
	LFS_OBJS_PATH = ".git/lfs/objects"
	DRS_DIR       = ".drs"
	// FIXME: should this be /lfs/objects or just /objects?
	DRS_OBJS_PATH = DRS_DIR + "/lfs/objects"
	CONFIG_YAML   = "config.yaml"
)

func getConfigPath() (string, error) {
	topLevel, err := utils.GitTopLevel()
	if err != nil {
		return "", err
	}

	configPath := filepath.Join(topLevel, DRS_DIR, CONFIG_YAML)
	return configPath, nil
}

// this should handle three cases:
// 1. create a new config file if it does not exist / is empty
// 2. return an error if the config file is invalid
// 3. update the existing config file, making sure to combine the new serversMap with the existing one
func UpdateServer(serversMap *ServersMap) (*Config, error) {
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
			Servers: ServersMap{},
		}
	}

	// update existing config, combining new serversMap with existing one
	if serversMap.Gen3 != nil {
		cfg.Servers.Gen3 = serversMap.Gen3
	}
	if serversMap.Anvil != nil {
		cfg.Servers.Anvil = serversMap.Anvil
	}

	// overwrite the file using config
	file.Seek(0, 0)
	file.Truncate(0)
	if err := yaml.NewEncoder(file).Encode(cfg); err != nil {
		return nil, fmt.Errorf("failed to write config file: %w", err)
	}

	return &cfg, nil
}

func UpdateCurrentServer(serverType ServerType) (*Config, error) {
	// load existing config
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	// set current server
	cfg.CurrentServer = serverType

	// overwrite the existing config file
	configPath, err := getConfigPath()
	if err != nil {
		return nil, err
	}

	file, err := os.OpenFile(configPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	file.Seek(0, 0)
	file.Truncate(0)
	if err := yaml.NewEncoder(file).Encode(cfg); err != nil {
		return nil, fmt.Errorf("failed to write config file: %w", err)
	}

	return cfg, nil
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
		return nil, fmt.Errorf("Failed to open config file at %s", configPath)
	}
	defer reader.Close()

	b, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("Unable to read config file at %s", configPath)
	}

	conf := Config{}
	err = yaml.Unmarshal(b, &conf)
	if err != nil {
		return nil, fmt.Errorf("Config file at %s is invalid: %w", configPath, err)
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
