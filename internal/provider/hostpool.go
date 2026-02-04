package provider

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// HostPool manages multiple LLM host connections with fallback and health checking
type HostPool struct {
	config      HostsConfig
	connections map[string]*HostConnection
	mu          sync.RWMutex

	// Health check configuration
	healthCheckInterval time.Duration
	healthCheckCancel   context.CancelFunc
}

// NewHostPool creates a new host pool from configuration
func NewHostPool(cfg HostsConfig) *HostPool {
	pool := &HostPool{
		config:              cfg,
		connections:         make(map[string]*HostConnection),
		healthCheckInterval: 30 * time.Second,
	}

	// Create connections for each configured host
	for name, hostCfg := range cfg.Hosts {
		hostCfg.Name = name // ensure name is set
		pool.connections[name] = NewHostConnection(hostCfg)
	}

	return pool
}

// Connect establishes a connection to a specific host
func (p *HostPool) Connect(ctx context.Context, name string) error {
	p.mu.Lock()
	conn, ok := p.connections[name]
	if !ok {
		p.mu.Unlock()
		return fmt.Errorf("host %q not found", name)
	}
	conn.Status = HostStatusConnecting
	p.mu.Unlock()

	// Create provider for this host
	timeout := conn.Config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	connCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	prov, err := New(connCtx, conn.Config.URL, conn.Config.Vendor, conn.Config.APIKey)
	latency := time.Since(start)

	p.mu.Lock()
	defer p.mu.Unlock()

	if err != nil {
		conn.MarkUnavailable(err)
		return fmt.Errorf("failed to connect to host %q: %w", name, err)
	}

	conn.Provider = prov
	conn.MarkHealthy(latency)

	// Detect available models
	models, err := prov.DetectModels(ctx)
	if err == nil {
		conn.Models = models
	}

	return nil
}

// ConnectAll establishes connections to all configured hosts
func (p *HostPool) ConnectAll(ctx context.Context) error {
	p.mu.RLock()
	names := make([]string, 0, len(p.connections))
	for name := range p.connections {
		names = append(names, name)
	}
	p.mu.RUnlock()

	// Sort by priority (lower number first)
	sort.Slice(names, func(i, j int) bool {
		p.mu.RLock()
		defer p.mu.RUnlock()
		return p.connections[names[i]].Config.Priority < p.connections[names[j]].Config.Priority
	})

	var wg sync.WaitGroup
	errChan := make(chan error, len(names))

	for _, name := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			if err := p.Connect(ctx, n); err != nil {
				errChan <- err
			}
		}(name)
	}

	wg.Wait()
	close(errChan)

	// Collect errors but don't fail if at least one host connected
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	// Check if we have at least one healthy connection
	p.mu.RLock()
	hasHealthy := false
	for _, conn := range p.connections {
		if conn.IsHealthy() {
			hasHealthy = true
			break
		}
	}
	p.mu.RUnlock()

	if !hasHealthy && len(errs) > 0 {
		return fmt.Errorf("no healthy hosts available: %w", errs[0])
	}

	return nil
}

// GetProvider returns the provider for a specific host
func (p *HostPool) GetProvider(name string) (Provider, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	conn, ok := p.connections[name]
	if !ok {
		return nil, fmt.Errorf("host %q not found", name)
	}

	if !conn.IsHealthy() {
		return nil, fmt.Errorf("host %q is not healthy (status: %s)", name, conn.Status)
	}

	if conn.Provider == nil {
		return nil, fmt.Errorf("host %q has no active provider", name)
	}

	return conn.Provider, nil
}

// GetProviderWithFallback returns a provider for the specified host, using fallback if unavailable
func (p *HostPool) GetProviderWithFallback(name string) (Provider, string, error) {
	p.mu.RLock()
	conn, ok := p.connections[name]
	if !ok {
		p.mu.RUnlock()
		return nil, "", fmt.Errorf("host %q not found", name)
	}

	// Try primary host while still holding the lock
	if conn.IsHealthy() && conn.Provider != nil {
		prov := conn.Provider
		p.mu.RUnlock()
		return prov, name, nil
	}

	// Get fallback name while holding lock
	fallbackName := conn.Config.Fallback
	p.mu.RUnlock()

	// Try fallback if configured
	if fallbackName != "" {
		fallbackProv, err := p.GetProvider(fallbackName)
		if err == nil {
			return fallbackProv, fallbackName, nil
		}
	}

	// Try any healthy host by priority
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Collect healthy connections sorted by priority
	type hostPriority struct {
		name     string
		priority int
		prov     Provider
	}
	var healthy []hostPriority
	for n, c := range p.connections {
		if c.IsHealthy() && c.Provider != nil {
			healthy = append(healthy, hostPriority{name: n, priority: c.Config.Priority, prov: c.Provider})
		}
	}

	if len(healthy) == 0 {
		return nil, "", fmt.Errorf("no healthy hosts available")
	}

	// Sort by priority
	sort.Slice(healthy, func(i, j int) bool {
		return healthy[i].priority < healthy[j].priority
	})

	return healthy[0].prov, healthy[0].name, nil
}

