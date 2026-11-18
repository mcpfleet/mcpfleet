package skilldir

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/mcpfleet/pkg/skillspec"
)

const notesMD = "---\nname: notes\ndescription: Take notes in a markdown file.\n---\n# Notes\n"

// writeTree writes files (path -> content) under root; a path ending in
// ".sh" gets the executable bit.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		mode := os.FileMode(0o644)
		if filepath.Ext(rel) == ".sh" {
			mode = 0o755
		}
		require.NoError(t, os.WriteFile(p, []byte(body), mode))
		require.NoError(t, os.Chmod(p, mode))
	}
}

// packSkill returns the archive and hash of a skill named name with files.
func packSkill(t *testing.T, name string, files map[string]string) ([]byte, string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), name)
	writeTree(t, src, files)
	archive, err := skillspec.Pack(src, name)
	require.NoError(t, err)
	sha, err := skillspec.Hash(src)
	require.NoError(t, err)
	return archive, sha
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := []string{}
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

func TestInstall_FreshCreatesDirectoryNamedAfterSkill(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills") // does not exist yet
	archive, sha := packSkill(t, "notes", map[string]string{"SKILL.md": notesMD, "scripts/run.sh": "#!/bin/sh\n"})

	require.NoError(t, Install(dir, "notes", archive, sha))

	got, err := skillspec.Hash(filepath.Join(dir, "notes"))
	require.NoError(t, err)
	assert.Equal(t, sha, got)
	assert.Equal(t, []string{"notes"}, entries(t, dir), "no temp directory is left behind")
	info, err := os.Stat(filepath.Join(dir, "notes", "scripts", "run.sh"))
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&0o111, "executable bit kept")
}

func TestInstall_ReplacesExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, filepath.Join(dir, "notes"), map[string]string{"SKILL.md": notesMD, "old.md": "old\n"})
	archive, sha := packSkill(t, "notes", map[string]string{"SKILL.md": notesMD, "new.md": "new\n"})

	require.NoError(t, Install(dir, "notes", archive, sha))

	assert.Equal(t, []string{"SKILL.md", "new.md"}, entries(t, filepath.Join(dir, "notes")))
	assert.Equal(t, []string{"notes"}, entries(t, dir))
}

func TestInstall_HashMismatchLeavesExistingCopy(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, filepath.Join(dir, "notes"), map[string]string{"SKILL.md": notesMD, "old.md": "old\n"})
	archive, _ := packSkill(t, "notes", map[string]string{"SKILL.md": notesMD})

	err := Install(dir, "notes", archive, "0000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sha256 mismatch")
	assert.Equal(t, []string{"SKILL.md", "old.md"}, entries(t, filepath.Join(dir, "notes")))
	assert.Equal(t, []string{"notes"}, entries(t, dir))
}

func TestInstall_BrokenArchiveLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	err := Install(dir, "notes", []byte("not a tarball"), "x")
	require.Error(t, err)
	assert.Equal(t, []string{}, entries(t, dir))
}

func TestInstall_RejectsUnusableName(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"", ".", "..", "a/b", `a\b`} {
		assert.Error(t, Install(dir, name, nil, ""), "name %q", name)
	}
}

func TestInstall_RejectsRelativeDir(t *testing.T) {
	archive, sha := packSkill(t, "notes", map[string]string{"SKILL.md": notesMD})
	err := Install("relative/skills", "notes", archive, sha)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute")
}

func TestList_ValidatesEverySkillIncludingUnmanaged(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, filepath.Join(dir, "notes"), map[string]string{"SKILL.md": notesMD})
	// Installed by hand, broken: the name does not match the directory.
	writeTree(t, filepath.Join(dir, "mine"), map[string]string{"SKILL.md": "---\nname: other\ndescription: d\n---\n"})
	// Not skills: a hidden temp directory and a plain file.
	writeTree(t, filepath.Join(dir, ".mcpfleet-tmp-1"), map[string]string{"SKILL.md": notesMD})
	writeTree(t, dir, map[string]string{"README.md": "x"})

	list, err := List(dir)
	require.NoError(t, err)
	require.Len(t, list, 2)

	assert.Equal(t, "mine", list[0].Name)
	assert.False(t, list[0].Result.Valid())
	assert.Equal(t, "notes", list[1].Name)
	assert.True(t, list[1].Result.Valid())
	assert.Equal(t, filepath.Join(dir, "notes"), list[1].Path)
	want, err := skillspec.Hash(filepath.Join(dir, "notes"))
	require.NoError(t, err)
	assert.Equal(t, want, list[1].SHA256)
	require.NotNil(t, list[1].Skill)
	assert.Equal(t, "Take notes in a markdown file.", list[1].Skill.Description)
}

func TestList_MissingDirectoryIsEmpty(t *testing.T) {
	list, err := List(filepath.Join(t.TempDir(), "nope"))
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestLocalHash(t *testing.T) {
	dir := t.TempDir()
	sha, err := LocalHash(dir, "notes")
	require.NoError(t, err)
	assert.Equal(t, "", sha, "absent skill has no hash")

	writeTree(t, filepath.Join(dir, "notes"), map[string]string{"SKILL.md": notesMD})
	sha, err = LocalHash(dir, "notes")
	require.NoError(t, err)
	want, _ := skillspec.Hash(filepath.Join(dir, "notes"))
	assert.Equal(t, want, sha)
}

func TestLocalHash_UnhashableIsError(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, filepath.Join(dir, "notes"), map[string]string{"SKILL.md": notesMD})
	require.NoError(t, os.Symlink("/etc/hostname", filepath.Join(dir, "notes", "link")))
	_, err := LocalHash(dir, "notes")
	require.Error(t, err)
	assert.False(t, errors.Is(err, fs.ErrNotExist))
}
