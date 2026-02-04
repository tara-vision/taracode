package provider

import (
	"time"
)

// HostConfig represents configuration for a single LLM host
type HostConfig struct {
	Name     string        `yaml:"name" mapstructure:"name"`
	URL      string        `yaml:"url" mapstructure:"url"`
	Vendor   string        `yaml:"vendor" mapstructure:"vendor"`     // optional, auto-detect if empty
	APIKey   string        `yaml:"api_key" mapstructure:"api_key"`   // optional for local servers
	Models   []string      `yaml:"models" mapstructure:"models"`     // optional, available models
	Fallback string        `yaml:"fallback" mapstructure:"fallback"` // host name to use if unavailable
	Timeout  time.Duration `yaml:"timeout" mapstructure:"timeout"`   // connection timeout
	Priority int           `yaml:"priority" mapstructure:"priority"` // lower number = higher priority
}

// HostsConfig represents the multi-host configuration section
type HostsConfig struct {
	Hosts       map[string]HostConfig `yaml:"hosts" mapstructure:"hosts"`
	DefaultHost string                `yaml:"default_host" mapstructure:"default_host"`
}

// NewHostsConfig creates a new empty HostsConfig
func NewHostsConfig() HostsConfig {
	return HostsConfig{
		Hosts: make(map[string]HostConfig),
	}
}

// NewHostsConfigFromLegacy creates a HostsConfig from legacy single-host configuration
func NewHostsConfigFromLegacy(host, apiKey, vendor string) HostsConfig {
	cfg := NewHostsConfig()
	cfg.Hosts["default"] = HostConfig{
		Name:     "default",
		URL:      host,
		Vendor:   vendor,
		APIKey:   apiKey,
		Timeout:  30 * time.Second,
		Priority: 1,
	}
	cfg.DefaultHost = "default"
	return cfg
}

// IsEmpty returns true if no hosts are configured
func (c HostsConfig) IsEmpty() bool {
	return len(c.Hosts) == 0
}

// GetHost returns a host config by name
func (c HostsConfig) GetHost(name string) (HostConfig, bool) {
	host, ok := c.Hosts[name]
	return host, ok
}

// GetDefaultHost returns the default host config
func (c HostsConfig) GetDefaultHost() (HostConfig, bool) {
	if c.DefaultHost == "" {
		return HostConfig{}, false
	}
	return c.GetHost(c.DefaultHost)
}

// HostStatus represents the current status of a host connection
type HostStatus string

const (
	HostStatusUnknown      HostStatus = "unknown"
	HostStatusConnecting   HostStatus = "connecting"
	HostStatusHealthy      HostStatus = "healthy"
	HostStatusUnhealthy    HostStatus = "unhealthy"
	HostStatusUnavailable  HostStatus = "unavailable"
	HostStatusDisconnected HostStatus = "disconnected"
)

// String returns the string representation of the host status
func (s HostStatus) String() string {
	return string(s)
}

// IsAvailable returns true if the host is in a usable state
func (s HostStatus) IsAvailable() bool {
	return s == HostStatusHealthy
}

// HostConnection represents an active connection to an LLM host
type HostConnection struct {
	Config      HostConfig
	Provider    Provider
	Status      HostStatus
	LastChecked time.Time
	LastError   error
	Models      []string      // Available models on this host
	Latency     time.Duration // Last measured response latency
}

// NewHostConnection creates a new host connection with initial unknown status
func NewHostConnection(cfg HostConfig) *HostConnection {
	return &HostConnection{
		Config: cfg,
		Status: HostStatusUnknown,
	}
}

// IsHealthy returns true if the host connection is healthy
func (c *HostConnection) IsHealthy() bool {
	return c.Status == HostStatusHealthy
}

// MarkHealthy updates the connection status to healthy
func (c *HostConnection) MarkHealthy(latency time.Duration) {
	c.Status = HostStatusHealthy
	c.LastChecked = time.Now()
	c.LastError = nil
	c.Latency = latency
}

// MarkUnhealthy updates the connection status to unhealthy with an error
func (c *HostConnection) MarkUnhealthy(err error) {
	c.Status = HostStatusUnhealthy
	c.LastChecked = time.Now()
	c.LastError = err
}

// MarkUnavailable updates the connection status to unavailable
func (c *HostConnection) MarkUnavailable(err error) {
	c.Status = HostStatusUnavailable
	c.LastChecked = time.Now()
	c.LastError = err
}

// HostInfo contains display information about a host
type HostInfo struct {
	Name        string        `json:"name"`
	URL         string        `json:"url"`
	Vendor      string        `json:"vendor"`
	Status      HostStatus    `json:"status"`
	Models      []string      `json:"models"`
	Latency     time.Duration `json:"latency"`
	LastChecked time.Time     `json:"last_checked"`
	LastError   string        `json:"last_error,omitempty"`
	Priority    int           `json:"priority"`
	Fallback    string        `json:"fallback,omitempty"`
	IsDefault   bool          `json:"is_default"`
}

// ToHostInfo converts a HostConnection to display-friendly HostInfo
func (c *HostConnection) ToHostInfo(isDefault bool) HostInfo {
	info := HostInfo{
		Name:        c.Config.Name,
		URL:         c.Config.URL,
		Status:      c.Status,
		Models:      c.Models,
		Latency:     c.Latency,
		LastChecked: c.LastChecked,
		Priority:    c.Config.Priority,
		Fallback:    c.Config.Fallback,
		IsDefault:   isDefault,
	}

	if c.Provider != nil {
		provInfo := c.Provider.Info()
		info.Vendor = provInfo.Type.String()
	} else if c.Config.Vendor != "" {
		info.Vendor = c.Config.Vendor
	}

	if c.LastError != nil {
		info.LastError = c.LastError.Error()
	}

	return info
}