// GetDefault returns the provider for the default host
func (p *HostPool) GetDefault() (Provider, error) {
	if p.config.DefaultHost == "" {
		return nil, fmt.Errorf("no default host configured")
	}
	return p.GetProvider(p.config.DefaultHost)
}

// GetDefaultWithFallback returns the default provider with fallback support
func (p *HostPool) GetDefaultWithFallback() (Provider, string, error) {
	if p.config.DefaultHost == "" {
		// Try to find any healthy host
		return p.GetProviderWithFallback("")
	}
	return p.GetProviderWithFallback(p.config.DefaultHost)
}

// StartHealthChecks begins periodic health checking for all hosts
func (p *HostPool) StartHealthChecks(ctx context.Context) {
	p.mu.Lock()
	// Stop any existing health check goroutine
	if p.healthCheckCancel != nil {
		p.healthCheckCancel()
	}
	ctx, cancel := context.WithCancel(ctx)
	p.healthCheckCancel = cancel
	interval := p.healthCheckInterval
	p.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.checkAllHealth(ctx)
			}
		}
	}()
}

// StopHealthChecks stops the periodic health checking
func (p *HostPool) StopHealthChecks() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.healthCheckCancel != nil {
		p.healthCheckCancel()
		p.healthCheckCancel = nil
	}
}

// checkAllHealth performs health checks on all hosts
func (p *HostPool) checkAllHealth(ctx context.Context) {
	p.mu.RLock()
	names := make([]string, 0, len(p.connections))
	for name := range p.connections {
		names = append(names, name)
	}
	p.mu.RUnlock()

	for _, name := range names {
		p.CheckHealth(ctx, name)
	}
}

// CheckHealth performs a health check on a specific host
func (p *HostPool) CheckHealth(ctx context.Context, name string) bool {
	p.mu.RLock()
	conn, ok := p.connections[name]
	p.mu.RUnlock()

	if !ok {
		return false
	}

	// If no provider, try to connect
	if conn.Provider == nil {
		err := p.Connect(ctx, name)
		return err == nil
	}

	// Perform a lightweight health check by detecting models
	start := time.Now()
	_, err := conn.Provider.DetectModels(ctx)
	latency := time.Since(start)

	p.mu.Lock()
	defer p.mu.Unlock()

	if err != nil {
		conn.MarkUnhealthy(err)
		return false
	}

	conn.MarkHealthy(latency)
	return true
}

// IsHealthy returns true if the specified host is healthy
func (p *HostPool) IsHealthy(name string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	conn, ok := p.connections[name]
	if !ok {
		return false
	}
	return conn.IsHealthy()
}

// GetHostInfo returns display information for all hosts
func (p *HostPool) GetHostInfo() []HostInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var infos []HostInfo
	for name, conn := range p.connections {
		isDefault := name == p.config.DefaultHost
		infos = append(infos, conn.ToHostInfo(isDefault))
	}

	// Sort by priority, then name
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].Priority != infos[j].Priority {
			return infos[i].Priority < infos[j].Priority
		}
		return infos[i].Name < infos[j].Name
	})

	return infos
}

// GetConnection returns a host connection by name
func (p *HostPool) GetConnection(name string) (*HostConnection, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	conn, ok := p.connections[name]
	return conn, ok
}

// GetDefaultHostName returns the name of the default host
func (p *HostPool) GetDefaultHostName() string {
	return p.config.DefaultHost
}

// GetConfig returns the hosts configuration
func (p *HostPool) GetConfig() HostsConfig {
	return p.config
}

// SetHealthCheckInterval sets the interval between health checks.
// Must be called before StartHealthChecks or during initialization.
func (p *HostPool) SetHealthCheckInterval(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.healthCheckInterval = d
}

// Close stops health checks and releases all connections
func (p *HostPool) Close() {
	p.StopHealthChecks()

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, conn := range p.connections {
		conn.Provider = nil
		conn.Status = HostStatusDisconnected
	}
}

// Reconnect attempts to reconnect to all unhealthy hosts
func (p *HostPool) Reconnect(ctx context.Context) error {
	p.mu.RLock()
	unhealthy := make([]string, 0)
	for name, conn := range p.connections {
		if !conn.IsHealthy() {
			unhealthy = append(unhealthy, name)
		}
	}
	p.mu.RUnlock()

	var lastErr error
	for _, name := range unhealthy {
		if err := p.Connect(ctx, name); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// HostCount returns the number of configured hosts
func (p *HostPool) HostCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.connections)
}

// HealthyCount returns the number of healthy hosts
func (p *HostPool) HealthyCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, conn := range p.connections {
		if conn.IsHealthy() {
			count++
		}
	}
	return count
}
