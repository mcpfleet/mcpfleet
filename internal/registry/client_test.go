package registry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// recorded is one request seen by recorder.
type recorded struct {
	method, path, auth, accept, contentType string
	// escapedPath and query are the path and query as sent on the wire.
	escapedPath, query string
	body               []byte
}

// recorder records every request and answers with a fixed status and body.
type recorder struct {
	status int
	body   string

	mu   sync.Mutex
	reqs []recorded
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	b, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, recorded{
		method:      req.Method,
		path:        req.URL.Path,
		escapedPath: req.URL.EscapedPath(),
		query:       req.URL.RawQuery,
		auth:        req.Header.Get("Authorization"),
		accept:      req.Header.Get("Accept"),
		contentType: req.Header.Get("Content-Type"),
		body:        b,
	})
	if r.body != "" {
		w.Header().Set("Content-Type", "application/json")
	}
	status := r.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, r.body)
}

func (r *recorder) last(t *testing.T) recorded {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.Len(t, r.reqs, 1)
	return r.reqs[0]
}

const serverJSON = `{"name":"github","transport":"stdio","install":{"type":"npx","package":"pkg"},"command":"npx"}`

var wantServer = schema.Server{
	Name: "github", Transport: "stdio",
	Install: schema.InstallConfig{Type: "npx", Package: "pkg"}, Command: "npx",
}

func TestListServers(t *testing.T) {
	rec := &recorder{body: "[" + serverJSON + "]"}
	c := newTestClient(t, rec)

	got, err := c.ListServers(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []schema.Server{wantServer}, got)

	r := rec.last(t)
	assert.Equal(t, http.MethodGet, r.method)
	assert.Equal(t, "/v1/servers", r.path)
	assert.Equal(t, "Bearer mcp_test", r.auth)
	assert.Equal(t, "application/json", r.accept)
	assert.Empty(t, r.contentType, "GET must not send Content-Type")
	assert.Empty(t, r.body)
}

func TestListServers_EmptyBody(t *testing.T) {
	c := newTestClient(t, &recorder{})
	got, err := c.ListServers(context.Background())
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestGetServer(t *testing.T) {
	rec := &recorder{body: serverJSON}
	c := newTestClient(t, rec)

	got, err := c.GetServer(context.Background(), "abc")
	require.NoError(t, err)
	assert.Equal(t, wantServer, *got)
	r := rec.last(t)
	assert.Equal(t, http.MethodGet, r.method)
	assert.Equal(t, "/v1/servers/abc", r.path)
}

func TestCreateServer(t *testing.T) {
	rec := &recorder{status: http.StatusCreated, body: serverJSON}
	c := newTestClient(t, rec)

	in := wantServer
	got, err := c.CreateServer(context.Background(), &in)
	require.NoError(t, err)
	assert.Equal(t, wantServer, *got)

	r := rec.last(t)
	assert.Equal(t, http.MethodPost, r.method)
	assert.Equal(t, "/v1/servers", r.path)
	assert.Equal(t, "application/json", r.contentType)
	assert.Equal(t, "Bearer mcp_test", r.auth)
	var sent schema.Server
	require.NoError(t, json.Unmarshal(r.body, &sent))
	assert.Equal(t, in, sent)
}

func TestUpdateServer(t *testing.T) {
	rec := &recorder{body: serverJSON}
	c := newTestClient(t, rec)

	in := wantServer
	got, err := c.UpdateServer(context.Background(), "abc", &in)
	require.NoError(t, err)
	assert.Equal(t, wantServer, *got)

	r := rec.last(t)
	assert.Equal(t, http.MethodPut, r.method)
	assert.Equal(t, "/v1/servers/abc", r.path)
	assert.Equal(t, "application/json", r.contentType)
	var sent schema.Server
	require.NoError(t, json.Unmarshal(r.body, &sent))
	assert.Equal(t, in, sent)
}

func TestDeleteServer(t *testing.T) {
	for name, status := range map[string]int{"204": http.StatusNoContent, "200 empty": http.StatusOK} {
		t.Run(name, func(t *testing.T) {
			rec := &recorder{status: status}
			c := newTestClient(t, rec)

			require.NoError(t, c.DeleteServer(context.Background(), "abc"))
			r := rec.last(t)
			assert.Equal(t, http.MethodDelete, r.method)
			assert.Equal(t, "/v1/servers/abc", r.path)
			assert.Equal(t, "Bearer mcp_test", r.auth)
		})
	}
}

func TestErrorStatuses(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusUnauthorized, "token invalid or expired"},
		{http.StatusNotFound, "registry: not found"},
		{http.StatusUnprocessableEntity, "registry: validation failed: x"},
		{http.StatusInternalServerError, "unexpected status 500"},
		{http.StatusMultipleChoices, "unexpected status 300"},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			c := newTestClient(t, &recorder{status: tc.status, body: `{"detail":"x"}`})
			ctx := context.Background()
			srv := wantServer

			_, err := c.ListServers(ctx)
			assert.ErrorContains(t, err, tc.want)
			_, err = c.GetServer(ctx, "abc")
			assert.ErrorContains(t, err, tc.want)
			_, err = c.CreateServer(ctx, &srv)
			assert.ErrorContains(t, err, tc.want)
			_, err = c.UpdateServer(ctx, "abc", &srv)
			assert.ErrorContains(t, err, tc.want)
			assert.ErrorContains(t, c.DeleteServer(ctx, "abc"), tc.want)
		})
	}
}

