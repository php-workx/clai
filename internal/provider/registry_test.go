package provider

import (
	"context"
	"testing"
)

// MockProvider is a mock implementation of Provider for testing
type MockProvider struct {
	name      string
	available bool
}

func (m *MockProvider) Name() string {
	return m.name
}

func (m *MockProvider) Available() bool {
	return m.available
}

func (m *MockProvider) TextToCommand(_ context.Context, req *TextToCommandRequest) (*TextToCommandResponse, error) {
	return &TextToCommandResponse{
		Suggestions:  []Suggestion{{Text: "mock command", Source: "ai", Score: 1.0, Risk: "safe"}},
		ProviderName: m.name,
		LatencyMs:    10,
	}, nil
}

func (m *MockProvider) NextStep(_ context.Context, req *NextStepRequest) (*NextStepResponse, error) {
	return &NextStepResponse{
		Suggestions:  []Suggestion{{Text: "mock next step", Source: "ai", Score: 1.0, Risk: "safe"}},
		ProviderName: m.name,
		LatencyMs:    10,
	}, nil
}

func (m *MockProvider) Diagnose(_ context.Context, req *DiagnoseRequest) (*DiagnoseResponse, error) {
	return &DiagnoseResponse{
		Explanation:  "Mock explanation",
		Fixes:        []Suggestion{{Text: "mock fix", Source: "ai", Score: 1.0, Risk: "safe"}},
		ProviderName: m.name,
		LatencyMs:    10,
	}, nil
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry() returned nil")
	}
	if r.providers == nil {
		t.Error("NewRegistry() created registry with nil providers map")
	}
	if r.preferred != "auto" {
		t.Errorf("NewRegistry() preferred = %q, want %q", r.preferred, "auto")
	}

	// Should have default provider registered (only anthropic/Claude CLI)
	if _, ok := r.providers["anthropic"]; !ok {
		t.Error("NewRegistry() missing anthropic provider")
	}
}

func TestNewRegistryWithPreference(t *testing.T) {
	r := NewRegistryWithPreference("anthropic")
	if r.preferred != "anthropic" {
		t.Errorf("NewRegistryWithPreference() preferred = %q, want %q", r.preferred, "anthropic")
	}
}

func TestRegistry_Register(t *testing.T) {
	r := &Registry{
		providers: make(map[string]Provider),
		preferred: "auto",
	}

	mock := &MockProvider{name: "test", available: true}
	r.Register(mock)

	if _, ok := r.providers["test"]; !ok {
		t.Error("Register() did not add provider to registry")
	}
}

func TestRegistry_SetPreferred(t *testing.T) {
	r := NewRegistry()
	r.SetPreferred("anthropic")
	if r.preferred != "anthropic" {
		t.Errorf("SetPreferred() preferred = %q, want %q", r.preferred, "anthropic")
	}
}

func TestRegistry_GetPreferred(t *testing.T) {
	r := NewRegistry()
	r.preferred = "anthropic"
	if r.GetPreferred() != "anthropic" {
		t.Errorf("GetPreferred() = %q, want %q", r.GetPreferred(), "anthropic")
	}
}

func TestRegistry_Get(t *testing.T) {
	r := &Registry{
		providers: make(map[string]Provider),
		preferred: "auto",
	}

	mock := &MockProvider{name: "test", available: true}
	r.providers["test"] = mock

	p, ok := r.Get("test")
	if !ok {
		t.Error("Get() returned false for existing provider")
	}
	if p != mock {
		t.Error("Get() returned wrong provider")
	}

	_, ok = r.Get("nonexistent")
	if ok {
		t.Error("Get() returned true for nonexistent provider")
	}
}

func TestRegistry_GetBest_SpecificProvider(t *testing.T) {
	r := &Registry{
		providers: make(map[string]Provider),
		preferred: "test",
	}

	mock := &MockProvider{name: "test", available: true}
	r.providers["test"] = mock

	p, err := r.GetBest()
	if err != nil {
		t.Fatalf("GetBest() error = %v", err)
	}
	if p.Name() != "test" {
		t.Errorf("GetBest() returned provider %q, want %q", p.Name(), "test")
	}
}

func TestRegistry_GetBest_SpecificProvider_Unavailable(t *testing.T) {
	r := &Registry{
		providers: make(map[string]Provider),
		preferred: "test",
	}

	mock := &MockProvider{name: "test", available: false}
	r.providers["test"] = mock

	_, err := r.GetBest()
	if err == nil {
		t.Error("GetBest() should return error for unavailable specific provider")
	}
}

func TestRegistry_GetBest_SpecificProvider_NotRegistered(t *testing.T) {
	r := &Registry{
		providers: make(map[string]Provider),
		preferred: "nonexistent",
	}

	_, err := r.GetBest()
	if err == nil {
		t.Error("GetBest() should return error for non-registered provider")
	}
}

func TestRegistry_GetBest_Auto(t *testing.T) {
	r := &Registry{
		providers: make(map[string]Provider),
		preferred: "auto",
	}

	// Register anthropic provider (the only supported provider)
	r.providers["anthropic"] = &MockProvider{name: "anthropic", available: true}

	p, err := r.GetBest()
	if err != nil {
		t.Fatalf("GetBest() error = %v", err)
	}

	// Should return anthropic
	if p.Name() != "anthropic" {
		t.Errorf("GetBest() returned provider %q, want %q", p.Name(), "anthropic")
	}
}

func TestRegistry_GetBest_Auto_Available(t *testing.T) {
	r := &Registry{
		providers: make(map[string]Provider),
		preferred: "auto",
	}

	r.providers["anthropic"] = &MockProvider{name: "anthropic", available: true}

	p, err := r.GetBest()
	if err != nil {
		t.Fatalf("GetBest() error = %v", err)
	}

	// Should return anthropic (only supported provider)
	if p.Name() != "anthropic" {
		t.Errorf("GetBest() returned provider %q, want %q", p.Name(), "anthropic")
	}
}

