package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/mcpfleet/mcpfleet/pkg/skillspec"
)

// Skill is a skill as the registry lists it: its frontmatter, the servers
// it requires, the skillspec hash of its directory and the archive size.
type Skill struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	License       string            `json:"license"`
	Compatibility string            `json:"compatibility"`
	AllowedTools  string            `json:"allowed_tools"`
	Metadata      map[string]string `json:"metadata"`
	Requires      []string          `json:"requires"`
	SHA256        string            `json:"sha256"`
	Size          int64             `json:"size"`
	CreatedAt     string            `json:"created_at"`
	UpdatedAt     string            `json:"updated_at"`
}

// SkillViolation is a rule of the Agent Skills specification a skill breaks.
type SkillViolation struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// PutSkillResult is the registry's answer to an accepted upload: the stored
// skill and the spec recommendations it does not follow.
type PutSkillResult struct {
	Skill
	Warnings []SkillViolation `json:"warnings"`
}

// skillPath is the path of one skill, its name escaped as one path segment.
func skillPath(name string) string {
	return "/v1/skills/" + url.PathEscape(name)
}

// ListSkills fetches every skill in the registry.
func (c *Client) ListSkills(ctx context.Context) ([]Skill, error) {
	var result []Skill
	if _, err := c.do(ctx, http.MethodGet, "/v1/skills", nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetSkill fetches one skill's frontmatter and hash.
func (c *Client) GetSkill(ctx context.Context, name string) (*Skill, error) {
	var result Skill
	if _, err := c.do(ctx, http.MethodGet, skillPath(name), nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PutSkill creates or replaces the skill name with archive, a tar.gz from
// skillspec.Pack. A 422 is an *APIError whose MissingServers lists the
// required servers the registry does not have, if that is the reason.
func (c *Client) PutSkill(ctx context.Context, name string, archive []byte) (*PutSkillResult, error) {
	resp, err := c.send(ctx, http.MethodPut, skillPath(name), "application/gzip", "application/json", archive)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result PutSkillResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("registry: decode response: %w", err)
	}
	return &result, nil
}

// GetSkillArchive downloads the skill's tar.gz archive and returns it with
// the hash the registry sends in X-Skill-Sha256 ("" when it sends none). An
// archive above skillspec.MaxArchiveBytes is an error.
func (c *Client) GetSkillArchive(ctx context.Context, name string) ([]byte, string, error) {
	resp, err := c.send(ctx, http.MethodGet, skillPath(name)+"/archive", "", "application/gzip, application/problem+json", nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	archive, err := io.ReadAll(io.LimitReader(resp.Body, skillspec.MaxArchiveBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("registry: read archive: %w", err)
	}
	if len(archive) > skillspec.MaxArchiveBytes {
		return nil, "", fmt.Errorf("registry: archive of skill %q is more than %d bytes", name, skillspec.MaxArchiveBytes)
	}
	return archive, resp.Header.Get("X-Skill-Sha256"), nil
}

// DeleteSkill removes a skill from the registry.
func (c *Client) DeleteSkill(ctx context.Context, name string) error {
	_, err := c.do(ctx, http.MethodDelete, skillPath(name), nil, nil)
	return err
}
