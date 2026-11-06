package jsonfile_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mcpfleet/mcpfleet/internal/adapters/jsonfile"
)

func decode(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("invalid JSON written: %v\n%s", err, data)
	}
	return m
}

func TestUpsertEntries_MissingFileCreatesPrivate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a", "b", "cfg.json")
	if err := jsonfile.UpsertEntries(path, "mcpServers", map[string]any{"a": map[string]string{"command": "x"}}); err != nil {
		t.Fatal(err)
	}
	m := decode(t, path)
	if _, ok := m["mcpServers"].(map[string]any)["a"]; !ok {
		t.Fatalf("entry not written: %v", m)
	}
	if runtime.GOOS != "windows" {
		st, _ := os.Stat(path)
		if st.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600", st.Mode().Perm())
		}
		for _, dir := range []string{filepath.Join(root, "a"), filepath.Join(root, "a", "b")} {
			st, err := os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			if st.Mode().Perm() != 0o700 {
				t.Errorf("dir %s mode = %v, want 0700", dir, st.Mode().Perm())
			}
		}
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Errorf("unexpected .bak for a new file")
	}
}

func TestWriteFile_ExistingFileKeepsMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions do not apply on Windows")
	}
	path := filepath.Join(t.TempDir(), "cfg.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := jsonfile.WriteFile(path, []byte(`{"a":1}`), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, path + ".bak"} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o644 {
			t.Errorf("%s mode = %v, want 0644", filepath.Base(p), st.Mode().Perm())
		}
	}
}

func TestUpsertEntries_EmptyOrNullTreatedAsEmpty(t *testing.T) {
	for name, content := range map[string]string{"empty": "", "whitespace": " \n\t", "null servers": `{"mcpServers":null,"k":1}`} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cfg.json")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := jsonfile.UpsertEntries(path, "mcpServers", map[string]any{"a": 1}); err != nil {
				t.Fatal(err)
			}
			if decode(t, path)["mcpServers"].(map[string]any)["a"] != float64(1) {
				t.Errorf("entry not written")
			}
		})
	}
}

func TestUpsertEntries_ReplacesOnlyNamedEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.json")
	orig := `{"z":true,"mcpServers":{"keep":{"url":"u","x":[1]},"a":{"old":1}}}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := jsonfile.UpsertEntries(path, "mcpServers", map[string]any{"a": map[string]int{"new": 2}}); err != nil {
		t.Fatal(err)
	}
	m := decode(t, path)
	srv := m["mcpServers"].(map[string]any)
	if m["z"] != true {
		t.Errorf("top-level key lost: %v", m)
	}
	if keep := srv["keep"].(map[string]any); keep["url"] != "u" {
		t.Errorf("unrelated entry changed: %v", keep)
	}
	if a := srv["a"].(map[string]any); a["new"] != float64(2) || a["old"] != nil {
		t.Errorf("entry not replaced: %v", a)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil || string(bak) != orig {
		t.Errorf(".bak = %q, %v; want original", bak, err)
	}
}

func TestUpsertEntries_InvalidFailsWithoutWrite(t *testing.T) {
	for name, content := range map[string]string{
		"syntax":      `{"a":`,
		"array root":  `[]`,
		"string root": `"x"`,
		"key array":   `{"mcpServers":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cfg.json")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			err := jsonfile.UpsertEntries(path, "mcpServers", map[string]any{"a": 1})
			if err == nil || !strings.Contains(err.Error(), "parse "+path) {
				t.Fatalf("err = %v, want parse error naming the file", err)
			}
			got, _ := os.ReadFile(path)
			if string(got) != content {
				t.Errorf("file modified: %q", got)
			}
			if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
				t.Errorf(".bak created on failure")
			}
		})
	}
}

func TestWriteFile_FollowsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles", "cfg.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(`{}`), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "cfg.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := jsonfile.UpsertEntries(link, "mcpServers", map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced by a regular file")
	}
	if _, ok := decode(t, target)["mcpServers"]; !ok {
		t.Errorf("link target not updated")
	}
	st, _ := os.Stat(target)
	if st.Mode().Perm() != 0o640 {
		t.Errorf("target mode = %v, want 0640", st.Mode().Perm())
	}
	if _, err := os.Stat(target + ".bak"); err != nil {
		t.Errorf("backup not next to link target: %v", err)
	}
}

