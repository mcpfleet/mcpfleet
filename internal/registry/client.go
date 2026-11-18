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
	"github.com/mcpfleet/mcpfleet/pkg/skillspec"
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
	// ErrTooLarge matches a 413 response: the body is above the registry's
	// limit, e.g. a skill archive above 1 MiB.
	ErrTooLarge = errors.New("registry: archive too large")
)

// validationFailed is the generic "detail" the registry sends with a 422; the
// message already says it, so APIError.Error drops it from the detail.
const validationFailed = "validation failed"

// maxErrorBody caps how much of an error response body is read for detail.
const maxErrorBody = 64 << 10

// APIError is a non-2xx response from the registry (other than 401).
// Detail is the "detail" field of the application/problem+json body followed
// by its errors[] entries, if any; Errors keeps those entries as sent.
type APIError struct {
	StatusCode int
	Detail     string
	Errors     []ProblemError
}

// ProblemError is one errors[] entry of a problem+json body: where the
// problem is (e.g. "body.url", "metadata.mcpfleet-requires"), what it is and
// the offending value.
type ProblemError struct {
	Location string `json:"location"`
	Message  string `json:"message"`
	Value    any    `json:"value"`
}

// requiresLocation is the errors[] location of a server a skill requires
// but the registry does not have.
const requiresLocation = "metadata." + skillspec.MetadataRequiresKey

// MissingServers returns the servers a rejected skill requires that the
// registry does not have, in the registry's order; nil for any other error.
func (e *APIError) MissingServers() []string {
	if e.StatusCode != http.StatusUnprocessableEntity {
		return nil
	}
	var names []string
	for _, pe := range e.Errors {
		if name, ok := pe.Value.(string); ok && pe.Location == requiresLocation {
			names = append(names, name)
		}
	}
	return names
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
	case http.StatusRequestEntityTooLarge:
		msg = ErrTooLarge.Error()
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

// Is lets errors.Is match ErrNotFound, ErrConflict, ErrValidation and
// ErrTooLarge by status code.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrConflict:
		return e.StatusCode == http.StatusConflict
	case ErrValidation:
		return e.StatusCode == http.StatusUnprocessableEntity
	case ErrTooLarge:
		return e.StatusCode == http.StatusRequestEntityTooLarge
	}
	return false
}

// readProblem decodes a problem+json error body. The detail it returns is
// the "detail" field followed by the errors[] entries ("location: message",
// joined by "; "), or "" when the body is empty or not JSON.
func readProblem(r io.Reader) (string, []ProblemError) {
	var p struct {
		Detail string         `json:"detail"`
		Errors []ProblemError `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(r, maxErrorBody)).Decode(&p); err != nil {
		return "", nil
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
	switch {
	case len(entries) == 0:
		return p.Detail, p.Errors
	case p.Detail == "":
		return strings.Join(entries, "; "), p.Errors
	}
	return p.Detail + ": " + strings.Join(entries, "; "), p.Errors
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

// do executes an authenticated JSON request: body (may be nil) is sent as
// JSON and the response is decoded into out (may be nil).
func (c *Client) do(ctx context.Context, method, path string, body any, out any) (*http.Response, error) {
	var (
		payload     []byte
		contentType string
	)
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("registry: marshal request: %w", err)
		}
		payload, contentType = b, "application/json"
	}
	resp, err := c.send(ctx, method, path, contentType, "application/json", payload)
	if err != nil {
		return resp, err
	}
	defer resp.Body.Close()
	if out != nil && resp.ContentLength != 0 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp, fmt.Errorf("registry: decode response: %w", err)
		}
	}
	return resp, nil
}

// send executes an authenticated HTTP request with a raw body (may be nil)
// of contentType. On a 2xx response the caller reads and closes resp.Body;
// any other response is turned into an error and its body is closed.
func (c *Client) send(ctx context.Context, method, path, contentType, accept string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("registry: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", accept)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
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
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		return resp, fmt.Errorf("registry: token invalid or expired — run 'mcpfleet auth login'")
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		detail, errs := readProblem(resp.Body)
		return resp, &APIError{StatusCode: resp.StatusCode, Detail: detail, Errors: errs}
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

// ConfigDir returns the mcpfleet config directory (~/.config/mcpfleet),
// which holds the token, the saved registry URL, the vault and the skills
// state.
func ConfigDir() (string, error) { return configDir() }

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
