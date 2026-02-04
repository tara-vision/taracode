package provider

import (
	"fmt"
	"testing"
	"time"
)

func TestNewHostsConfig(t *testing.T) {
	cfg := NewHostsConfig()

	if cfg.Hosts == nil {
		t.Error("Hosts map should be initialized")
	}
	if len(cfg.Hosts) != 0 {
		t.Errorf("Hosts map should be empty, got %d", len(cfg.Hosts))
	}
	if cfg.DefaultHost != "" {
		t.Errorf("DefaultHost should be empty, got %q", cfg.DefaultHost)
	}
}

func TestNewHostsConfigFromLegacy(t *testing.T) {
	cfg := NewHostsConfigFromLegacy("http://localhost:11434", "test-key", "ollama")

	if len(cfg.Hosts) != 1 {
		t.Errorf("Expected 1 host, got %d", len(cfg.Hosts))
	}
	if cfg.DefaultHost != "default" {
		t.Errorf("Expected default host 'default', got %q", cfg.DefaultHost)
	}

	host, ok := cfg.Hosts["default"]
	if !ok {
		t.Error("Expected 'default' host to exist")
	}
	if host.URL != "http://localhost:11434" {
		t.Errorf("Expected URL 'http://localhost:11434', got %q", host.URL)
	}
	if host.APIKey != "test-key" {
		t.Errorf("Expected APIKey 'test-key', got %q", host.APIKey)
	}
	if host.Vendor != "ollama" {
		t.Errorf("Expected Vendor 'ollama', got %q", host.Vendor)
	}
}

func TestHostsConfigIsEmpty(t *testing.T) {
	cfg := NewHostsConfig()
	if !cfg.IsEmpty() {
		t.Error("Empty config should return true for IsEmpty")
	}

	cfg.Hosts["test"] = HostConfig{Name: "test", URL: "http://test"}
	if cfg.IsEmpty() {
		t.Error("Config with hosts should return false for IsEmpty")
	}
}

func TestHostsConfigGetHost(t *testing.T) {
	cfg := NewHostsConfig()
	cfg.Hosts["primary"] = HostConfig{Name: "primary", URL: "http://primary"}
	cfg.Hosts["secondary"] = HostConfig{Name: "secondary", URL: "http://secondary"}

	// Test existing host
	host, ok := cfg.GetHost("primary")
	if !ok {
		t.Error("Expected to find 'primary' host")
	}
	if host.URL != "http://primary" {
		t.Errorf("Expected URL 'http://primary', got %q", host.URL)
	}

	// Test non-existing host
	_, ok = cfg.GetHost("nonexistent")
	if ok {
		t.Error("Expected not to find 'nonexistent' host")
	}
}

func TestHostsConfigGetDefaultHost(t *testing.T) {
	cfg := NewHostsConfig()
	cfg.Hosts["primary"] = HostConfig{Name: "primary", URL: "http://primary"}
	cfg.DefaultHost = "primary"

	// Test with default set
	host, ok := cfg.GetDefaultHost()
	if !ok {
		t.Error("Expected to find default host")
	}
	if host.URL != "http://primary" {
		t.Errorf("Expected URL 'http://primary', got %q", host.URL)
	}

	// Test with no default set
	cfg.DefaultHost = ""
	_, ok = cfg.GetDefaultHost()
	if ok {
		t.Error("Expected not to find default host when not set")
	}
}

func TestHostStatus(t *testing.T) {
	tests := []struct {
		status      HostStatus
		isAvailable bool
	}{
		{HostStatusUnknown, false},
		{HostStatusConnecting, false},
		{HostStatusHealthy, true},
		{HostStatusUnhealthy, false},
		{HostStatusUnavailable, false},
		{HostStatusDisconnected, false},
	}

	for _, tc := range tests {
		if tc.status.IsAvailable() != tc.isAvailable {
			t.Errorf("Status %q IsAvailable() = %v, want %v", tc.status, tc.status.IsAvailable(), tc.isAvailable)
		}
	}
}

func TestHostConnection(t *testing.T) {
	cfg := HostConfig{
		Name:     "test",
		URL:      "http://test",
		Priority: 1,
	}

	conn := NewHostConnection(cfg)

	if conn.Status != HostStatusUnknown {
		t.Errorf("Initial status should be Unknown, got %q", conn.Status)
	}
	if conn.IsHealthy() {
		t.Error("New connection should not be healthy")
	}

	// Test MarkHealthy
	conn.MarkHealthy(100 * time.Millisecond)
	if !conn.IsHealthy() {
		t.Error("Connection should be healthy after MarkHealthy")
	}
	if conn.Latency != 100*time.Millisecond {
		t.Errorf("Latency should be 100ms, got %v", conn.Latency)
	}
	if conn.LastError != nil {
		t.Error("LastError should be nil after MarkHealthy")
	}

	// Test MarkUnhealthy
	testErr := fmt.Errorf("test error")
	conn.MarkUnhealthy(testErr)
	if conn.IsHealthy() {
		t.Error("Connection should not be healthy after MarkUnhealthy")
	}
	if conn.LastError != testErr {
		t.Errorf("LastError should be set to test error")
	}

	// Test MarkUnavailable
	conn.MarkUnavailable(testErr)
	if conn.Status != HostStatusUnavailable {
		t.Errorf("Status should be Unavailable, got %q", conn.Status)
	}
}

func TestHostConnectionToHostInfo(t *testing.T) {
	cfg := HostConfig{
		Name:     "test",
		URL:      "http://test",
		Priority: 1,
		Fallback: "backup",
	}

	conn := NewHostConnection(cfg)
	conn.MarkHealthy(50 * time.Millisecond)
	conn.Models = []string{"model1", "model2"}

	info := conn.ToHostInfo(true)

	if info.Name != "test" {
		t.Errorf("Name should be 'test', got %q", info.Name)
	}
	if info.URL != "http://test" {
		t.Errorf("URL should be 'http://test', got %q", info.URL)
	}
	if info.Status != HostStatusHealthy {
		t.Errorf("Status should be Healthy, got %q", info.Status)
	}
	if info.Latency != 50*time.Millisecond {
		t.Errorf("Latency should be 50ms, got %v", info.Latency)
	}
	if !info.IsDefault {
		t.Error("IsDefault should be true")
	}
	if info.Fallback != "backup" {
		t.Errorf("Fallback should be 'backup', got %q", info.Fallback)
	}
	if len(info.Models) != 2 {
		t.Errorf("Models count should be 2, got %d", len(info.Models))
	}
}
