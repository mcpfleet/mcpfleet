package adapters_test

import (
	"maps"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/mcpfleet/mcpfleet/internal/adapters"
	"github.com/mcpfleet/mcpfleet/internal/schema"
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

// supportedTransports pins which transports each adapter writes to its
// config file; the rest are skipped by apply.
var supportedTransports = map[string][]string{
	"claude-code":    {"", "stdio", "http", "sse"},
	"claude-desktop": {"", "stdio"},
	"crush":          {"", "stdio", "http", "sse"},
	"cursor":         {"", "stdio", "http", "sse"},
	"windsurf":       {"", "stdio", "http", "sse"},
	"zed":            {"", "stdio", "http"},
}

func TestSupportsTransport(t *testing.T) {
	for _, a := range adapters.All() {
		want, ok := supportedTransports[a.Name()]
		if !ok {
			t.Fatalf("adapter %q has no entry in supportedTransports", a.Name())
		}
		for _, transport := range []string{"", "stdio", "http", "sse", "ws"} {
			if got := a.SupportsTransport(transport); got != slices.Contains(want, transport) {
				t.Errorf("%s.SupportsTransport(%q) = %v, want %v", a.Name(), transport, got, !got)
			}
		}
	}
}

func TestPartition(t *testing.T) {
	servers := []schema.Server{
		{Name: "local", Command: "npx"},
		{Name: "local-explicit", Transport: "stdio", Command: "npx"},
		{Name: "remote-http", Transport: "http", URL: "https://example.com/mcp"},
		{Name: "remote-sse", Transport: "sse", URL: "https://example.com/sse"},
		{Name: "no-url", Transport: "http"},
	}
	names := func(list []schema.Server) []string {
		var out []string
		for _, s := range list {
			out = append(out, s.Name)
		}
		return out
	}

	t.Run("adapter supporting every transport keeps remote servers", func(t *testing.T) {
		a, _ := adapters.Get("claude-code")
		apply, skipped := adapters.Partition(a, servers)
		if got, want := names(apply), []string{"local", "local-explicit", "remote-http", "remote-sse"}; !slices.Equal(got, want) {
			t.Errorf("apply = %v, want %v", got, want)
		}
		if len(skipped) != 1 || skipped[0].Server.Name != "no-url" {
			t.Fatalf("skipped = %+v, want only no-url", skipped)
		}
		if want := "server has transport http but no url"; skipped[0].Reason != want {
			t.Errorf("reason = %q, want %q", skipped[0].Reason, want)
		}
	})

	t.Run("adapter supporting only stdio skips remote servers", func(t *testing.T) {
		a, _ := adapters.Get("claude-desktop")
		apply, skipped := adapters.Partition(a, servers)
		if got, want := names(apply), []string{"local", "local-explicit"}; !slices.Equal(got, want) {
			t.Errorf("apply = %v, want %v", got, want)
		}
		got := map[string]string{}
		for _, s := range skipped {
			got[s.Server.Name] = s.Reason
		}
		want := map[string]string{
			"remote-http": "claude-desktop does not support http servers in its config file",
			"remote-sse":  "claude-desktop does not support sse servers in its config file",
			"no-url":      "claude-desktop does not support http servers in its config file",
		}
		if !maps.Equal(got, want) {
			t.Errorf("skipped = %v, want %v", got, want)
		}
	})

	t.Run("zed skips sse only", func(t *testing.T) {
		a, _ := adapters.Get("zed")
		apply, skipped := adapters.Partition(a, servers)
		if got, want := names(apply), []string{"local", "local-explicit", "remote-http"}; !slices.Equal(got, want) {
			t.Errorf("apply = %v, want %v", got, want)
		}
		if got, want := names(skippedServers(skipped)), []string{"remote-sse", "no-url"}; !slices.Equal(got, want) {
			t.Errorf("skipped = %v, want %v", got, want)
		}
	})
}

func skippedServers(skipped []adapters.Skipped) []schema.Server {
	out := make([]schema.Server, len(skipped))
	for i, s := range skipped {
		out[i] = s.Server
	}
	return out
}
