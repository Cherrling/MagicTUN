package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// Config is the top-level configuration.
type Config struct {
	Node    NodeConfig    `json:"node"`
	Routing RoutingConfig `json:"routing"`
	Gossip  GossipConfig  `json:"gossip"`
	UDP     UDPConfig     `json:"udp"`
	Logging LoggingConfig `json:"logging"`
}

// NodeConfig holds node identity and listen settings.
type NodeConfig struct {
	Name         string `json:"name"`
	IdentityFile string `json:"identity_file"`
	ListenAddr   string `json:"listen_addr"`
	Socks5Addr   string `json:"socks5_addr"`
	Socks5Auth   string `json:"socks5_auth"`
}

// RoutingConfig holds routing-related settings.
type RoutingConfig struct {
	DirectNetworks              []string `json:"direct_networks"`
	LocalPreference             uint32   `json:"local_preference"`
	MaxASPathLength             int      `json:"max_as_path_length"`
	RouteAdvertisementIntervalS string   `json:"route_advertisement_interval"`
}

// GossipConfig holds gossip protocol settings.
type GossipConfig struct {
	BootstrapPeers []string `json:"bootstrap_peers"`
	PushIntervalS  string   `json:"push_interval"`
	PeerTimeoutS   string   `json:"peer_timeout"`
	ProbeIntervalS string   `json:"probe_interval"`
	Fanout         int      `json:"fanout"`
}

// UDPConfig holds UDP relay settings.
type UDPConfig struct {
	SessionTTLS       string `json:"session_ttl"`
	SessionGCInterval string `json:"session_gc_interval"`
	MaxDatagramSize   int    `json:"max_datagram_size"`
}

// LoggingConfig holds logging settings.
type LoggingConfig struct {
	Level  string `json:"level"`
	Format string `json:"format"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Node: NodeConfig{
			ListenAddr: "0.0.0.0:9443",
			Socks5Addr: "127.0.0.1:1080",
			Socks5Auth: "noauth",
		},
		Routing: RoutingConfig{
			LocalPreference:             100,
			MaxASPathLength:             32,
			RouteAdvertisementIntervalS: "30s",
		},
		Gossip: GossipConfig{
			PushIntervalS:  "5s",
			PeerTimeoutS:   "15s",
			ProbeIntervalS: "3s",
			Fanout:         3,
		},
		UDP: UDPConfig{
			SessionTTLS:       "120s",
			SessionGCInterval: "30s",
			MaxDatagramSize:   1200,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
	}
}

// Load reads and parses a JSON config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate checks the configuration for correctness.
func (c *Config) Validate() error {
	if c.Node.ListenAddr == "" {
		return fmt.Errorf("node.listen_addr is required")
	}
	if c.Node.Socks5Addr == "" {
		return fmt.Errorf("node.socks5_addr is required")
	}
	for _, nw := range c.Routing.DirectNetworks {
		if _, _, err := net.ParseCIDR(nw); err != nil {
			return fmt.Errorf("invalid direct_network %q: %w", nw, err)
		}
	}
	return nil
}

func parseDuration(s string) time.Duration {
	d, _ := time.ParseDuration(s)
	return d
}

// RouteAdvertisementInterval returns the parsed route advertisement interval.
func (c *Config) RouteAdvertisementInterval() time.Duration {
	return parseDuration(c.Routing.RouteAdvertisementIntervalS)
}

// PushInterval returns the parsed gossip push interval.
func (c *Config) PushInterval() time.Duration {
	return parseDuration(c.Gossip.PushIntervalS)
}

// PeerTimeout returns the parsed peer timeout.
func (c *Config) PeerTimeout() time.Duration {
	return parseDuration(c.Gossip.PeerTimeoutS)
}

// ProbeInterval returns the parsed probe interval.
func (c *Config) ProbeInterval() time.Duration {
	return parseDuration(c.Gossip.ProbeIntervalS)
}

// SessionTTL returns the parsed UDP session TTL.
func (c *Config) SessionTTL() time.Duration {
	return parseDuration(c.UDP.SessionTTLS)
}

// SessionGCInterval returns the parsed session GC interval.
func (c *Config) SessionGCInterval() time.Duration {
	return parseDuration(c.UDP.SessionGCInterval)
}