func TestErrorStatuses_IsAndDetail(t *testing.T) {
	cases := []struct {
		status   int
		body     string
		sentinel error
		want     string
	}{
		{http.StatusNotFound, `{"detail":"server not found"}`, ErrNotFound, "registry: not found: server not found"},
		{http.StatusConflict, `{"detail":"server name \"x\" is already taken"}`, ErrConflict, `registry: conflict: server name "x" is already taken`},
		{http.StatusNotFound, "", ErrNotFound, "registry: not found"},
		{http.StatusUnprocessableEntity,
			`{"detail":"validation failed","errors":[{"location":"body.url","message":"url is required for transport sse"}]}`,
			ErrValidation, "registry: validation failed: body.url: url is required for transport sse"},
		{http.StatusUnprocessableEntity, `{"detail":"expected required property name"}`,
			ErrValidation, "registry: validation failed: expected required property name"},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status)+" "+tc.body, func(t *testing.T) {
			c := newTestClient(t, &recorder{status: tc.status, body: tc.body})

			err := c.DeleteServer(context.Background(), "x")
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.sentinel)
			assert.EqualError(t, err, tc.want)

			var apiErr *APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, tc.status, apiErr.StatusCode)
		})
	}
}

// TestErrorStatuses_OtherStatusHasNoSentinel keeps the sentinels tied to their
// own status codes: a 500 is none of not found, conflict or validation failed.
func TestErrorStatuses_OtherStatusHasNoSentinel(t *testing.T) {
	c := newTestClient(t, &recorder{status: http.StatusInternalServerError, body: `{"detail":"validation failed"}`})

	err := c.DeleteServer(context.Background(), "x")
	require.Error(t, err)
	assert.EqualError(t, err, "registry: unexpected status 500: validation failed")
	assert.NotErrorIs(t, err, ErrValidation)
	assert.NotErrorIs(t, err, ErrNotFound)
	assert.NotErrorIs(t, err, ErrConflict)
}

