package registry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// fakeRegistry mimics the mcpfleet-registry server API: it stores request
// bodies as-is and answers in the ServerOutput shape (lowercase keys plus
// id/created_at/updated_at, every field present). Like the registry, it
// rejects a request key that ServerBody does not declare with a 422.
type fakeRegistry struct {
	mu      sync.Mutex
	bodies  []map[string]any
	servers []map[string]any
}

// serverBodyKeys are the JSON keys of ServerBody in mcpfleet-registry
// (internal/api/routes.go); keep the two lists in sync.
var serverBodyKeys = map[string]bool{
	"name": true, "description": true, "transport": true, "install": true,
	"command": true, "url": true, "args": true, "env": true, "tags": true,
	"platforms": true,
}

func (f *fakeRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/servers":
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.bodies = append(f.bodies, body)
		for k := range body {
			if !serverBodyKeys[k] {
				http.Error(w, `{"detail":"validation failed","errors":[{"message":"unexpected property","location":"body.`+k+`"}]}`, http.StatusUnprocessableEntity)
				return
			}
		}
		if _, ok := body["name"].(string); !ok {
			http.Error(w, `{"detail":"expected required property name"}`, http.StatusUnprocessableEntity)
			return
		}
		out := map[string]any{
			"id": "0000", "description": "", "transport": "stdio",
			"install": map[string]any{}, "command": "", "url": "", "args": []any{},
			"env": map[string]any{}, "tags": []any{}, "platforms": []any{},
			"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z",
		}
		for k, v := range body {
			out[k] = v
		}
		f.servers = append(f.servers, out)
		_ = json.NewEncoder(w).Encode(out)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/servers":
		_ = json.NewEncoder(w).Encode(f.servers)
	default:
		http.NotFound(w, r)
	}
}

func newTestClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MCPFLEET_REGISTRY_URL", ts.URL)
	dir := filepath.Join(home, ".config", "mcpfleet")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("mcp_test"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func githubServer() *schema.Server {
	return &schema.Server{
		Name:        "github",
		Description: "GitHub MCP server",
		Transport:   "stdio",
		Install:     schema.InstallConfig{Type: "npx", Package: "@modelcontextprotocol/server-github"},
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-github"},
		Env: map[string]schema.EnvVar{
			"GITHUB_TOKEN": {Secret: "github_token"},
			"LOG_LEVEL":    {Value: "debug"},
		},
		Tags:      []string{"dev"},
		Platforms: []string{"linux"},
	}
}

func TestCreateServer_SendsLowercaseJSON(t *testing.T) {
	fake := &fakeRegistry{}
	c := newTestClient(t, fake)

	if _, err := c.CreateServer(context.Background(), githubServer()); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if len(fake.bodies) != 1 {
		t.Fatalf("got %d request bodies, want 1", len(fake.bodies))
	}
	body := fake.bodies[0]
	for _, k := range []string{"name", "description", "transport", "install", "command", "args", "env", "tags", "platforms"} {
		if _, ok := body[k]; !ok {
			t.Errorf("request body lacks %q: %v", k, body)
		}
	}
	for k := range body {
		if k != "" && k[0] >= 'A' && k[0] <= 'Z' {
			t.Errorf("request body has capitalised key %q", k)
		}
	}
	env, _ := body["env"].(map[string]any)
	tok, _ := env["GITHUB_TOKEN"].(map[string]any)
	if tok["secret"] != "github_token" {
		t.Errorf("env.GITHUB_TOKEN = %v, want {secret: github_token}", env["GITHUB_TOKEN"])
	}
}

func TestPushPullRoundTrip_PreservesSecretRefs(t *testing.T) {
	c := newTestClient(t, &fakeRegistry{})
	ctx := context.Background()

	want := githubServer()
	if _, err := c.CreateServer(ctx, want); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	got, err := c.ListServers(ctx)
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d servers, want 1", len(got))
	}
	if !reflect.DeepEqual(got[0], *want) {
		t.Errorf("round trip changed the server\n got: %+v\nwant: %+v", got[0], *want)
	}
}

// TestPushPullRoundTrip_RemoteServer pushes an http server, which has a url
// and no command, and pulls it back unchanged.
func TestPushPullRoundTrip_RemoteServer(t *testing.T) {
	fake := &fakeRegistry{}
	c := newTestClient(t, fake)
	ctx := context.Background()

	// The registry returns every collection, empty ones as [] or {}, so a
	// pulled server has empty (not nil) collections.
	want := &schema.Server{Name: "remote", Transport: "http", URL: "https://example.com/mcp",
		Args: []string{}, Env: map[string]schema.EnvVar{}, Tags: []string{}, Platforms: []string{}}
	if _, err := c.CreateServer(ctx, want); err != nil {
		t.Fatalf("CreateServer: %v", err)
	}
	if got := fake.bodies[0]["url"]; got != want.URL {
		t.Errorf("request body url = %v, want %q", got, want.URL)
	}
	got, err := c.ListServers(ctx)
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d servers, want 1", len(got))
	}
	if !reflect.DeepEqual(got[0], *want) {
		t.Errorf("round trip changed the server\n got: %+v\nwant: %+v", got[0], *want)
	}
}

// TestFakeRegistry_RejectsUnknownKey keeps the fake as strict as the
// registry, so a key the registry does not know fails here too.
func TestFakeRegistry_RejectsUnknownKey(t *testing.T) {
	ts := httptest.NewServer(&fakeRegistry{})
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/v1/servers", "application/json",
		strings.NewReader(`{"name":"x","headers":{"A":"b"}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
}
