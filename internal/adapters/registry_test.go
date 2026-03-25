package adapters_test

import (
	"testing"

	"github.com/mcpfleet/mcpfleet/internal/adapters"
)

func TestGet_KnownAgents(t *testing.T) {
	known := []string{"cursor", "claude-code", "windsurf", "zed", "crush"}
	for _, name := range known {
		t.Run(name, func(t *testing.T) {
			a, err := adapters.Get(name)
			if err != nil {
				t.Fatalf("Get(%q) returned unexpected error: %v", name, err)
			}
			if a == nil {
				t.Fatalf("Get(%q) returned nil adapter", name)
			}
			if a.Name() != name {
				t.Errorf("Name() = %q, want %q", a.Name(), name)
			}
			if a.ConfigPath() == "" {
				t.Errorf("ConfigPath() is empty for agent %q", name)
			}
		})
	}
}

func TestGet_UnknownAgent(t *testing.T) {
	_, err := adapters.Get("nonexistent-agent")
	if err == nil {
		t.Fatal("expected error for unknown agent, got nil")
	}
}

func TestAll_ReturnsAllAdapters(t *testing.T) {
	all := adapters.All()
	if len(all) == 0 {
		t.Fatal("All() returned empty slice")
	}
	expectedCount := 5
	if len(all) != expectedCount {
		t.Errorf("All() returned %d adapters, want %d", len(all), expectedCount)
	}
	for _, a := range all {
		if a.Name() == "" {
			t.Error("adapter with empty name found")
		}
	}
}

func TestNames_ContainsAllAgents(t *testing.T) {
	names := adapters.Names()
	expected := map[string]bool{
		"cursor":     true,
		"claude-code": true,
		"windsurf":   true,
		"zed":        true,
		"crush":      true,
	}
	for _, n := range names {
		if !expected[n] {
			t.Errorf("unexpected agent name: %q", n)
		}
		delete(expected, n)
	}
	for missing := range expected {
		t.Errorf("missing agent name: %q", missing)
	}
}