func TestRegistry_GetBest_Auto_NoneAvailable(t *testing.T) {
	r := &Registry{
		providers: make(map[string]Provider),
		preferred: "auto",
	}

	r.providers["anthropic"] = &MockProvider{name: "anthropic", available: false}

	_, err := r.GetBest()
	if err == nil {
		t.Error("GetBest() should return error when no providers available")
	}
}

func TestRegistry_GetBest_EmptyPreference(t *testing.T) {
	r := &Registry{
		providers: make(map[string]Provider),
		preferred: "",
	}

	r.providers["anthropic"] = &MockProvider{name: "anthropic", available: true}

	p, err := r.GetBest()
	if err != nil {
		t.Fatalf("GetBest() error = %v", err)
	}

	// Empty preference should be treated as auto
	if p.Name() != "anthropic" {
		t.Errorf("GetBest() returned provider %q, want %q", p.Name(), "anthropic")
	}
}

func TestRegistry_ListAvailable(t *testing.T) {
	r := &Registry{
		providers: make(map[string]Provider),
		preferred: "auto",
	}

	r.providers["available1"] = &MockProvider{name: "available1", available: true}
	r.providers["unavailable"] = &MockProvider{name: "unavailable", available: false}
	r.providers["available2"] = &MockProvider{name: "available2", available: true}

	available := r.ListAvailable()

	if len(available) != 2 {
		t.Errorf("ListAvailable() returned %d providers, want %d", len(available), 2)
	}

	// Check that both available providers are in the list
	found := make(map[string]bool)
	for _, name := range available {
		found[name] = true
	}
	if !found["available1"] {
		t.Error("ListAvailable() missing available1")
	}
	if !found["available2"] {
		t.Error("ListAvailable() missing available2")
	}
	if found["unavailable"] {
		t.Error("ListAvailable() should not include unavailable")
	}
}

func TestRegistry_ListAll(t *testing.T) {
	r := &Registry{
		providers: make(map[string]Provider),
		preferred: "auto",
	}

	r.providers["available"] = &MockProvider{name: "available", available: true}
	r.providers["unavailable"] = &MockProvider{name: "unavailable", available: false}

	status := r.ListAll()

	if len(status) != 2 {
		t.Errorf("ListAll() returned %d providers, want %d", len(status), 2)
	}
	if !status["available"] {
		t.Error("ListAll() should show available as true")
	}
	if status["unavailable"] {
		t.Error("ListAll() should show unavailable as false")
	}
}

func TestProviderPriority(t *testing.T) {
	expected := []string{"anthropic"}

	if len(ProviderPriority) != len(expected) {
		t.Fatalf("ProviderPriority has %d items, want %d", len(ProviderPriority), len(expected))
	}

	for i, name := range expected {
		if ProviderPriority[i] != name {
			t.Errorf("ProviderPriority[%d] = %q, want %q", i, ProviderPriority[i], name)
		}
	}
}

func TestDefaultRegistry(t *testing.T) {
	if DefaultRegistry == nil {
		t.Fatal("DefaultRegistry is nil")
	}
}

func TestGetDefaultProvider(t *testing.T) {
	// Save original registry
	original := DefaultRegistry
	defer func() { DefaultRegistry = original }()

	// Create test registry with mock provider
	// Use "anthropic" as the name since it's in the priority list
	DefaultRegistry = &Registry{
		providers: make(map[string]Provider),
		preferred: "auto",
	}
	DefaultRegistry.providers["anthropic"] = &MockProvider{name: "anthropic", available: true}

	p, err := GetDefaultProvider()
	if err != nil {
		t.Fatalf("GetDefaultProvider() error = %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("GetDefaultProvider() returned provider %q, want %q", p.Name(), "anthropic")
	}
}

func TestSetDefaultPreference(t *testing.T) {
	// Save original registry
	original := DefaultRegistry
	defer func() { DefaultRegistry = original }()

	DefaultRegistry = NewRegistry()
	SetDefaultPreference("anthropic")

	if DefaultRegistry.preferred != "anthropic" {
		t.Errorf("SetDefaultPreference() preferred = %q, want %q", DefaultRegistry.preferred, "anthropic")
	}
}

func TestGetProvider(t *testing.T) {
	// Save original registry
	original := DefaultRegistry
	defer func() { DefaultRegistry = original }()

	DefaultRegistry = &Registry{
		providers: make(map[string]Provider),
		preferred: "auto",
	}
	mock := &MockProvider{name: "test", available: true}
	DefaultRegistry.providers["test"] = mock

	p, ok := GetProvider("test")
	if !ok {
		t.Error("GetProvider() returned false for existing provider")
	}
	if p != mock {
		t.Error("GetProvider() returned wrong provider")
	}
}

func TestListAvailableProviders(t *testing.T) {
	// Save original registry
	original := DefaultRegistry
	defer func() { DefaultRegistry = original }()

	DefaultRegistry = &Registry{
		providers: make(map[string]Provider),
		preferred: "auto",
	}
	DefaultRegistry.providers["available"] = &MockProvider{name: "available", available: true}
	DefaultRegistry.providers["unavailable"] = &MockProvider{name: "unavailable", available: false}

	available := ListAvailableProviders()

	if len(available) != 1 {
		t.Errorf("ListAvailableProviders() returned %d, want %d", len(available), 1)
	}
	if available[0] != "available" {
		t.Errorf("ListAvailableProviders()[0] = %q, want %q", available[0], "available")
	}
}
