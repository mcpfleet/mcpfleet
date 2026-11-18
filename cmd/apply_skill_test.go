package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/mcpfleet/internal/adapters"
	"github.com/mcpfleet/mcpfleet/internal/schema"
)

const ghNotesSkillMD = "---\nname: gh-notes\ndescription: Keep notes on GitHub issues.\nmetadata:\n  mcpfleet-requires: github\n---\n# GitHub notes\n\nUse the github server.\n"

// skillApplySetup starts a registry holding the servers github (tag vcs)
// and fs (tag dev) and the skill gh-notes that requires github, pushed with
// "skill push". It returns the registry, the pushed source directory (its
// .git removed, so it is what the registry holds) and the test's home.
func skillApplySetup(t *testing.T) (*skillRegistry, string, string) {
	t.Helper()
	reg := newSkillRegistry(t, "github", "fs")
	reg.serverDefs = []schema.Server{
		{Name: "github", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-github"}, Tags: []string{"vcs"}},
		{Name: "fs", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem"}, Tags: []string{"dev"}},
	}
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	isolateAgentConfigs(t, home)
	t.Setenv("USERPROFILE", home)

	src := writeSkill(t, "gh-notes", ghNotesSkillMD)
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, runForTest(t, []string{"skill", "push", src}, &stdout, &stderr), stderr.String())
	require.NoError(t, os.RemoveAll(filepath.Join(src, ".git")))
	return reg, src, home
}

// pushNewVersion pushes gh-notes again with an extra file.
func pushNewVersion(t *testing.T) string {
	t.Helper()
	src := writeSkill(t, "gh-notes", ghNotesSkillMD)
	require.NoError(t, os.WriteFile(filepath.Join(src, "references", "v2.md"), []byte("v2\n"), 0o644))
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, runForTest(t, []string{"skill", "push", src}, &stdout, &stderr), stderr.String())
	require.NoError(t, os.RemoveAll(filepath.Join(src, ".git")))
	return src
}

func claudeSkillsDir(home string) string { return filepath.Join(home, ".claude", "skills") }

func agentServers(t *testing.T, agent string) map[string]map[string]any {
	t.Helper()
	a, err := adapters.Get(agent)
	require.NoError(t, err)
	return readMcpServers(t, a.ConfigPath())
}

func applySkill(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runForTest(t, append([]string{"apply"}, args...), &stdout, &stderr)
	resetFlags(rootCmd)
	return code, stdout.String(), stderr.String()
}

// TestApplySkillE2E: apply --skill installs the skill byte for byte into
// Claude Code's skills directory, writes the server it requires to Claude
// Code's config and keeps its state outside the skill directory. With
// MCPFLEET_REQUIRE_SKILLS_REF=1 (CI) the installed directory must also pass
// skills-ref validate.
func TestApplySkillE2E(t *testing.T) {
	_, src, home := skillApplySetup(t)

	code, out, errOut := applySkill(t, "--skill", "gh-notes", "claude-code")
	require.Equal(t, 0, code, errOut)
	assert.Empty(t, errOut)
	assert.Contains(t, out, "Applied 2 MCP server(s) to claude-code")
	installed := filepath.Join(claudeSkillsDir(home), "gh-notes")
	assert.Contains(t, out, "Installed skill gh-notes "+skillHashOf(t, src)[:12]+" -> "+installed)

	assert.Equal(t, treeOf(t, src), treeOf(t, installed), "skill tree is byte-identical")
	assert.Equal(t, []string{"gh-notes"}, dirEntries(t, claudeSkillsDir(home)), "no temp or state files next to the skill")
	assert.Contains(t, agentServers(t, "claude-code"), "github")

	data, err := os.ReadFile(filepath.Join(home, ".config", "mcpfleet", "skills-state.json"))
	require.NoError(t, err)
	var state map[string]map[string]struct {
		SHA256 string `json:"sha256"`
	}
	require.NoError(t, json.Unmarshal(data, &state))
	assert.Equal(t, skillHashOf(t, src), state["claude-code"]["gh-notes"].SHA256)

	if _, err := exec.LookPath("skills-ref"); err != nil {
		if os.Getenv("MCPFLEET_REQUIRE_SKILLS_REF") == "1" {
			t.Fatal("skills-ref is not on PATH")
		}
		t.Log("skills-ref not on PATH; the CI job skills-ref-parity runs this check")
		return
	}
	res, err := exec.Command("skills-ref", "validate", installed).CombinedOutput()
	assert.NoError(t, err, "skills-ref validate %s: %s", installed, res)
}

func TestApplySkill_RequiredServerAddedDespiteTagFilter(t *testing.T) {
	_, _, _ = skillApplySetup(t)

	code, out, errOut := applySkill(t, "--tag", "dev", "--skill", "gh-notes", "claude-code")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "Applied 2 MCP server(s) to claude-code")
	servers := agentServers(t, "claude-code")
	assert.Contains(t, servers, "fs")
	assert.Contains(t, servers, "github")
}

func TestApplySkill_RequiredServerNotDuplicated(t *testing.T) {
	_, _, _ = skillApplySetup(t)

	code, out, errOut := applySkill(t, "--tag", "vcs", "--skill", "gh-notes", "claude-code")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "Applied 1 MCP server(s) to claude-code")
}

