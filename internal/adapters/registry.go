package adapters

import (
	"fmt"

	"github.com/mcpfleet/mcpfleet/internal/adapters/claudecode"
	"github.com/mcpfleet/mcpfleet/internal/adapters/crush"
	"github.com/mcpfleet/mcpfleet/internal/adapters/cursor"
	"github.com/mcpfleet/mcpfleet/internal/adapters/windsurf"
	"github.com/mcpfleet/mcpfleet/internal/adapters/zed"
)

// all registered adapters, keyed by their canonical name.
var all = map[string]Adapter{
	"cursor":      cursor.New(),
	"claude-code": claudecode.New(),
	"windsurf":    windsurf.New(),
	"zed":         zed.New(),
	"crush":       crush.New(),
}

// Get returns the adapter for the given agent name, or an error if unknown.
func Get(name string) (Adapter, error) {
	a, ok := all[name]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q (supported: cursor, claude-code, windsurf, zed, crush)", name)
	}
	return a, nil
}

// All returns every registered adapter.
func All() []Adapter {
	list := make([]Adapter, 0, len(all))
	for _, a := range all {
		list = append(list, a)
	}
	return list
}

// Names returns the canonical names of all registered adapters.
func Names() []string {
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	return names
}
