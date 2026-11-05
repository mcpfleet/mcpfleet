package adapters_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/mcpfleet/mcpfleet/internal/adapters"
)

var knownAgents = []string{"claude-code", "claude-desktop", "crush", "cursor", "windsurf", "zed"}

func TestGet_KnownAgents(t *testing.T) {
	for _, name := range knownAgents {
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
	want := "supported: " + strings.Join(knownAgents, ", ")
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not list agents as %q", err, want)
	}
}

func TestAll_ReturnsAllAdapters(t *testing.T) {
	all := adapters.All()
	if len(all) != len(knownAgents) {
		t.Fatalf("All() returned %d adapters, want %d", len(all), len(knownAgents))
	}
	for i, a := range all {
		if a.Name() != knownAgents[i] {
			t.Errorf("All()[%d].Name() = %q, want %q (sorted by name)", i, a.Name(), knownAgents[i])
		}
	}
}

func TestNames_SortedAndComplete(t *testing.T) {
	names := adapters.Names()
	if !sort.StringsAreSorted(names) {
		t.Errorf("Names() not sorted: %v", names)
	}
	if strings.Join(names, ",") != strings.Join(knownAgents, ",") {
		t.Errorf("Names() = %v, want %v", names, knownAgents)
	}
}
