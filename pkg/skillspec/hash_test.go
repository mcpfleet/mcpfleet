package skillspec

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type file struct {
	path    string
	mode    os.FileMode
	content string
}

// writeTree writes files in the given order, so two calls with the same files
// in a different order create the same tree with a different creation order.
func writeTree(t *testing.T, files ...file) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f.path))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(f.content), f.mode))
		// WriteFile is subject to the umask; set the mode explicitly.
		require.NoError(t, os.Chmod(p, f.mode))
	}
	return dir
}

func hashOf(t *testing.T, dir string) string {
	t.Helper()
	h, err := Hash(dir)
	require.NoError(t, err)
	return h
}

func skipWithoutExecBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no executable bit")
	}
}

func TestHash_Golden(t *testing.T) {
	skipWithoutExecBits(t)
	// sha256 over "<path>\x00<mode>\x00<len>\x00<content>" for each file in
	// path order, computed outside Go.
	dir := writeTree(t,
		file{"SKILL.md", 0o644, "a\n"},
		file{"scripts/run.sh", 0o755, "echo\n"},
	)
	assert.Equal(t, "1d2c0678aa4cbe1f9d534dfefa205ea948a6da4e106f4603325983894c14b432", hashOf(t, dir))
}

func TestHash_IgnoresCreationOrder(t *testing.T) {
	a := writeTree(t,
		file{"SKILL.md", 0o644, "a\n"},
		file{"b/x.txt", 0o644, "x"},
		file{"a/y.txt", 0o644, "y"},
	)
	b := writeTree(t,
		file{"a/y.txt", 0o644, "y"},
		file{"b/x.txt", 0o644, "x"},
		file{"SKILL.md", 0o644, "a\n"},
	)
	assert.Equal(t, hashOf(t, a), hashOf(t, b))
}

func TestHash_ExecutableBitChangesHash(t *testing.T) {
	skipWithoutExecBits(t)
	plain := writeTree(t, file{"run.sh", 0o644, "echo\n"})
	exec := writeTree(t, file{"run.sh", 0o755, "echo\n"})
	assert.NotEqual(t, hashOf(t, plain), hashOf(t, exec))
}

func TestHash_IgnoresOtherPermissionBits(t *testing.T) {
	skipWithoutExecBits(t)
	// A different umask gives 0600 or 0700 instead of 0644 or 0755.
	assert.Equal(t,
		hashOf(t, writeTree(t, file{"a", 0o644, "x"}, file{"b", 0o755, "y"})),
		hashOf(t, writeTree(t, file{"a", 0o600, "x"}, file{"b", 0o700, "y"})),
	)
}

func TestHash_ContentAndPathChangeHash(t *testing.T) {
	base := hashOf(t, writeTree(t, file{"a.txt", 0o644, "x"}))
	assert.NotEqual(t, base, hashOf(t, writeTree(t, file{"a.txt", 0o644, "y"})))
	assert.NotEqual(t, base, hashOf(t, writeTree(t, file{"b.txt", 0o644, "x"})))
	// Moving bytes between path and content must not collide.
	assert.NotEqual(t,
		hashOf(t, writeTree(t, file{"ab", 0o644, "c"})),
		hashOf(t, writeTree(t, file{"a", 0o644, "bc"})),
	)
}

func TestHash_SkipsGitDirectory(t *testing.T) {
	clean := writeTree(t, file{"SKILL.md", 0o644, "a\n"})
	withGit := writeTree(t, file{"SKILL.md", 0o644, "a\n"}, file{".git/HEAD", 0o644, "ref: refs/heads/main\n"})
	assert.Equal(t, hashOf(t, clean), hashOf(t, withGit))
}

func TestHash_RejectsSymlinks(t *testing.T) {
	dir := writeTree(t, file{"SKILL.md", 0o644, "a\n"})
	if err := os.Symlink("SKILL.md", filepath.Join(dir, "link.md")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	_, err := Hash(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "link.md")
}

func TestParse_SetsSHA256(t *testing.T) {
	dir := filepath.Join("testdata", "valid", "full-fields")
	s, err := Parse(dir)
	require.NoError(t, err)
	assert.Equal(t, hashOf(t, dir), s.SHA256)
	assert.Regexp(t, `^[0-9a-f]{64}$`, s.SHA256)
}

func TestHash_FollowsSymlinkedRoot(t *testing.T) {
	// A skill directory is often linked into an agent's skills folder; the
	// link itself is the skill, only links inside it are rejected.
	target := writeTree(t, file{"SKILL.md", 0o644, "a\n"})
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	assert.Equal(t, hashOf(t, target), hashOf(t, link))
}
