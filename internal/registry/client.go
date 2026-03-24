// Package registry communicates with the mcpfleet cloud registry.
// It fetches server definitions for the authenticated user.
package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mcpfleet/mcpfleet/internal/schema"
)

const (
	defaultBaseURL = "https://registry.mcpfleet.dev"
	tokenFile      = "token"
	registryURLFile = "registry_url"
)

// Client talks to the mcpfleet registry API.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

// New returns a Client using the stored auth token.
func New() (*Client, error) {
	token, err := LoadToken()
	if err != nil {
		return nil, fmt.Errorf("registry: not authenticated — run 'mcpfleet auth login' first")
	}
	baseURL := os.Getenv("MCPFLEET_REGISTRY_URL")
	if baseURL == "" {
		baseURL, _ = LoadRegistryURL()
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL:    baseURL,
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
		return nil, fmt.Errorf("registry: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return resp, fmt.Errorf("registry: token invalid or expired — run 'mcpfleet auth login'")
	}
	if resp.StatusCode == http.StatusNotFound {
		return resp, fmt.Errorf("registry: not found")
	}
	if resp.StatusCode >= 300 {
		return resp, fmt.Errorf("registry: unexpected status %d", resp.StatusCode)
	}
	if out != nil && resp.ContentLength != 0 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp, fmt.Errorf("registry: decode response: %w", err)
		}
	}
	return resp, nil
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
	if _, err := c.do(ctx, http.MethodGet, "/v1/servers/"+id, nil, &result); err != nil {
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
	if _, err := c.do(ctx, http.MethodPut, "/v1/servers/"+id, srv, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteServer removes a server from the registry by ID.
func (c *Client) DeleteServer(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/v1/servers/"+id, nil, nil)
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
