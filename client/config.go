package client

import (
	"io"
	"os"
	"path/filepath"

	"github.com/bmeg/git-drs/utils"
	"sigs.k8s.io/yaml"
)

type Server struct {
	BaseURL       string `json:"baseURL"`
	ExtensionType string `json:"type,omitempty"`
}

type Config struct {
	Gen3Profile string `json:"gen3Profile"`
	Gen3Project string `json:"gen3Project"`
	Gen3Bucket  string `json:"gen3Bucket"`
}

const (
	DRS_CONFIG = "config"
)

func LoadConfig() (*Config, error) {
	//look in Git base dir and find .drs/config file

	topLevel, err := utils.GitTopLevel()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(topLevel, DRS_DIR, DRS_CONFIG)

	//check if config exists
	reader, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}

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
