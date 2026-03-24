// Package registry communicates with the mcpfleet cloud registry.
// It fetches server definitions for the authenticated user.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mcpfleet/mcpfleet/internal/schema"
)

const (
	defaultBaseURL = "https://registry.mcpfleet.dev"
	tokenFile      = "token"
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
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		token:      token,
	}, nil
}

// ListServers fetches all MCP servers registered to the authenticated user.
func (c *Client) ListServers(ctx context.Context) ([]schema.Server, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/servers", nil)
	if err != nil {
		return nil, fmt.Errorf("registry: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("registry: token expired — run 'mcpfleet auth login'")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Servers []schema.Server `json:"servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("registry: decode response: %w", err)
	}
	return result.Servers, nil
}

// --- token helpers ---

// configDir returns the mcpfleet config directory.
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
	return string(data), nil
}

// DeleteToken removes the stored auth token (logout).
func DeleteToken() error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	return os.Remove(filepath.Join(dir, tokenFile))
}