// TestAPIError_ValidationDetails shows each errors[] entry of a 422, so the
// user learns which field the registry rejected and why.
func TestAPIError_ValidationDetails(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"one entry",
			`{"detail":"validation failed","errors":[{"location":"body.url","message":"url is required for transport sse","value":""}]}`,
			"registry: validation failed: body.url: url is required for transport sse"},
		{"two entries",
			`{"detail":"validation failed","errors":[{"location":"body.command","message":"command is required for transport stdio"},{"location":"body.url","message":"url is only used for transport http or sse"}]}`,
			"registry: validation failed: body.command: command is required for transport stdio; body.url: url is only used for transport http or sse"},
		{"entry without location",
			`{"detail":"validation failed","errors":[{"message":"bad body"}]}`,
			"registry: validation failed: bad body"},
		{"no entries",
			`{"detail":"validation failed"}`,
			"registry: validation failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, &recorder{status: http.StatusUnprocessableEntity, body: tc.body})
			_, err := c.CreateServer(context.Background(), &schema.Server{Name: "x"})
			assert.EqualError(t, err, tc.want)
		})
	}
}

func TestInvalidJSONResponse(t *testing.T) {
	c := newTestClient(t, &recorder{body: `{"name":`})
	_, err := c.GetServer(context.Background(), "abc")
	assert.ErrorContains(t, err, "registry: decode response")
}

func TestCanceledContext(t *testing.T) {
	c := newTestClient(t, &recorder{body: "[]"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.ListServers(ctx)
	assert.ErrorContains(t, err, "registry: request failed")
}

func TestNew_WithoutToken(t *testing.T) {
	setHome(t)
	_, err := New()
	assert.ErrorContains(t, err, "not authenticated")
}

// setHome isolates the config dir and clears the URL override.
func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MCPFLEET_REGISTRY_URL", "")
	return home
}

func TestToken_SaveLoadDelete(t *testing.T) {
	home := setHome(t)

	require.NoError(t, SaveToken("  tok\n"))
	got, err := LoadToken()
	require.NoError(t, err)
	assert.Equal(t, "tok", got)

	st, err := os.Stat(filepath.Join(home, ".config", "mcpfleet", "token"))
	require.NoError(t, err)
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("token mode = %v, want 0600", st.Mode().Perm())
	}

	require.NoError(t, DeleteToken())
	_, err = LoadToken()
	assert.Error(t, err)
	assert.Error(t, DeleteToken(), "deleting a missing token")
}

func TestRegistryURL_SaveLoad(t *testing.T) {
	setHome(t)
	_, err := LoadRegistryURL()
	assert.Error(t, err, "no file yet")

	require.NoError(t, SaveRegistryURL(" https://r.example \n"))
	got, err := LoadRegistryURL()
	require.NoError(t, err)
	assert.Equal(t, "https://r.example", got)
}

func TestBaseURL_Sources(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		setHome(t)
		u, src := BaseURL()
		assert.Equal(t, "https://registry.mcpfleet.dev", u)
		assert.Equal(t, SourceDefault, src)
	})
	t.Run("file", func(t *testing.T) {
		setHome(t)
		require.NoError(t, SaveRegistryURL("https://file.example/"))
		u, src := BaseURL()
		assert.Equal(t, "https://file.example", u)
		assert.Equal(t, SourceFile, src)
	})
	t.Run("env over file", func(t *testing.T) {
		setHome(t)
		require.NoError(t, SaveRegistryURL("https://file.example"))
		t.Setenv("MCPFLEET_REGISTRY_URL", "https://env.example//")
		u, src := BaseURL()
		assert.Equal(t, "https://env.example", u)
		assert.Equal(t, SourceEnv, src)
	})
	t.Run("source names", func(t *testing.T) {
		assert.Equal(t, "MCPFLEET_REGISTRY_URL", string(SourceEnv))
		assert.Equal(t, "~/.config/mcpfleet/registry_url", string(SourceFile))
		assert.Equal(t, "default", string(SourceDefault))
	})
}

