package provider

import (
	"fmt"
	"sort"
	"sync"
)

// Registry manages available AI providers and handles provider selection
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	preferred string // User-specified preferred provider
}

// ProviderPriority defines the order of provider selection when in "auto" mode
// Currently only Claude CLI (via Anthropic provider) is supported
var ProviderPriority = []string{"anthropic"}

// NewRegistry creates a new provider registry with default providers
func NewRegistry() *Registry {
	r := &Registry{
		providers: make(map[string]Provider),
		preferred: "auto",
	}

	// Register default provider (Claude CLI via Anthropic)
	r.Register(NewAnthropicProvider())

	return r
}

// NewRegistryWithPreference creates a registry with a preferred provider
func NewRegistryWithPreference(preferred string) *Registry {
	r := NewRegistry()
	r.preferred = preferred
	return r
}

// Register adds a provider to the registry
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// SetPreferred sets the preferred provider
// Use "auto" to automatically select the best available provider
func (r *Registry) SetPreferred(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.preferred = name
}

// GetPreferred returns the current preferred provider setting
func (r *Registry) GetPreferred() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.preferred
}

// Get returns a specific provider by name
func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

// GetBest returns the best available provider based on configuration
// If preferred is set to a specific provider, that provider is returned if available
// If preferred is "auto", providers are tried in order of ProviderPriority
func (r *Registry) GetBest() (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// If a specific provider is preferred
	if r.preferred != "" && r.preferred != "auto" {
		p, ok := r.providers[r.preferred]
		if !ok {
			return nil, fmt.Errorf("provider %q not registered", r.preferred)
		}
		if !p.Available() {
			return nil, fmt.Errorf("provider %q is not available", r.preferred)
		}
		return p, nil
	}

	// Auto-select: try providers in priority order
	for _, name := range ProviderPriority {
		if p, ok := r.providers[name]; ok && p.Available() {
			return p, nil
		}
	}

	return nil, fmt.Errorf("no AI providers available")
}

// ListAvailable returns a list of all available providers
func (r *Registry) ListAvailable() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var available []string
	for name, p := range r.providers {
		if p.Available() {
			available = append(available, name)
		}
	}
	sort.Strings(available)
	return available
}

// ListAll returns a list of all registered providers with their availability status
func (r *Registry) ListAll() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	status := make(map[string]bool)
	for name, p := range r.providers {
		status[name] = p.Available()
	}
	return status
}

// DefaultRegistry is the package-level default registry
var DefaultRegistry = NewRegistry()

// GetDefaultProvider returns the best available provider from the default registry
func GetDefaultProvider() (Provider, error) {
	return DefaultRegistry.GetBest()
}

// SetDefaultPreference sets the preferred provider in the default registry
func SetDefaultPreference(name string) {
	DefaultRegistry.SetPreferred(name)
}

// GetProvider returns a specific provider from the default registry
func GetProvider(name string) (Provider, bool) {
	return DefaultRegistry.Get(name)
}

// ListAvailableProviders returns available providers from the default registry
func ListAvailableProviders() []string {
	return DefaultRegistry.ListAvailable()
}
