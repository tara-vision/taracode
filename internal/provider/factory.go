package provider

import (
	"context"
	"fmt"
)

// New creates the provider for vendor; an empty or "auto" vendor is detected (Detect). A host where nothing answers
// is an *llm.UnreachableError.
func New(ctx context.Context, host, vendor, apiKey string) (Provider, error) {
	if host == "" {
		return nil, fmt.Errorf("host is required")
	}
	providerType := ParseVendorConfig(vendor)
	if providerType == TypeUnknown {
		detected, err := Detect(ctx, host)
		if err != nil {
			return nil, err
		}
		providerType = detected
	}
	return NewWithType(providerType, host, apiKey)
}

// NewWithType creates a provider with an explicit type (no auto-detection); an unknown type gets the
// OpenAI-compatible provider.
func NewWithType(providerType Type, host, apiKey string) (Provider, error) {
	if host == "" {
		return nil, fmt.Errorf("host is required")
	}
	switch providerType {
	case TypeTaraengine:
		return NewTaraengineProvider(host, apiKey), nil
	case TypeOllama:
		return NewOllamaProvider(host, apiKey), nil
	case TypeLlamaCpp:
		return NewLlamaCppProvider(host, apiKey), nil
	}
	return NewVLLMProvider(host, apiKey), nil
}
