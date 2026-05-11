package config

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigPath = "/etc/nodestral/agent.yaml"
	DefaultAPIURL     = "https://api.nodestral.web.id"
	DefaultRelayURL   = "wss://nx.nodestral.web.id"
)

type Config struct {
	APIURL            string            `yaml:"api_url"`
	RelayURL          string            `yaml:"relay_url"`
	HomeDir           string            `yaml:"home_dir"`
	NodeID            string            `yaml:"node_id"`
	AuthToken         string            `yaml:"auth_token"`
	HeartbeatInterval time.Duration     `yaml:"heartbeat_interval"`
	DiscoveryInterval time.Duration     `yaml:"discovery_interval"`
	Discovery         DiscoveryFeatures `yaml:"discovery"`
}

type DiscoveryFeatures struct {
	Services     bool `yaml:"services"`
	Packages     bool `yaml:"packages"`
	Ports        bool `yaml:"ports"`
	SSHUsers     bool `yaml:"ssh_users"`
	Containers   bool `yaml:"containers"`
	Certificates bool `yaml:"certificates"`
	Firewall     bool `yaml:"firewall"`
	OSUpdates    bool `yaml:"os_updates"`
}

func DefaultConfig() *Config {
	return &Config{
		APIURL:            DefaultAPIURL,
		RelayURL:          DefaultRelayURL,
		HeartbeatInterval: 30 * time.Second,
		DiscoveryInterval: 5 * time.Minute,
		Discovery: DiscoveryFeatures{
			Services: true,
			Packages: true,
			Ports:    true,
			SSHUsers: true,
			Containers:   false,
			Certificates: false,
			Firewall:     false,
			OSUpdates:    false,
		},
	}
}

func resolveConfigPath(path string) string {
	if path != "" {
		return path
	}
	// 1. Explicit path given
	// 2. Check user config (~/.config/nodestral/agent.yaml)
	if home, err := os.UserHomeDir(); err == nil {
		userPath := filepath.Join(home, ".config", "nodestral", "agent.yaml")
		if _, err := os.Stat(userPath); err == nil {
			return userPath
		}
	}
	// 3. Check current directory
	if _, err := os.Stat("agent.yaml"); err == nil {
		return "agent.yaml"
	}
	// 4. Default system path
	return DefaultConfigPath
}

func Load(path string) (*Config, error) {
	path = resolveConfigPath(path)
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
	// Resolve home_dir: if not set, use current working directory
	if cfg.HomeDir == "" {
		if wd, err := os.Getwd(); err == nil {
			cfg.HomeDir = wd
		}
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

func (c *Config) IsRegistered() bool {
	return c.NodeID != "" && c.AuthToken != ""
}
