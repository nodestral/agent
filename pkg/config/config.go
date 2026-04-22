package config

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigPath = "/etc/nodestral/agent.yaml"
	DefaultAPIURL     = "https://nx.nodestral.web.id"
)

type Config struct {
	APIURL            string            `yaml:"api_url"`
	NodeID            string            `yaml:"node_id"`
	AuthToken         string            `yaml:"auth_token"`
	HeartbeatInterval time.Duration     `yaml:"heartbeat_interval"`
	DiscoveryInterval time.Duration     `yaml:"discovery_interval"`
	Discovery         DiscoveryFeatures `yaml:"discovery"`
}

// DiscoveryFeatures controls what the agent collects.
// All features are opt-in. Features that require elevated permissions
// will only attempt collection if explicitly enabled by the user.
type DiscoveryFeatures struct {
	// Basic features — work with zero special permissions
	Services   bool `yaml:"services"`
	Packages   bool `yaml:"packages"`
	Ports      bool `yaml:"ports"`
	SSHUsers   bool `yaml:"ssh_users"`

	// Elevated features — require additional permissions (see PERMISSIONS.md)
	// Agent will log a warning if enabled but permissions are insufficient
	Containers  bool `yaml:"containers"`   // needs: docker group OR root
	Certificates bool `yaml:"certificates"` // needs: read access to /etc/letsencrypt/live/
	Firewall    bool `yaml:"firewall"`     // needs: root OR sudoers rule
	OSUpdates   bool `yaml:"os_updates"`   // needs: root OR sudoers rule
}

func DefaultConfig() *Config {
	return &Config{
		APIURL:            DefaultAPIURL,
		HeartbeatInterval: 30 * time.Second,
		DiscoveryInterval: 5 * time.Minute,
		Discovery: DiscoveryFeatures{
			Services: true,
			Packages: true,
			Ports:    true,
			SSHUsers: true,
			// Elevated features off by default — user must opt-in
			Containers:  false,
			Certificates: false,
			Firewall:    false,
			OSUpdates:   false,
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

// IsRegistered returns true if the agent has been registered with the API.
func (c *Config) IsRegistered() bool {
	return c.NodeID != "" && c.AuthToken != ""
}
