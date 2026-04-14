package config

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigPath = "/etc/nodestral/agent.yaml"
	DefaultAPIURL     = "https://api.nodestral.io"
)

type Config struct {
	APIURL            string        `yaml:"api_url"`
	NodeID            string        `yaml:"node_id"`
	AuthToken         string        `yaml:"auth_token"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	DiscoveryInterval time.Duration `yaml:"discovery_interval"`
}

func DefaultConfig() *Config {
	return &Config{
		APIURL:            DefaultAPIURL,
		HeartbeatInterval: 30 * time.Second,
		DiscoveryInterval: 5 * time.Minute,
	}
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath
	}

	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Save(path string) error {
	if path == "" {
		path = DefaultConfigPath
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// IsRegistered returns true if the agent has been registered with the API.
func (c *Config) IsRegistered() bool {
	return c.NodeID != "" && c.AuthToken != ""
}