func TestApplySkillUnsupportedAgent(t *testing.T) {
	_, _, home := skillApplySetup(t)

	code, out, errOut := applySkill(t, "--tag", "dev", "--skill", "gh-notes", "cursor")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "warning: agent cursor does not support skills, skipping skill gh-notes\n", errOut)
	assert.Contains(t, out, "Applied 2 MCP server(s) to cursor")
	assert.Contains(t, agentServers(t, "cursor"), "github", "required servers are applied anyway")
	_, err := os.Stat(filepath.Join(home, ".config", "mcpfleet", "skills-state.json"))
	assert.True(t, os.IsNotExist(err), "no state is recorded for an agent without skills")
}

func TestApplySkill_AlreadyCurrentIsSkipped(t *testing.T) {
	_, _, _ = skillApplySetup(t)
	code, _, errOut := applySkill(t, "--skill", "gh-notes", "claude-code")
	require.Equal(t, 0, code, errOut)

	code, out, errOut := applySkill(t, "--skill", "gh-notes", "claude-code")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "Skill gh-notes is up to date")
}

func TestApplySkill_UnchangedCopyIsUpdated(t *testing.T) {
	_, _, home := skillApplySetup(t)
	code, _, errOut := applySkill(t, "--skill", "gh-notes", "claude-code")
	require.Equal(t, 0, code, errOut)
	v2 := pushNewVersion(t)

	code, out, errOut := applySkill(t, "--skill", "gh-notes", "claude-code")
	require.Equal(t, 0, code, errOut)
	installed := filepath.Join(claudeSkillsDir(home), "gh-notes")
	assert.Contains(t, out, "Updated skill gh-notes "+skillHashOf(t, v2)[:12]+" -> "+installed)
	assert.Equal(t, treeOf(t, v2), treeOf(t, installed))
}

func TestApplySkill_LocallyModifiedCopyIsAConflict(t *testing.T) {
	_, _, home := skillApplySetup(t)
	code, _, errOut := applySkill(t, "--skill", "gh-notes", "claude-code")
	require.Equal(t, 0, code, errOut)
	installed := filepath.Join(claudeSkillsDir(home), "gh-notes")
	edited := filepath.Join(installed, "references", "mine.md")
	require.NoError(t, os.WriteFile(edited, []byte("my notes\n"), 0o644))
	v2 := pushNewVersion(t)

	code, _, errOut = applySkill(t, "--skill", "gh-notes", "claude-code")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "conflict: skill gh-notes at "+installed+" was changed locally since mcpfleet installed it; use --force to replace it")
	assert.Contains(t, errOut, "1 skill conflict(s)")
	assert.FileExists(t, edited, "the local edit is kept")
	assert.Contains(t, agentServers(t, "claude-code"), "github", "servers are applied despite the conflict")

	code, out, errOut := applySkill(t, "--force", "--skill", "gh-notes", "claude-code")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "Updated skill gh-notes")
	assert.Equal(t, treeOf(t, v2), treeOf(t, installed))
}

func TestApplySkill_UnmanagedLocalCopyIsAConflict(t *testing.T) {
	_, _, home := skillApplySetup(t)
	mine := filepath.Join(claudeSkillsDir(home), "gh-notes")
	require.NoError(t, os.MkdirAll(mine, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mine, "SKILL.md"), []byte("---\nname: gh-notes\ndescription: mine\n---\n"), 0o644))

	code, _, errOut := applySkill(t, "--skill", "gh-notes", "claude-code")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "conflict: skill gh-notes at "+mine)
	data, err := os.ReadFile(filepath.Join(mine, "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "description: mine")
}

func TestApplySkill_AllSkills(t *testing.T) {
	_, _, home := skillApplySetup(t)
	src := writeSkill(t, "notes", notesSkillMD)
	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, runForTest(t, []string{"skill", "push", src}, &stdout, &stderr), stderr.String())

	code, out, errOut := applySkill(t, "--all-skills", "--tag", "dev", "claude-code")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "Installed skill gh-notes")
	assert.Contains(t, out, "Installed skill notes")
	assert.Equal(t, []string{"gh-notes", "notes"}, dirEntries(t, claudeSkillsDir(home)))
	assert.Contains(t, agentServers(t, "claude-code"), "github")
}

func TestApplySkill_UnknownSkillWritesNothing(t *testing.T) {
	_, _, home := skillApplySetup(t)

	code, _, errOut := applySkill(t, "--skill", "nope", "claude-code")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, `skill "nope" not found in registry`)
	a, err := adapters.Get("claude-code")
	require.NoError(t, err)
	assert.NoFileExists(t, a.ConfigPath())
	assert.NoDirExists(t, claudeSkillsDir(home))
}

func TestApplySkill_MissingRequiredServerWritesNothing(t *testing.T) {
	reg, _, _ := skillApplySetup(t)
	reg.mu.Lock()
	reg.serverDefs = reg.serverDefs[1:] // github was deleted after the push
	reg.mu.Unlock()

	code, _, errOut := applySkill(t, "--skill", "gh-notes", "claude-code")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, `skill "gh-notes" requires server "github", which is not in the registry`)
	a, err := adapters.Get("claude-code")
	require.NoError(t, err)
	assert.NoFileExists(t, a.ConfigPath())
}

func TestApplySkill_SkillAndAllSkillsExclusive(t *testing.T) {
	isolateConfig(t)
	code, _, errOut := applySkill(t, "--skill", "a", "--all-skills", "claude-code")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "--skill")
	assert.Contains(t, errOut, "--all-skills")
}