func TestNew_BaseURLPrecedence(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		setHome(t)
		require.NoError(t, SaveToken("t"))
		c, err := New()
		require.NoError(t, err)
		assert.Equal(t, "https://registry.mcpfleet.dev", c.baseURL)
		assert.Equal(t, "t", c.token)
	})
	t.Run("file over default", func(t *testing.T) {
		setHome(t)
		require.NoError(t, SaveToken("t"))
		require.NoError(t, SaveRegistryURL("https://file.example/"))
		c, err := New()
		require.NoError(t, err)
		assert.Equal(t, "https://file.example", c.baseURL)
	})
	t.Run("env over file", func(t *testing.T) {
		setHome(t)
		require.NoError(t, SaveToken("t"))
		require.NoError(t, SaveRegistryURL("https://file.example"))
		t.Setenv("MCPFLEET_REGISTRY_URL", "https://env.example//")
		c, err := New()
		require.NoError(t, err)
		assert.Equal(t, "https://env.example", c.baseURL)
	})
}

// TestServerPaths_EscapeName pins that a server name is one escaped path
// segment: "/", " ", "?" and "#" must not split the path, start a query or
// cut the URL at a fragment.
func TestServerPaths_EscapeName(t *testing.T) {
	calls := map[string]func(c *Client, id string) error{
		http.MethodGet: func(c *Client, id string) error {
			_, err := c.GetServer(context.Background(), id)
			return err
		},
		http.MethodPut: func(c *Client, id string) error {
			_, err := c.UpdateServer(context.Background(), id, &schema.Server{Name: id})
			return err
		},
		http.MethodDelete: func(c *Client, id string) error {
			return c.DeleteServer(context.Background(), id)
		},
	}
	names := []struct{ name, wantPath, wantEscaped string }{
		{"a/b c?d", "/v1/servers/a/b c?d", "/v1/servers/a%2Fb%20c%3Fd"},
		{"a#b", "/v1/servers/a#b", "/v1/servers/a%23b"},
	}
	for method, call := range calls {
		for _, n := range names {
			t.Run(method+" "+n.name, func(t *testing.T) {
				rec := &recorder{body: serverJSON}
				c := newTestClient(t, rec)
				require.NoError(t, call(c, n.name))

				r := rec.last(t)
				assert.Equal(t, method, r.method)
				assert.Equal(t, n.wantEscaped, r.escapedPath)
				assert.Equal(t, n.wantPath, r.path)
				assert.Empty(t, r.query)
			})
		}
	}
}

// unreachableURL returns an http URL on which nothing listens.
func unreachableURL(t *testing.T) string {
	t.Helper()
	ts := httptest.NewServer(http.NotFoundHandler())
	u := ts.URL
	ts.Close()
	return u
}

func TestUnreachableDefault_SaysHowToSetURL(t *testing.T) {
	setHome(t)
	require.NoError(t, SaveToken("t"))
	old := defaultBaseURL
	defaultBaseURL = unreachableURL(t)
	t.Cleanup(func() { defaultBaseURL = old })

	c, err := New()
	require.NoError(t, err)
	_, err = c.ListServers(context.Background())
	require.Error(t, err)
	assert.ErrorContains(t, err, "registry: request failed")
	assert.ErrorContains(t, err, "no registry answers at the default URL "+defaultBaseURL)
	assert.ErrorContains(t, err, "mcpfleet auth login --url <registry>")
	assert.ErrorContains(t, err, "MCPFLEET_REGISTRY_URL")
}

func TestUnreachableConfiguredURL_NoDefaultHint(t *testing.T) {
	setHome(t)
	require.NoError(t, SaveToken("t"))
	t.Setenv("MCPFLEET_REGISTRY_URL", unreachableURL(t))

	c, err := New()
	require.NoError(t, err)
	_, err = c.ListServers(context.Background())
	assert.ErrorContains(t, err, "registry: request failed")
	assert.NotContains(t, err.Error(), "default URL")
}

func TestCanceledContext_NoDefaultHint(t *testing.T) {
	setHome(t)
	require.NoError(t, SaveToken("t"))
	c, err := New()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.ListServers(ctx)
	assert.ErrorContains(t, err, "registry: request failed")
	assert.NotContains(t, err.Error(), "default URL")
}
