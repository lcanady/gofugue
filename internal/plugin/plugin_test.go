package plugin_test

import (
	"context"
	"testing"

	"github.com/kumakun/gofugue/internal/plugin"
)

// stubPlugin is a minimal Plugin implementation for tests.
type stubPlugin struct {
	name     string
	initErr  error
	shutErr  error
	initCalled bool
	shutCalled bool
}

func (s *stubPlugin) Name() string { return s.name }
func (s *stubPlugin) Init(_ context.Context, _ plugin.API) error {
	s.initCalled = true
	return s.initErr
}
func (s *stubPlugin) Shutdown() error {
	s.shutCalled = true
	return s.shutErr
}

func TestRegister_And_All(t *testing.T) {
	// Note: registry is global, so we can only check the count grows.
	before := len(plugin.All())
	plugin.Register(&stubPlugin{name: "test-plugin"})
	after := len(plugin.All())
	if after != before+1 {
		t.Errorf("expected %d plugins after Register, got %d", before+1, after)
	}
}

func TestAll_ContainsRegisteredPlugin(t *testing.T) {
	p := &stubPlugin{name: "unique-plugin-xyz"}
	plugin.Register(p)

	var found bool
	for _, pl := range plugin.All() {
		if pl.Name() == "unique-plugin-xyz" {
			found = true
			break
		}
	}
	if !found {
		t.Error("registered plugin not found in All()")
	}
}

func TestPlugin_Name(t *testing.T) {
	p := &stubPlugin{name: "my-plugin"}
	if p.Name() != "my-plugin" {
		t.Errorf("Name() = %q, want %q", p.Name(), "my-plugin")
	}
}

func TestPlugin_Init_Called(t *testing.T) {
	p := &stubPlugin{name: "init-test"}
	err := p.Init(context.Background(), nil)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !p.initCalled {
		t.Error("Init was not called")
	}
}

func TestPlugin_Shutdown_Called(t *testing.T) {
	p := &stubPlugin{name: "shutdown-test"}
	err := p.Shutdown()
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if !p.shutCalled {
		t.Error("Shutdown was not called")
	}
}
