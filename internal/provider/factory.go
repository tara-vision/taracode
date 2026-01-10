package provider

import (
	"context"
	"fmt"
)

// New creates a new provider based on the vendor configuration
// If vendor is empty or "auto", it will auto-detect the provider type
func New(ctx context.Context, host, vendor, apiKey string) (Provider, error) {
	if host == "" {
		return nil, fmt.Errorf("host is required")
	}

	// Parse vendor config
	providerType := ParseVendorConfig(vendor)

	// Auto-detect if not specified
	if providerType == TypeUnknown {
		providerType = Detect(ctx, host)
	}

	// Create the appropriate provider
	switch providerType {
	case TypeOllama:
		return NewOllamaProvider(host, apiKey), nil
	case TypeLlamaCpp:
		return NewLlamaCppProvider(host, apiKey), nil
	case TypeVLLM:
		return NewVLLMProvider(host, apiKey), nil
	default:
		// Default to vLLM for unknown (most compatible)
		return NewVLLMProvider(host, apiKey), nil
	}
}

// NewWithType creates a provider with an explicit type (no auto-detection)
func NewWithType(providerType Type, host, apiKey string) (Provider, error) {
	if host == "" {
		return nil, fmt.Errorf("host is required")
	}

	switch providerType {
	case TypeOllama:
		return NewOllamaProvider(host, apiKey), nil
	case TypeLlamaCpp:
		return NewLlamaCppProvider(host, apiKey), nil
	case TypeVLLM:
		return NewVLLMProvider(host, apiKey), nil
	default:
		return NewVLLMProvider(host, apiKey), nil
	}
}
