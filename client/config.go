package client

import (
	"io"
	"log"
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
	QueryServer Server `json:"queryServer"`
	WriteServer Server `json:"writeServer"`
}

const (
	DRS_CONFIG = ".drsconfig"
)

func LoadConfig() (*Config, error) {
	//look in Git base dir and find .drsconfig file

	topLevel, err := utils.GitTopLevel()

	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(topLevel, DRS_CONFIG)

	log.Printf("Looking for %s", configPath)
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

	log.Printf("Config: %s %#v", string(b), conf)
	return &conf, nil
}
