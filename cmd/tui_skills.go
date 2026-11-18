package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mcpfleet/mcpfleet/internal/adapters"
	"github.com/mcpfleet/mcpfleet/internal/adapters/skilldir"
	"github.com/mcpfleet/mcpfleet/internal/registry"
	"github.com/mcpfleet/mcpfleet/internal/skillstate"
	"github.com/mcpfleet/mcpfleet/internal/tui"
)

// tuiSkills serves the TUI's Skills tab with the code "mcpfleet apply
// --skill" runs.
type tuiSkills struct{}

var _ tui.SkillBackend = tuiSkills{}

// LoadSkills lists the registry's skills with their state on every agent.
func (tuiSkills) LoadSkills(ctx context.Context) (tui.SkillsData, error) {
	client, err := registry.New()
	if err != nil {
		return tui.SkillsData{}, err
	}
	list, err := client.ListSkills(ctx)
	if err != nil {
		return tui.SkillsData{}, fmt.Errorf("list skills: %w", err)
	}
	state, err := loadSkillState()
	if err != nil {
		return tui.SkillsData{}, err
	}

	data := tui.SkillsData{Agents: adapters.Names(), Skills: make([]tui.SkillRow, len(list))}
	for i, s := range list {
		data.Skills[i] = tui.SkillRow{
			Name:        s.Name,
			Description: s.Description,
			Requires:    s.Requires,
			States:      make(map[string]skillstate.Status, len(data.Agents)),
		}
	}
	for _, a := range adapters.All() {
		agent := a.Name()
		sa, ok := a.(adapters.SkillAdapter)
		if !ok {
			for _, row := range data.Skills {
				row.States[agent] = skillstate.Unsupported
			}
			continue
		}
		installed, err := sa.ListSkills()
		if err != nil {
			return tui.SkillsData{}, fmt.Errorf("read skills of %s: %w", agent, err)
		}
		local := make(map[string]skilldir.InstalledSkill, len(installed))
		for _, s := range installed {
			local[s.Name] = s
		}
		for i, s := range list {
			st, err := skillStatus(local, sa.SkillsDir(), s, state.Hash(agent, s.Name))
			if err != nil {
				return tui.SkillsData{}, err
			}
			data.Skills[i].States[agent] = st
		}
	}
	return data, nil
}

// skillStatus returns the state of the registry skill s in the skills
// directory dir, which holds the skills local, given the hash mcpfleet last
// installed there (recorded). It follows the rule applySkills decides by.
func skillStatus(local map[string]skilldir.InstalledSkill, dir string, s registry.Skill, recorded string) (skillstate.Status, error) {
	if l, ok := local[s.Name]; ok {
		if !l.Result.Valid() {
			return skillstate.NotConformant, nil
		}
		if l.HashErr != nil {
			return skillstate.Modified, nil
		}
	}
	hash, err := localSkillHash(local, filepath.Join(dir, s.Name), s.Name)
	if err != nil {
		return 0, err
	}
	if hash == notASkillDir {
		return skillstate.NotConformant, nil
	}
	return skillstate.StatusOf(hash, recorded, s.SHA256), nil
}

// tuiConflict is the error of a TUI apply that left a changed skill alone;
// it is skillstate.ErrConflict.
type tuiConflict string

func (c tuiConflict) Error() string { return string(c) }

func (tuiConflict) Is(target error) bool { return target == skillstate.ErrConflict }

// ApplySkill does what "mcpfleet apply --skill <skill> <agent>" does for
// that skill, except that only the servers the skill requires are written
// (merged into the agent config), not every server in the registry.
func (tuiSkills) ApplySkill(ctx context.Context, agent, skill string, force bool) (string, error) {
	adapter, err := adapters.Get(agent)
	if err != nil {
		return "", err
	}
	if _, ok := adapter.(adapters.SkillAdapter); !ok {
		return "", fmt.Errorf("%s does not support skills", agent)
	}
	client, err := registry.New()
	if err != nil {
		return "", err
	}
	all, err := client.ListServers(ctx)
	if err != nil {
		return "", err
	}
	plan, err := fetchSkills(ctx, client, adapter, []string{skill}, false)
	if err != nil {
		return "", err
	}
	servers, err := addRequiredServers(nil, all, plan)
	if err != nil {
		return "", err
	}
	servers, skipped := adapters.Partition(adapter, servers)
	servers, err = resolveSecrets(servers, openVault)
	if err != nil {
		return "", err
	}
	if len(servers) > 0 {
		if err := adapter.Apply(servers); err != nil {
			return "", fmt.Errorf("apply: %w", err)
		}
	}
	state, err := loadSkillState()
	if err != nil {
		return "", err
	}

	var out, errOut bytes.Buffer
	if err := applySkills(&out, &errOut, adapter, plan, state, force); err != nil {
		if errors.Is(err, skillstate.ErrConflict) {
			line := strings.TrimSuffix(lastLine(errOut.String()), "; use --force to replace it")
			return "", tuiConflict(line)
		}
		return "", err
	}

	summary := lastLine(out.String())
	if len(servers) > 0 {
		summary += fmt.Sprintf(" (with %d required server(s))", len(servers))
	}
	for _, s := range skipped {
		summary += fmt.Sprintf("; skipped server %s: %s", s.Server.Name, s.Reason)
	}
	return summary, nil
}

// lastLine returns the last non-empty line of s.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
