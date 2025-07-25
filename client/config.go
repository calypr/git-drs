package client

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/bmeg/git-drs/utils"
	"gopkg.in/yaml.v3"
)

// Gen3Auth holds authentication info for Gen3
type Gen3Auth struct {
	Type      string `yaml:"type"`
	Profile   string `yaml:"profile"`
	ProjectID string `yaml:"project_id"`
	Bucket    string `yaml:"bucket"`
}

// AnvilAuth holds authentication info for Anvil
type AnvilAuth struct {
	Type         string `yaml:"type"`
	TerraProject string `yaml:"terra_project"`
}

// Gen3Server holds Gen3 server config
type Gen3Server struct {
	Endpoint string   `yaml:"endpoint"`
	Auth     Gen3Auth `yaml:"auth"`
}

// AnvilServer holds Anvil server config
type AnvilServer struct {
	Endpoint string    `yaml:"endpoint"`
	Auth     AnvilAuth `yaml:"auth"`
}

// ServersMap holds all possible server configs
type ServersMap struct {
	Gen3  *Gen3Server  `yaml:"gen3,omitempty"`
	Anvil *AnvilServer `yaml:"anvil,omitempty"`
}

// Config holds the overall config structure
type Config struct {
	CurrentServer string     `yaml:"current_server"`
	Servers       ServersMap `yaml:"servers"`
}

const (
	CONFIG_YAML = "config.yaml"
	GEN3_TYPE   = "gen3"
	ANVIL_TYPE  = "anvil"
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
func UpdateServer(serversMap *ServersMap) error {
	configPath, err := getConfigPath()
	if err != nil {
		return err
	}

	// check if file exists, if not create parent directory
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			return err
		}
	}

	// if file doesn't exist, create file. Otherwise, open the file for writing
	file, err := os.OpenFile(configPath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// if file is not empty, unmarshal into Config
	var config Config
	if err := yaml.NewDecoder(file).Decode(&config); err != nil {
		// if the file is empty, we can just create a new config
		config = Config{
			CurrentServer: "",
			Servers:       ServersMap{},
		}
	}

	// // validate config that it has at least one server configured and a current server
	// if config.Servers.Gen3 == nil && config.Servers.Anvil == nil {
	// 	return fmt.Errorf("config file must have at least one server configured (gen3 or anvil)")
	// }
	// if config.CurrentServer == "" {
	// 	return fmt.Errorf("config file must have a current server set")
	// }

	// update existing config, combining new serversMap with existing one
	if serversMap.Gen3 != nil {
		config.CurrentServer = GEN3_TYPE
		config.Servers.Gen3 = serversMap.Gen3
	}
	if serversMap.Anvil != nil {
		config.CurrentServer = ANVIL_TYPE
		config.Servers.Anvil = serversMap.Anvil
	}

	// overwrite the file using config
	file.Seek(0, 0)
	file.Truncate(0)
	if err := yaml.NewEncoder(file).Encode(config); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// load an existing config
func LoadConfig() (*Config, error) {
	configPath, err := getConfigPath()

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, os.ErrNotExist
	}

	if err != nil {
		return nil, err
	}
	reader, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	b, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	conf := Config{}
	err = yaml.Unmarshal(b, &conf)
	if err != nil {
		return nil, err
	}

	return &conf, nil
}
