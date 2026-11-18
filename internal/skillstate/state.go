// Package skillstate records which skill content mcpfleet last installed on
// each agent, so apply can tell a copy it may replace from one the user
// changed. The record lives in mcpfleet's config directory, never inside a
// skill directory, so an installed skill stays byte for byte the registry's.
package skillstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// FileName is the state file's name inside mcpfleet's config directory.
const FileName = "skills-state.json"

// Action is what apply does with one skill on one agent.
type Action int

const (
	// Install writes a skill the agent does not have.
	Install Action = iota
	// Overwrite replaces the local copy: it is unchanged since mcpfleet last
	// installed it, or --force was given.
	Overwrite
	// Skip leaves the local copy alone: it already is the registry's.
	Skip
	// Conflict leaves the local copy alone and reports it: it differs from
	// both the registry and what mcpfleet last installed.
	Conflict
)

func (a Action) String() string {
	switch a {
	case Install:
		return "install"
	case Overwrite:
		return "overwrite"
	case Skip:
		return "skip"
	case Conflict:
		return "conflict"
	}
	return fmt.Sprintf("Action(%d)", int(a))
}

// Decide returns the action for a skill whose local copy hashes to local
// ("" when there is none), whose last installed hash is state ("" when
// mcpfleet never installed it) and whose registry hash is remote.
func Decide(local, state, remote string, force bool) Action {
	switch {
	case local == "":
		return Install
	case local == remote:
		return Skip
	case local == state, force:
		return Overwrite
	}
	return Conflict
}

// Entry is the record of one installed skill.
type Entry struct {
	SHA256      string    `json:"sha256"`
	InstalledAt time.Time `json:"installed_at"`
}

// State maps agent name -> skill name -> the last installed entry.
type State struct {
	path   string
	agents map[string]map[string]Entry
}

// Load reads the state file at path; a missing file is an empty state.
func Load(path string) (*State, error) {
	s := &State{path: path, agents: map[string]map[string]Entry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.agents); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if s.agents == nil {
		s.agents = map[string]map[string]Entry{}
	}
	return s, nil
}

// Hash returns the hash last installed for skill on agent, or "".
func (s *State) Hash(agent, skill string) string {
	return s.agents[agent][skill].SHA256
}

// Record notes that skill with hash sha is installed on agent.
func (s *State) Record(agent, skill, sha string, at time.Time) {
	if s.agents[agent] == nil {
		s.agents[agent] = map[string]Entry{}
	}
	s.agents[agent][skill] = Entry{SHA256: sha, InstalledAt: at.UTC()}
}

// Save writes the state file (0600, its directory 0700), replacing it
// atomically.
func (s *State) Save() error {
	data, err := json.MarshalIndent(s.agents, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+FileName+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
