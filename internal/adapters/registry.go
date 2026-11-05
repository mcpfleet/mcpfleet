package adapters

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mcpfleet/mcpfleet/internal/adapters/claudecode"
	"github.com/mcpfleet/mcpfleet/internal/adapters/claudedesktop"
	"github.com/mcpfleet/mcpfleet/internal/adapters/crush"
	"github.com/mcpfleet/mcpfleet/internal/adapters/cursor"
	"github.com/mcpfleet/mcpfleet/internal/adapters/windsurf"
	"github.com/mcpfleet/mcpfleet/internal/adapters/zed"
)

// all registered adapters, keyed by their canonical name.
var all = map[string]Adapter{
	"claude-code":    claudecode.New(),
	"claude-desktop": claudedesktop.New(),
	"crush":          crush.New(),
	"cursor":         cursor.New(),
	"windsurf":       windsurf.New(),
	"zed":            zed.New(),
}

// Get returns the adapter for the given agent name, or an error if unknown.
func Get(name string) (Adapter, error) {
	a, ok := all[name]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q (supported: %s)", name, strings.Join(Names(), ", "))
	}
	return a, nil
}

// All returns every registered adapter, sorted by name.
func All() []Adapter {
	names := Names()
	list := make([]Adapter, 0, len(names))
	for _, name := range names {
		list = append(list, all[name])
	}
	return list
}

// Names returns the canonical names of all registered adapters, sorted.
func Names() []string {
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
