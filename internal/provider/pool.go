package provider

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Pool manages multiple provider instances for different models
type Pool struct {
	defaultProvider Provider
	providers       map[string]Provider // model name -> provider
	host            string
	apiKey          string
	mu              sync.RWMutex
}

// NewPool creates a new provider pool with a default provider
func NewPool(defaultProvider Provider, host, apiKey string) *Pool {
	return &Pool{
		defaultProvider: defaultProvider,
		providers:       make(map[string]Provider),
		host:            host,
		apiKey:          apiKey,
	}
}

// GetDefault returns the default provider
func (p *Pool) GetDefault() Provider {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.defaultProvider
}

// Get returns a provider for the specified model
// If no specific provider exists, returns the default provider
func (p *Pool) Get(model string) Provider {
	p.mu.RLock()
	prov, ok := p.providers[model]
	p.mu.RUnlock()

	if ok {
		return prov
	}

	// Return default provider but set its model
	return p.defaultProvider
}

// GetOrCreate returns a provider for the specified model, creating one if needed
func (p *Pool) GetOrCreate(model string) (Provider, error) {
	return p.GetOrCreateWithContext(context.Background(), model)
}

// GetOrCreateWithContext returns a provider for the specified model, creating one if needed
func (p *Pool) GetOrCreateWithContext(ctx context.Context, model string) (Provider, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if we already have this provider
	if prov, ok := p.providers[model]; ok {
		return prov, nil
	}

	// Create a new provider for this model
	// For now, we use the same host but different model
	prov, err := New(ctx, p.host, "", p.apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider for model %s: %w", model, err)
	}

	prov.SetModel(model)
	p.providers[model] = prov

	return prov, nil
}

// SetModel sets the model for a specific provider
// If the provider doesn't exist, creates one
func (p *Pool) SetModel(providerKey, model string) error {
	return p.SetModelWithContext(context.Background(), providerKey, model)
}

// SetModelWithContext sets the model for a specific provider with context
func (p *Pool) SetModelWithContext(ctx context.Context, providerKey, model string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if prov, ok := p.providers[providerKey]; ok {
		prov.SetModel(model)
		return nil
	}

	// Create new provider with this model
	prov, err := New(ctx, p.host, "", p.apiKey)
	if err != nil {
		return err
	}

	prov.SetModel(model)
	p.providers[providerKey] = prov

	return nil
}

// Preload ensures providers for specified models are ready
func (p *Pool) Preload(ctx context.Context, models []string) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(models))

	for _, model := range models {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()

			_, err := p.GetOrCreate(m)
			if err != nil {
				errChan <- fmt.Errorf("failed to preload %s: %w", m, err)
			}
		}(model)
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errs[0] // Return first error
	}

	return nil
}

// GetInfo returns info about all providers in the pool
func (p *Pool) GetInfo() []Info {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var infos []Info

	// Add default provider
	if p.defaultProvider != nil {
		infos = append(infos, *p.defaultProvider.Info())
	}

	// Add other providers
	for model, prov := range p.providers {
		info := *prov.Info()
		info.Model = model // Ensure model is set correctly
		infos = append(infos, info)
	}

	return infos
}

// ListAvailableModels returns all models that can be used
func (p *Pool) ListAvailableModels(ctx context.Context) ([]string, error) {
	if p.defaultProvider == nil {
		return nil, fmt.Errorf("no default provider")
	}

	return p.defaultProvider.DetectModels(ctx)
}

// Close releases resources for all providers
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Clear providers
	p.providers = make(map[string]Provider)
}

// ModelSwitcher provides a convenient interface for switching models
type ModelSwitcher struct {
	pool         *Pool
	currentModel string
	mu           sync.RWMutex
}

// NewModelSwitcher creates a new model switcher
func NewModelSwitcher(pool *Pool, defaultModel string) *ModelSwitcher {
	return &ModelSwitcher{
		pool:         pool,
		currentModel: defaultModel,
	}
}

// Switch changes the current model
func (s *ModelSwitcher) Switch(model string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.pool.GetOrCreate(model)
	if err != nil {
		return err
	}

	s.currentModel = model
	return nil
}

// Current returns the current model name
func (s *ModelSwitcher) Current() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentModel
}

// Provider returns the provider for the current model
func (s *ModelSwitcher) Provider() Provider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pool.Get(s.currentModel)
}

// SwitchTime is used for timing model switches
type SwitchTime struct {
	Model    string
	Duration time.Duration
}

// SwitchWithTiming switches model and returns timing info
func (s *ModelSwitcher) SwitchWithTiming(model string) (*SwitchTime, error) {
	start := time.Now()
	err := s.Switch(model)
	duration := time.Since(start)

	return &SwitchTime{
		Model:    model,
		Duration: duration,
	}, err
}
