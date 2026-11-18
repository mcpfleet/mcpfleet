package claudecode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/mcpfleet/pkg/skillspec"
)

const notesMD = "---\nname: notes\ndescription: Take notes in a markdown file.\n---\n# Notes\n"

func packNotes(t *testing.T) ([]byte, string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "notes")
	require.NoError(t, os.MkdirAll(src, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "SKILL.md"), []byte(notesMD), 0o644))
	archive, err := skillspec.Pack(src, "notes")
	require.NoError(t, err)
	sha, err := skillspec.Hash(src)
	require.NoError(t, err)
	return archive, sha
}

func TestSkillsDir_IsClaudeCodeUserSkills(t *testing.T) {
	home := isolate(t)
	assert.Equal(t, filepath.Join(home, ".claude", "skills"), New().SkillsDir())
}

func TestSkillsDir_HonoursClaudeConfigDir(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	assert.Equal(t, filepath.Join(dir, "skills"), New().SkillsDir())
}

func TestApplySkill_DirectoryNamedAfterSkill(t *testing.T) {
	home := isolate(t)
	archive, sha := packNotes(t)

	require.NoError(t, New().ApplySkill("notes", archive, sha))

	got, err := skillspec.Hash(filepath.Join(home, ".claude", "skills", "notes"))
	require.NoError(t, err)
	assert.Equal(t, sha, got)
}

func TestApplySkill_RelativeClaudeConfigDirRefused(t *testing.T) {
	isolate(t)
	t.Setenv("CLAUDE_CONFIG_DIR", "rel")
	archive, sha := packNotes(t)
	err := New().ApplySkill("notes", archive, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CLAUDE_CONFIG_DIR must be an absolute path")
}

func TestListSkills(t *testing.T) {
	home := isolate(t)
	archive, sha := packNotes(t)
	a := New()
	require.NoError(t, a.ApplySkill("notes", archive, sha))
	// A skill the user installed by hand, which breaks the name rule.
	mine := filepath.Join(home, ".claude", "skills", "mine")
	require.NoError(t, os.MkdirAll(mine, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(mine, "SKILL.md"), []byte("---\nname: Mine\ndescription: d\n---\n"), 0o644))

	list, err := a.ListSkills()
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "mine", list[0].Name)
	assert.False(t, list[0].Result.Valid())
	assert.Equal(t, "notes", list[1].Name)
	assert.True(t, list[1].Result.Valid())
	assert.Equal(t, sha, list[1].SHA256)
}
