package adapters_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	t.Setenv("CRUSH_GLOBAL_CONFIG", "")
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

// missingDirs returns the directories between path and its nearest existing
// ancestor, i.e. the ones Apply has to create.
func missingDirs(t *testing.T, path string) []string {
	t.Helper()
	var dirs []string
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(dir); err == nil {
			return dirs
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		dirs = append(dirs, dir)
		if dir == filepath.Dir(dir) {
			return dirs
		}
	}
}

func TestApply_NewFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions do not apply on Windows")
	}
	forEachAdapter(t, func(t *testing.T, a adapters.Adapter, key string) {
		path := a.ConfigPath()
		created := missingDirs(t, path)

		if err := a.Apply(newServer); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		st, err := os.Stat(path)
		if err != nil {
			t.Fatalf("config not created: %v", err)
		}
		if got := st.Mode().Perm(); got != 0o600 {
			t.Errorf("new config mode = %v, want 0600", got)
		}
		for _, dir := range created {
			st, err := os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := st.Mode().Perm(); got != 0o700 {
				t.Errorf("new dir %s mode = %v, want 0700", dir, got)
			}
		}
	})
}

func TestApply_ExistingFileKeepsMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions do not apply on Windows")
	}
	forEachAdapter(t, func(t *testing.T, a adapters.Adapter, key string) {
		path := a.ConfigPath()
		writeConfig(t, path, `{"`+key+`":{}}`, 0o644)

		if err := a.Apply(newServer); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		for _, p := range []string{path, path + ".bak"} {
			st, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if got := st.Mode().Perm(); got != 0o644 {
				t.Errorf("%s mode = %v, want 0644 (kept)", filepath.Base(p), got)
			}
		}
	})
}

// remoteEntry is the entry each adapter writes for a remote server with the
// given transport and URL, or nil when the adapter skips that transport.
var remoteEntry = map[string]func(transport, url string) map[string]any{
	"claude-code": func(tr, u string) map[string]any { return map[string]any{"type": tr, "url": u} },
	"crush":       func(tr, u string) map[string]any { return map[string]any{"type": tr, "url": u} },
	"cursor":      func(_, u string) map[string]any { return map[string]any{"url": u} },
	"windsurf":    func(_, u string) map[string]any { return map[string]any{"serverUrl": u} },
	"zed": func(tr, u string) map[string]any {
		if tr != "http" {
			return nil // Zed has no SSE transport
		}
		return map[string]any{"url": u}
	},
	"claude-desktop": func(string, string) map[string]any { return nil },
}

// listTransport is the transport List reports for a remote entry the
// adapter wrote: adapters whose entry has no type field read it back as http.
var listTransport = map[string]func(transport string) string{
	"claude-code":    func(tr string) string { return tr },
	"crush":          func(tr string) string { return tr },
	"cursor":         func(string) string { return "http" },
	"windsurf":       func(string) string { return "http" },
	"zed":            func(tr string) string { return tr },
	"claude-desktop": func(tr string) string { return tr },
}

var remoteServers = []schema.Server{
	{Name: "local", Command: "npx", Args: []string{"-y", "pkg"}},
	{Name: "remote-http", Transport: "http", URL: "https://example.com/mcp",
		Env: map[string]schema.EnvVar{"IGNORED": {Value: "x"}}},
	{Name: "remote-sse", Transport: "sse", URL: "https://example.com/sse"},
}

func TestApply_RemoteServersEntryShape(t *testing.T) {
	forEachAdapter(t, func(t *testing.T, a adapters.Adapter, key string) {
		path := a.ConfigPath()
		// remote-http replaces a stdio entry of the same name.
		writeConfig(t, path, `{"`+key+`":{"other":{"command":"keep"},"remote-http":{"command":"old","args":["a"]}}}`, 0o644)

		if err := a.Apply(remoteServers); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		entries, _ := readJSON(t, path)[key].(map[string]any)
		if _, ok := entries["local"]; !ok {
			t.Errorf("stdio server not written: %v", entries)
		}
		if _, ok := entries["other"]; !ok {
			t.Errorf("unrelated entry lost: %v", entries)
		}
		for _, s := range remoteServers[1:] {
			want := remoteEntry[a.Name()](s.Transport, s.URL)
			got, written := entries[s.Name]
			if want == nil {
				if s.Name == "remote-http" {
					// Skipped: the previous entry stays as it was.
					if e, _ := got.(map[string]any); e["command"] != "old" {
						t.Errorf("%s: skipped server changed the existing entry: %v", s.Name, got)
					}
				} else if written {
					t.Errorf("%s: unsupported transport %s written: %v", s.Name, s.Transport, got)
				}
				continue
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s entry = %v, want %v", s.Name, got, want)
			}
		}
	})
}

func TestApply_RemoteServersListRoundTrip(t *testing.T) {
	forEachAdapter(t, func(t *testing.T, a adapters.Adapter, key string) {
		if err := a.Apply(remoteServers); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		listed, err := a.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		got := map[string]schema.Server{}
		for _, s := range listed {
			got[s.Name] = s
		}
		for _, s := range remoteServers[1:] {
			if remoteEntry[a.Name()](s.Transport, s.URL) == nil {
				if _, ok := got[s.Name]; ok {
					t.Errorf("%s: listed although skipped", s.Name)
				}
				continue
			}
			l, ok := got[s.Name]
			if !ok {
				t.Errorf("%s: not listed", s.Name)
				continue
			}
			if want := listTransport[a.Name()](s.Transport); l.Transport != want || l.URL != s.URL || l.Command != "" {
				t.Errorf("%s listed as transport=%q url=%q command=%q, want transport=%q url=%q command=\"\"",
					s.Name, l.Transport, l.URL, l.Command, want, s.URL)
			}
		}
	})
}

func TestApply_RemoteServersSkippedWithoutWrite(t *testing.T) {
	forEachAdapter(t, func(t *testing.T, a adapters.Adapter, key string) {
		skipped := []schema.Server{{Name: "no-url", Transport: "http"}}
		for _, tr := range []string{"http", "sse"} {
			if !a.SupportsTransport(tr) {
				skipped = append(skipped, schema.Server{Name: "remote-" + tr, Transport: tr, URL: "https://example.com/" + tr})
			}
		}
		path := a.ConfigPath()
		if err := a.Apply(skipped); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			data, _ := os.ReadFile(path)
			t.Errorf("config written although every server was skipped (err=%v):\n%s", err, data)
		}
	})
}
