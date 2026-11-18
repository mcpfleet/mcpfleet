package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/mcpfleet/internal/adapters"
	"github.com/mcpfleet/mcpfleet/internal/skillstate"
	"github.com/mcpfleet/mcpfleet/internal/tui"
)

func loadSkillRow(t *testing.T, name string) (tui.SkillsData, tui.SkillRow) {
	t.Helper()
	data, err := tuiSkills{}.LoadSkills(context.Background())
	require.NoError(t, err)
	for _, s := range data.Skills {
		if s.Name == name {
			return data, s
		}
	}
	t.Fatalf("skill %s not loaded: %+v", name, data.Skills)
	return data, tui.SkillRow{}
}

func TestTUISkills_LoadSkillsStates(t *testing.T) {
	_, _, home := skillApplySetup(t)

	data, row := loadSkillRow(t, "gh-notes")
	assert.Equal(t, adapters.Names(), data.Agents)
	assert.Equal(t, []string{"github"}, row.Requires)
	assert.Equal(t, "Keep notes on GitHub issues.", row.Description)
	assert.Equal(t, skillstate.NotInstalled, row.States["claude-code"])
	assert.Equal(t, skillstate.NotInstalled, row.States["windsurf"])
	for _, agent := range []string{"claude-desktop", "crush", "cursor", "zed"} {
		assert.Equal(t, skillstate.Unsupported, row.States[agent], agent)
	}

	code, _, errOut := applySkill(t, "--skill", "gh-notes", "claude-code")
	require.Equal(t, 0, code, errOut)
	_, row = loadSkillRow(t, "gh-notes")
	assert.Equal(t, skillstate.UpToDate, row.States["claude-code"])

	pushNewVersion(t)
	_, row = loadSkillRow(t, "gh-notes")
	assert.Equal(t, skillstate.OutOfDate, row.States["claude-code"])

	installed := filepath.Join(claudeSkillsDir(home), "gh-notes")
	require.NoError(t, os.WriteFile(filepath.Join(installed, "references", "mine.md"), []byte("mine\n"), 0o644))
	_, row = loadSkillRow(t, "gh-notes")
	assert.Equal(t, skillstate.Modified, row.States["claude-code"])

	// A copy that breaks the spec: SKILL.md without a description.
	windsurf := filepath.Join(home, ".codeium", "windsurf", "skills", "gh-notes")
	require.NoError(t, os.MkdirAll(windsurf, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(windsurf, "SKILL.md"), []byte("---\nname: gh-notes\n---\n"), 0o644))
	_, row = loadSkillRow(t, "gh-notes")
	assert.Equal(t, skillstate.NotConformant, row.States["windsurf"])
}

func TestTUISkills_LoadSkillsNotASkillDirectory(t *testing.T) {
	_, _, home := skillApplySetup(t)
	require.NoError(t, os.MkdirAll(claudeSkillsDir(home), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(claudeSkillsDir(home), "gh-notes"), []byte("a file\n"), 0o644))
	_, row := loadSkillRow(t, "gh-notes")
	assert.Equal(t, skillstate.NotConformant, row.States["claude-code"])
}

func TestTUISkills_ApplyInstallsSkillWithRequiredServersOnly(t *testing.T) {
	_, src, home := skillApplySetup(t)

	summary, err := tuiSkills{}.ApplySkill(context.Background(), "claude-code", "gh-notes", false)
	require.NoError(t, err)
	installed := filepath.Join(claudeSkillsDir(home), "gh-notes")
	assert.Contains(t, summary, "Installed skill gh-notes")
	assert.Equal(t, treeOf(t, src), treeOf(t, installed))
	servers := agentServers(t, "claude-code")
	assert.Contains(t, servers, "github", "the required server is applied")
	assert.NotContains(t, servers, "fs", "servers the skill does not require are not")

	_, row := loadSkillRow(t, "gh-notes")
	assert.Equal(t, skillstate.UpToDate, row.States["claude-code"])
}

func TestTUISkills_ApplyConflictThenForce(t *testing.T) {
	_, _, home := skillApplySetup(t)
	_, err := tuiSkills{}.ApplySkill(context.Background(), "claude-code", "gh-notes", false)
	require.NoError(t, err)
	installed := filepath.Join(claudeSkillsDir(home), "gh-notes")
	edited := filepath.Join(installed, "references", "mine.md")
	require.NoError(t, os.WriteFile(edited, []byte("mine\n"), 0o644))
	v2 := pushNewVersion(t)

	_, err = tuiSkills{}.ApplySkill(context.Background(), "claude-code", "gh-notes", false)
	require.Error(t, err)
	assert.True(t, errors.Is(err, skillstate.ErrConflict), "a conflict is skillstate.ErrConflict: %v", err)
	assert.Contains(t, err.Error(), "skill gh-notes at "+installed+" was changed locally")
	assert.NotContains(t, err.Error(), "--force", "the TUI asks instead of naming the flag")
	assert.FileExists(t, edited)

	summary, err := tuiSkills{}.ApplySkill(context.Background(), "claude-code", "gh-notes", true)
	require.NoError(t, err)
	assert.Contains(t, summary, "Updated skill gh-notes")
	assert.Equal(t, treeOf(t, v2), treeOf(t, installed))
}

func TestTUISkills_ApplyToAgentWithoutSkillsSupport(t *testing.T) {
	_, _, _ = skillApplySetup(t)
	_, err := tuiSkills{}.ApplySkill(context.Background(), "cursor", "gh-notes", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cursor does not support skills")
	a, aerr := adapters.Get("cursor")
	require.NoError(t, aerr)
	assert.NoFileExists(t, a.ConfigPath(), "nothing is written")
}

func TestApplySkill_ConflictErrorIsErrConflict(t *testing.T) {
	assert.True(t, errors.Is(skillConflictError(2), skillstate.ErrConflict))
	assert.Equal(t, "2 skill conflict(s); nothing was changed for them", skillConflictError(2).Error())
}
