package adapters_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mcpfleet/mcpfleet/internal/adapters"
	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// serversKey maps each adapter to the top-level key holding its MCP servers.
var serversKey = map[string]string{
	"cursor":         "mcpServers",
	"claude-code":    "mcpServers",
	"claude-desktop": "mcpServers",
	"windsurf":       "mcpServers",
	"crush":          "mcp",
	"zed":            "context_servers",
}

// isolate points every config location at a fresh temp dir.
func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("APPDATA", filepath.Join(dir, "AppData"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
}

// forEachAdapter runs fn for every registered adapter in an isolated home.
func forEachAdapter(t *testing.T, fn func(t *testing.T, a adapters.Adapter, key string)) {
	t.Helper()
	for _, a := range adapters.All() {
		a := a
		key, ok := serversKey[a.Name()]
		if !ok {
			t.Fatalf("adapter %q has no entry in serversKey", a.Name())
		}
		t.Run(a.Name(), func(t *testing.T) {
			isolate(t)
			fn(t, a, key)
		})
	}
}

func writeConfig(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("written config is not valid JSON: %v\n%s", err, data)
	}
	return m
}

var newServer = []schema.Server{{Name: "new-srv", Command: "npx", Args: []string{"-y", "pkg"}}}

func TestApply_PreservesUnknownTopLevelKeys(t *testing.T) {
	forEachAdapter(t, func(t *testing.T, a adapters.Adapter, key string) {
		path := a.ConfigPath()
		writeConfig(t, path, `{"keep":1,"nested":{"a":[1,2]},"`+key+`":{}}`, 0o644)

		if err := a.Apply(newServer); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		m := readJSON(t, path)
		if m["keep"] != float64(1) {
			t.Errorf("top-level key \"keep\" lost or changed: %v", m)
		}
		if _, ok := m["nested"]; !ok {
			t.Errorf("top-level key \"nested\" lost: %v", m)
		}
		servers, _ := m[key].(map[string]any)
		if _, ok := servers["new-srv"]; !ok {
			t.Errorf("new server not written under %q: %v", key, m)
		}
	})
}

func TestApply_PreservesUnrelatedServerEntries(t *testing.T) {
	forEachAdapter(t, func(t *testing.T, a adapters.Adapter, key string) {
		path := a.ConfigPath()
		writeConfig(t, path, `{"`+key+`":{"old":{"type":"http","url":"https://x","extra":true}}}`, 0o644)

		if err := a.Apply(newServer); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		servers, _ := readJSON(t, path)[key].(map[string]any)
		old, _ := servers["old"].(map[string]any)
		if old["type"] != "http" || old["url"] != "https://x" || old["extra"] != true {
			t.Errorf("unrelated server entry changed: %v", servers["old"])
		}
	})
}

func TestApply_InvalidConfigFailsWithoutWrite(t *testing.T) {
	cases := map[string]string{
		"syntax":             `{"keep":1,`,
		"root not object":    `[1,2]`,
		"servers not object": `{"SERVERS":[1]}`,
	}
	forEachAdapter(t, func(t *testing.T, a adapters.Adapter, key string) {
		for name, content := range cases {
			t.Run(name, func(t *testing.T) {
				content = string(bytes.ReplaceAll([]byte(content), []byte("SERVERS"), []byte(key)))
				path := a.ConfigPath()
				writeConfig(t, path, content, 0o644)
				_ = os.Remove(path + ".bak")

				if err := a.Apply(newServer); err == nil {
					t.Fatal("Apply returned nil error for an invalid config")
				}
				got, _ := os.ReadFile(path)
				if string(got) != content {
					t.Errorf("config modified:\n got %s\nwant %s", got, content)
				}
				if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
					t.Errorf(".bak created for a failed apply (err=%v)", err)
				}
			})
		}
	})
}

func TestApply_CreatesBackup(t *testing.T) {
	forEachAdapter(t, func(t *testing.T, a adapters.Adapter, key string) {
		path := a.ConfigPath()
		orig := `{"keep":"original","` + key + `":{}}`
		writeConfig(t, path, orig, 0o600)

		if err := a.Apply(newServer); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		bak, err := os.ReadFile(path + ".bak")
		if err != nil {
			t.Fatalf("no backup: %v", err)
		}
		if string(bak) != orig {
			t.Errorf("backup content = %s, want %s", bak, orig)
		}
		if runtime.GOOS != "windows" {
			for _, p := range []string{path, path + ".bak"} {
				st, err := os.Stat(p)
				if err != nil {
					t.Fatal(err)
				}
				if st.Mode().Perm() != 0o600 {
					t.Errorf("%s mode = %v, want 0600", filepath.Base(p), st.Mode().Perm())
				}
			}
		}
	})
}

func TestApply_NoBackupWhenFileMissing(t *testing.T) {
	forEachAdapter(t, func(t *testing.T, a adapters.Adapter, key string) {
		path := a.ConfigPath()
		if err := a.Apply(newServer); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("config not created: %v", err)
		}
		if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
			t.Errorf(".bak created for a new file (err=%v)", err)
		}
	})
}

func TestApply_DoesNotEscapeHTMLCharacters(t *testing.T) {
	forEachAdapter(t, func(t *testing.T, a adapters.Adapter, key string) {
		path := a.ConfigPath()
		writeConfig(t, path, `{"note":"a<b>&c","`+key+`":{"old":{"url":"https://x/?a=1&b=2"}}}`, 0o644)

		srv := []schema.Server{{Name: "new-srv", Command: "run", Args: []string{"--q=a&b<c>"}}}
		if err := a.Apply(srv); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		out, _ := os.ReadFile(path)
		for _, want := range []string{`a<b>&c`, `https://x/?a=1&b=2`, `--q=a&b<c>`} {
			if !bytes.Contains(out, []byte(want)) {
				t.Errorf("%q not written literally:\n%s", want, out)
			}
		}
	})
}