func TestWriteFile_ChangedDuringApply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.json")
	if err := os.WriteFile(path, []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := jsonfile.WriteFile(path, []byte(`{"v":3}`), []byte(`{"v":1}`))
	if err == nil || !strings.Contains(err.Error(), "changed during apply") {
		t.Fatalf("err = %v, want changed during apply", err)
	}
	if got, _ := os.ReadFile(path); string(got) != `{"v":2}` {
		t.Errorf("file overwritten: %s", got)
	}
}

func TestWriteFile_FileAppearedDuringApply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.json")
	if err := os.WriteFile(path, []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// prev == nil means the file did not exist when it was read.
	if err := jsonfile.WriteFile(path, []byte(`{}`), nil); err == nil {
		t.Fatal("expected error when a file appeared after the read")
	}
}

func TestWriteFile_NoTempLeftovers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.json")
	if err := jsonfile.WriteFile(path, []byte(`{}`), nil); err != nil {
		t.Fatal(err)
	}
	if err := jsonfile.WriteFile(path, []byte(`{"a":1}`), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "cfg.json,cfg.json.bak" {
		t.Errorf("dir contents = %v", names)
	}
}

func TestHomeVar(t *testing.T) {
	for goos, want := range map[string]string{
		"linux":   "HOME",
		"darwin":  "HOME",
		"freebsd": "HOME",
		"windows": "USERPROFILE",
		"plan9":   "home",
	} {
		if got := jsonfile.HomeVar(goos); got != want {
			t.Errorf("HomeVar(%q) = %q, want %q", goos, got, want)
		}
	}
}

func TestRequireAbs(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "cfg.json")
	if got, err := jsonfile.RequireAbs("agent", abs, "HOME"); err != nil || got != abs {
		t.Errorf("RequireAbs(abs) = %q, %v; want %q, nil", got, err, abs)
	}

	cases := []struct {
		vars []string
		want string
	}{
		{[]string{"HOME"}, "agent: cannot determine config directory (set HOME)"},
		{[]string{"HOME", "XDG_CONFIG_HOME"}, "agent: cannot determine config directory (set HOME or XDG_CONFIG_HOME)"},
		{[]string{"A", "B", "C"}, "agent: cannot determine config directory (set A, B or C)"},
	}
	for _, c := range cases {
		got, err := jsonfile.RequireAbs("agent", filepath.Join(".cfg", "x.json"), c.vars...)
		if err == nil {
			t.Errorf("RequireAbs(relative, %v) = %q, nil; want an error", c.vars, got)
			continue
		}
		if err.Error() != c.want {
			t.Errorf("RequireAbs(relative, %v) error = %q, want %q", c.vars, err, c.want)
		}
	}
}

// chdir changes the working directory to dir and restores it when the test
// ends (t.Chdir needs Go 1.24).
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
}

// assertEmptyDir fails when anything was created in dir.
func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("created in the working directory: %s", e.Name())
	}
}

const relPath = ".cfg/cfg.json"

func TestUpsertEntries_RefusesRelativePath(t *testing.T) {
	cwd := t.TempDir()
	chdir(t, cwd)
	err := jsonfile.UpsertEntries(relPath, "mcpServers", map[string]any{"a": 1})
	if err == nil || !strings.Contains(err.Error(), "refusing relative config path") {
		t.Errorf("UpsertEntries(%q) error = %v, want \"refusing relative config path\"", relPath, err)
	}
	assertEmptyDir(t, cwd)
}

func TestWriteFile_RefusesRelativePath(t *testing.T) {
	cwd := t.TempDir()
	chdir(t, cwd)
	err := jsonfile.WriteFile(relPath, []byte(`{}`), nil)
	if err == nil || !strings.Contains(err.Error(), "refusing relative config path") {
		t.Errorf("WriteFile(%q) error = %v, want \"refusing relative config path\"", relPath, err)
	}
	assertEmptyDir(t, cwd)
}

func TestRead_RefusesRelativePath(t *testing.T) {
	cwd := t.TempDir()
	chdir(t, cwd)
	if err := os.MkdirAll(filepath.Dir(relPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(relPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := jsonfile.Read(relPath)
	if err == nil || !strings.Contains(err.Error(), "refusing relative config path") {
		t.Errorf("Read(%q) = %q, %v; want \"refusing relative config path\"", relPath, data, err)
	}
}
