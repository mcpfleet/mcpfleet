// Package registry communicates with the mcpfleet cloud registry.
// It fetches server definitions for the authenticated user.
package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mcpfleet/mcpfleet/internal/schema"
)

const (
	tokenFile       = "token"
	registryURLFile = "registry_url"
)

// defaultBaseURL is the registry used when neither MCPFLEET_REGISTRY_URL nor
// a saved URL is set. A variable so tests can point it at an unreachable host.
var defaultBaseURL = "https://registry.mcpfleet.dev"

// Sentinel errors matched by APIError through errors.Is.
var (
	// ErrNotFound matches a 404 response: the server does not exist.
	ErrNotFound = errors.New("registry: not found")
	// ErrConflict matches a 409 response, e.g. a server name already taken.
	ErrConflict = errors.New("registry: conflict")
	// ErrValidation matches a 422 response: the registry rejected the body.
	ErrValidation = errors.New("registry: validation failed")
)

// validationFailed is the generic "detail" the registry sends with a 422; the
// message already says it, so APIError.Error drops it from the detail.
const validationFailed = "validation failed"

// maxErrorBody caps how much of an error response body is read for detail.
const maxErrorBody = 64 << 10

// APIError is a non-2xx response from the registry (other than 401).
// Detail is the "detail" field of the application/problem+json body, if any.
type APIError struct {
	StatusCode int
	Detail     string
}

// Error names the failure for 404, 409 and 422 and gives the status code for
// any other response, followed by the server's detail.
func (e *APIError) Error() string {
	var msg string
	detail := e.Detail
	switch e.StatusCode {
	case http.StatusNotFound:
		msg = ErrNotFound.Error()
	case http.StatusConflict:
		msg = ErrConflict.Error()
	case http.StatusUnprocessableEntity:
		msg = ErrValidation.Error()
		if detail == validationFailed {
			detail = ""
		} else {
			detail = strings.TrimPrefix(detail, validationFailed+": ")
		}
	default:
		msg = fmt.Sprintf("registry: unexpected status %d", e.StatusCode)
	}
	if detail != "" {
		msg += ": " + detail
	}
	return msg
}

// Is lets errors.Is match ErrNotFound, ErrConflict and ErrValidation by
// status code.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrConflict:
		return e.StatusCode == http.StatusConflict
	case ErrValidation:
		return e.StatusCode == http.StatusUnprocessableEntity
	}
	return false
}

