package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"

	"github.com/mcpfleet/mcpfleet/internal/adapters"
	"github.com/mcpfleet/mcpfleet/internal/adapters/skilldir"
	"github.com/mcpfleet/mcpfleet/internal/registry"
	"github.com/mcpfleet/mcpfleet/internal/schema"
	"github.com/mcpfleet/mcpfleet/internal/skillstate"
)

// plannedSkill is a skill apply installs: its registry record and, for an
// agent with skills support, its archive and the hash the registry sent.
type plannedSkill struct {
	skill   registry.Skill
	archive []byte
	sha     string
}

// fetchSkills fetches the skills named by --skill (or every skill with
// --all-skills) and, when adapter supports skills, their archives.
func fetchSkills(ctx context.Context, client *registry.Client, adapter adapters.Adapter, names []string, all bool) ([]plannedSkill, error) {
	var list []registry.Skill
	switch {
	case all:
		var err error
		if list, err = client.ListSkills(ctx); err != nil {
			return nil, fmt.Errorf("list skills: %w", err)
		}
	default:
		seen := map[string]bool{}
		for _, name := range names {
			if seen[name] {
				continue
			}
			seen[name] = true
			s, err := client.GetSkill(ctx, name)
			if err != nil {
				if errors.Is(err, registry.ErrNotFound) {
					return nil, fmt.Errorf("skill %q not found in registry", name)
				}
				return nil, fmt.Errorf("get skill %q: %w", name, err)
			}
			list = append(list, *s)
		}
	}

	plan := make([]plannedSkill, 0, len(list))
	_, supported := adapter.(adapters.SkillAdapter)
	for _, s := range list {
		p := plannedSkill{skill: s}
		if supported {
			archive, sha, err := client.GetSkillArchive(ctx, s.Name)
			if err != nil {
				return nil, fmt.Errorf("download skill %q: %w", s.Name, err)
			}
			if sha == "" {
				return nil, fmt.Errorf("download skill %q: the registry sent no sha256 (X-Skill-Sha256 header), so the archive cannot be verified", s.Name)
			}
			p.archive, p.sha = archive, sha
		}
		plan = append(plan, p)
	}
	return plan, nil
}

// addRequiredServers returns selected plus every server a planned skill
// requires that selected lacks, looked up by name in all. A required server
// missing from the registry is an error.
func addRequiredServers(selected, all []schema.Server, plan []plannedSkill) ([]schema.Server, error) {
	have := make(map[string]bool, len(selected))
	for _, s := range selected {
		have[s.Name] = true
	}
	byName := make(map[string]schema.Server, len(all))
	for _, s := range all {
		byName[s.Name] = s
	}
	out := selected
	for _, p := range plan {
		for _, name := range p.skill.Requires {
			if have[name] {
				continue
			}
			s, ok := byName[name]
			if !ok {
				return nil, fmt.Errorf("skill %q requires server %q, which is not in the registry", p.skill.Name, name)
			}
			log.Debug("Adding required server", "server", name, "skill", p.skill.Name)
			out = append(out, s)
			have[name] = true
		}
	}
	return out, nil
}

func loadSkillState() (*skillstate.State, error) {
	dir, err := registry.ConfigDir()
	if err != nil {
		return nil, err
	}
	return skillstate.Load(filepath.Join(dir, skillstate.FileName))
}

// applySkills installs plan on adapter following skillstate.Decide, records
// what it installed in state and reports each skill. A conflict leaves the
// local copy alone; any conflict makes the result an error.
func applySkills(cmd *cobra.Command, adapter adapters.Adapter, plan []plannedSkill, state *skillstate.State, force bool) error {
	if len(plan) == 0 {
		return nil
	}
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	agent := adapter.Name()
	sa, ok := adapter.(adapters.SkillAdapter)
	if !ok {
		for _, p := range plan {
			fmt.Fprintf(errOut, "warning: agent %s does not support skills, skipping skill %s\n", agent, p.skill.Name)
		}
		return nil
	}

	installed, err := sa.ListSkills()
	if err != nil {
		return fmt.Errorf("read installed skills: %w", err)
	}
	local := make(map[string]skilldir.InstalledSkill, len(installed))
	for _, s := range installed {
		local[s.Name] = s
	}

	conflicts := 0
	var failed error
	for _, p := range plan {
		name := p.skill.Name
		target := filepath.Join(sa.SkillsDir(), name)
		localHash := ""
		if s, ok := local[name]; ok {
			if s.HashErr != nil && !force {
				conflicts++
				fmt.Fprintf(errOut, "conflict: skill %s at %s cannot be compared with the registry (%v); use --force to replace it\n", name, target, s.HashErr)
				continue
			}
			localHash = s.SHA256
			if localHash == "" {
				localHash = "unreadable"
			}
		} else if _, err := os.Lstat(target); err == nil {
			localHash = "not a skill directory"
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		switch skillstate.Decide(localHash, state.Hash(agent, name), p.sha, force) {
		case skillstate.Skip:
			state.Record(agent, name, p.sha, time.Now())
			fmt.Fprintf(out, "Skill %s is up to date\n", name)
			continue
		case skillstate.Conflict:
			conflicts++
			fmt.Fprintf(errOut, "conflict: skill %s at %s was changed locally since mcpfleet installed it; use --force to replace it\n", name, target)
			continue
		}
		verb := "Installed"
		if localHash != "" {
			verb = "Updated"
		}
		if err := sa.ApplySkill(name, p.archive, p.sha); err != nil {
			failed = err
			break
		}
		state.Record(agent, name, p.sha, time.Now())
		fmt.Fprintf(out, "✓ %s skill %s %s -> %s\n", verb, name, shortHash(p.sha), target)
	}
	if err := state.Save(); err != nil {
		return fmt.Errorf("save skills state: %w", err)
	}
	if failed != nil {
		return failed
	}
	if conflicts > 0 {
		return fmt.Errorf("%d skill conflict(s); nothing was changed for them", conflicts)
	}
	return nil
}
