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
	NodeID            string            `yaml:"node_id"`
	AuthToken         string            `yaml:"auth_token"`
	HeartbeatInterval time.Duration     `yaml:"heartbeat_interval"`
	DiscoveryInterval time.Duration     `yaml:"discovery_interval"`
	Discovery         DiscoveryFeatures `yaml:"discovery"`
	Terminal          TerminalConfig    `yaml:"terminal"`
}

// TerminalConfig controls the remote terminal feature.
type TerminalConfig struct {
	Enabled bool `yaml:"enabled"` // opt-in — agent must explicitly enable
}

// DiscoveryFeatures controls what the agent collects.
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
		Terminal: TerminalConfig{
			Enabled: false,
		},
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

func (c *Config) IsRegistered() bool {
	return c.NodeID != "" && c.AuthToken != ""
}
