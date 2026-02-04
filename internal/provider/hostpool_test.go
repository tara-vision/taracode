package provider

import (
	"context"
	"testing"
	"time"
)

func TestNewHostPool(t *testing.T) {
	cfg := NewHostsConfig()
	cfg.Hosts["primary"] = HostConfig{
		Name:     "primary",
		URL:      "http://localhost:11434",
		Priority: 1,
	}
	cfg.Hosts["secondary"] = HostConfig{
		Name:     "secondary",
		URL:      "http://localhost:11435",
		Fallback: "primary",
		Priority: 2,
	}
	cfg.DefaultHost = "primary"

	pool := NewHostPool(cfg)

	if pool.HostCount() != 2 {
		t.Errorf("Expected 2 hosts, got %d", pool.HostCount())
	}
	if pool.GetDefaultHostName() != "primary" {
		t.Errorf("Expected default host 'primary', got %q", pool.GetDefaultHostName())
	}
}

func TestHostPoolGetConnection(t *testing.T) {
	cfg := NewHostsConfig()
	cfg.Hosts["test"] = HostConfig{
		Name: "test",
		URL:  "http://localhost:11434",
	}

	pool := NewHostPool(cfg)

	conn, ok := pool.GetConnection("test")
	if !ok {
		t.Error("Expected to find connection 'test'")
	}
	if conn.Config.Name != "test" {
		t.Errorf("Expected connection name 'test', got %q", conn.Config.Name)
	}

	_, ok = pool.GetConnection("nonexistent")
	if ok {
		t.Error("Expected not to find connection 'nonexistent'")
	}
}

func TestHostPoolIsHealthy(t *testing.T) {
	cfg := NewHostsConfig()
	cfg.Hosts["test"] = HostConfig{
		Name: "test",
		URL:  "http://localhost:11434",
	}

	pool := NewHostPool(cfg)

	// Initially not healthy
	if pool.IsHealthy("test") {
		t.Error("Host should not be healthy initially")
	}

	// Manually mark as healthy for testing
	conn, _ := pool.GetConnection("test")
	conn.MarkHealthy(10 * time.Millisecond)

	if !pool.IsHealthy("test") {
		t.Error("Host should be healthy after marking")
	}

	// Check non-existent host
	if pool.IsHealthy("nonexistent") {
		t.Error("Non-existent host should not be healthy")
	}
}

func TestHostPoolHealthyCount(t *testing.T) {
	cfg := NewHostsConfig()
	cfg.Hosts["host1"] = HostConfig{Name: "host1", URL: "http://h1"}
	cfg.Hosts["host2"] = HostConfig{Name: "host2", URL: "http://h2"}
	cfg.Hosts["host3"] = HostConfig{Name: "host3", URL: "http://h3"}

	pool := NewHostPool(cfg)

	if pool.HealthyCount() != 0 {
		t.Errorf("Expected 0 healthy hosts initially, got %d", pool.HealthyCount())
	}

	// Mark some as healthy
	conn1, _ := pool.GetConnection("host1")
	conn1.MarkHealthy(10 * time.Millisecond)

	if pool.HealthyCount() != 1 {
		t.Errorf("Expected 1 healthy host, got %d", pool.HealthyCount())
	}

	conn2, _ := pool.GetConnection("host2")
	conn2.MarkHealthy(10 * time.Millisecond)

	if pool.HealthyCount() != 2 {
		t.Errorf("Expected 2 healthy hosts, got %d", pool.HealthyCount())
	}
}

func TestHostPoolGetHostInfo(t *testing.T) {
	cfg := NewHostsConfig()
	cfg.Hosts["primary"] = HostConfig{
		Name:     "primary",
		URL:      "http://primary",
		Priority: 1,
	}
	cfg.Hosts["secondary"] = HostConfig{
		Name:     "secondary",
		URL:      "http://secondary",
		Priority: 2,
	}
	cfg.DefaultHost = "primary"

	pool := NewHostPool(cfg)

	infos := pool.GetHostInfo()

	if len(infos) != 2 {
		t.Errorf("Expected 2 host infos, got %d", len(infos))
	}

	// Check sorted by priority
	if infos[0].Name != "primary" {
		t.Errorf("First host should be 'primary' (priority 1), got %q", infos[0].Name)
	}
	if infos[1].Name != "secondary" {
		t.Errorf("Second host should be 'secondary' (priority 2), got %q", infos[1].Name)
	}

	// Check default marker
	if !infos[0].IsDefault {
		t.Error("Primary should be marked as default")
	}
	if infos[1].IsDefault {
		t.Error("Secondary should not be marked as default")
	}
}

func TestHostPoolClose(t *testing.T) {
	cfg := NewHostsConfig()
	cfg.Hosts["test"] = HostConfig{Name: "test", URL: "http://test"}

	pool := NewHostPool(cfg)

	// Mark as healthy first
	conn, _ := pool.GetConnection("test")
	conn.MarkHealthy(10 * time.Millisecond)

	pool.Close()

	// Check that connection is disconnected
	conn, _ = pool.GetConnection("test")
	if conn.Status != HostStatusDisconnected {
		t.Errorf("Expected status Disconnected after close, got %q", conn.Status)
	}
}

func TestHostPoolSetHealthCheckInterval(t *testing.T) {
	cfg := NewHostsConfig()
	cfg.Hosts["test"] = HostConfig{Name: "test", URL: "http://test"}

	pool := NewHostPool(cfg)
	pool.SetHealthCheckInterval(5 * time.Second)

	if pool.healthCheckInterval != 5*time.Second {
		t.Errorf("Expected interval 5s, got %v", pool.healthCheckInterval)
	}
}

func TestHostPoolStartStopHealthChecks(t *testing.T) {
	cfg := NewHostsConfig()
	cfg.Hosts["test"] = HostConfig{Name: "test", URL: "http://test"}

	pool := NewHostPool(cfg)
	pool.SetHealthCheckInterval(100 * time.Millisecond)

	ctx := context.Background()
	pool.StartHealthChecks(ctx)

	// Health check should be running
	if pool.healthCheckCancel == nil {
		t.Error("Health check cancel func should be set")
	}

	// Stop health checks
	pool.StopHealthChecks()

	if pool.healthCheckCancel != nil {
		t.Error("Health check cancel func should be nil after stop")
	}
}

func TestHostPoolGetConfig(t *testing.T) {
	cfg := NewHostsConfig()
	cfg.Hosts["test"] = HostConfig{Name: "test", URL: "http://test"}
	cfg.DefaultHost = "test"

	pool := NewHostPool(cfg)
	poolCfg := pool.GetConfig()

	if poolCfg.DefaultHost != "test" {
		t.Errorf("Expected default host 'test', got %q", poolCfg.DefaultHost)
	}
	if len(poolCfg.Hosts) != 1 {
		t.Errorf("Expected 1 host in config, got %d", len(poolCfg.Hosts))
	}
}