// readDetail returns the "detail" field of a problem+json error body
// followed by its errors[] entries ("location: message", joined by "; "),
// or "" when the body is empty or not JSON.
func readDetail(r io.Reader) string {
	var p struct {
		Detail string `json:"detail"`
		Errors []struct {
			Location string `json:"location"`
			Message  string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(r, maxErrorBody)).Decode(&p); err != nil {
		return ""
	}
	var entries []string
	for _, e := range p.Errors {
		switch {
		case e.Location != "" && e.Message != "":
			entries = append(entries, e.Location+": "+e.Message)
		case e.Message != "":
			entries = append(entries, e.Message)
		}
	}
	if len(entries) == 0 {
		return p.Detail
	}
	if p.Detail == "" {
		return strings.Join(entries, "; ")
	}
	return p.Detail + ": " + strings.Join(entries, "; ")
}

// URLSource names where the effective registry URL came from.
type URLSource string

// Sources of the registry URL, in order of precedence.
const (
	SourceEnv     URLSource = "MCPFLEET_REGISTRY_URL"
	SourceFile    URLSource = "~/.config/mcpfleet/registry_url"
	SourceDefault URLSource = "default"
)

// BaseURL returns the effective registry URL, without trailing slashes, and
// its source: MCPFLEET_REGISTRY_URL, then the URL saved by "auth login",
// then the default.
func BaseURL() (string, URLSource) {
	if u := os.Getenv("MCPFLEET_REGISTRY_URL"); u != "" {
		return strings.TrimRight(u, "/"), SourceEnv
	}
	if u, _ := LoadRegistryURL(); u != "" {
		return strings.TrimRight(u, "/"), SourceFile
	}
	return defaultBaseURL, SourceDefault
}

// Client talks to the mcpfleet registry API.
type Client struct {
	baseURL    string
	source     URLSource
	httpClient *http.Client
	token      string
}

// New returns a Client using the stored auth token.
func New() (*Client, error) {
	token, err := LoadToken()
	if err != nil {
		return nil, fmt.Errorf("registry: not authenticated — run 'mcpfleet auth login' first")
	}
	baseURL, source := BaseURL()
	return &Client{
		baseURL:    baseURL,
		source:     source,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
	}, nil
}

// do executes an authenticated HTTP request and decodes the JSON response into out (may be nil).
func (c *Client) do(ctx context.Context, method, path string, body any, out any) (*http.Response, error) {
	var bodyReader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("registry: marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("registry: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Nothing answering at the built-in default usually means no
		// registry was configured; say how to set one.
		if c.source == SourceDefault && ctx.Err() == nil {
			return nil, fmt.Errorf("registry: request failed: %w — no registry answers at the default URL %s; "+
				"point mcpfleet at your registry with 'mcpfleet auth login --url <registry>' or MCPFLEET_REGISTRY_URL",
				err, c.baseURL)
		}
		return nil, fmt.Errorf("registry: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return resp, fmt.Errorf("registry: token invalid or expired — run 'mcpfleet auth login'")
	}
	if resp.StatusCode >= 300 {
		return resp, &APIError{StatusCode: resp.StatusCode, Detail: readDetail(resp.Body)}
	}
	if out != nil && resp.ContentLength != 0 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp, fmt.Errorf("registry: decode response: %w", err)
		}
	}
	return resp, nil
}

// serverPath is the path of one server. The name or ID is escaped as a single
// path segment, so "/", "?", "#" and spaces in a name reach the right endpoint.
func serverPath(id string) string {
	return "/v1/servers/" + url.PathEscape(id)
}

// ListServers fetches all MCP servers from the registry.
func (c *Client) ListServers(ctx context.Context) ([]schema.Server, error) {
	var result []schema.Server
	if _, err := c.do(ctx, http.MethodGet, "/v1/servers", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetServer fetches a single server by ID.
func (c *Client) GetServer(ctx context.Context, id string) (*schema.Server, error) {
	var result schema.Server
	if _, err := c.do(ctx, http.MethodGet, serverPath(id), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateServer uploads a new MCP server definition to the registry.
func (c *Client) CreateServer(ctx context.Context, srv *schema.Server) (*schema.Server, error) {
	var result schema.Server
	if _, err := c.do(ctx, http.MethodPost, "/v1/servers", srv, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateServer replaces an existing MCP server definition.
func (c *Client) UpdateServer(ctx context.Context, id string, srv *schema.Server) (*schema.Server, error) {
	var result schema.Server
	if _, err := c.do(ctx, http.MethodPut, serverPath(id), srv, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteServer removes a server from the registry by ID.
func (c *Client) DeleteServer(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, serverPath(id), nil, nil)
	return err
}

// --- token helpers ---

// configDir returns the mcpfleet config directory (~/.config/mcpfleet).
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "mcpfleet"), nil
}

// SaveToken persists the auth token to disk.
func SaveToken(token string) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, tokenFile), []byte(token), 0o600)
}

// LoadToken reads the stored auth token.
func LoadToken() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, tokenFile))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// DeleteToken removes the stored auth token (logout).
func DeleteToken() error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	return os.Remove(filepath.Join(dir, tokenFile))
}

// SaveRegistryURL persists the registry base URL.
func SaveRegistryURL(url string) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, registryURLFile), []byte(url), 0o600)
}

// LoadRegistryURL reads the stored registry URL.
func LoadRegistryURL() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, registryURLFile))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
