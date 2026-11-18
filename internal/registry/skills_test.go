package registry

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/mcpfleet/pkg/skillspec"
)

const skillJSON = `{"name":"notes","description":"Take notes.","license":"MIT","compatibility":"",
	"allowed_tools":"Read","metadata":{"mcpfleet-requires":"github"},"requires":["github"],
	"sha256":"abc123","size":42,"created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-02T10:00:00Z"}`

var wantSkill = Skill{
	Name: "notes", Description: "Take notes.", License: "MIT", AllowedTools: "Read",
	Metadata: map[string]string{"mcpfleet-requires": "github"}, Requires: []string{"github"},
	SHA256: "abc123", Size: 42, CreatedAt: "2026-11-01T10:00:00Z", UpdatedAt: "2026-11-02T10:00:00Z",
}

func TestListSkills(t *testing.T) {
	rec := &recorder{body: "[" + skillJSON + "]"}
	c := newTestClient(t, rec)

	skills, err := c.ListSkills(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []Skill{wantSkill}, skills)

	r := rec.last(t)
	assert.Equal(t, http.MethodGet, r.method)
	assert.Equal(t, "/v1/skills", r.path)
	assert.Equal(t, "Bearer mcp_test", r.auth)
}

func TestGetSkill(t *testing.T) {
	rec := &recorder{body: skillJSON}
	c := newTestClient(t, rec)

	s, err := c.GetSkill(context.Background(), "notes")
	require.NoError(t, err)
	assert.Equal(t, wantSkill, *s)
	assert.Equal(t, "/v1/skills/notes", rec.last(t).path)
}

func TestPutSkill_SendsRawArchive(t *testing.T) {
	rec := &recorder{body: `{"name":"notes","sha256":"abc123","requires":[],"warnings":[{"field":"SKILL.md","rule":"line-count","message":"too long"}]}`}
	c := newTestClient(t, rec)
	archive := []byte("\x1f\x8b fake archive")

	res, err := c.PutSkill(context.Background(), "notes", archive)
	require.NoError(t, err)
	assert.Equal(t, "notes", res.Name)
	assert.Equal(t, "abc123", res.SHA256)
	assert.Equal(t, []SkillViolation{{Field: "SKILL.md", Rule: "line-count", Message: "too long"}}, res.Warnings)

	r := rec.last(t)
	assert.Equal(t, http.MethodPut, r.method)
	assert.Equal(t, "/v1/skills/notes", r.path)
	assert.Equal(t, "application/gzip", r.contentType)
	assert.Equal(t, "application/json", r.accept)
	assert.Equal(t, archive, r.body, "the body is the archive itself, not JSON")
}

func TestPutSkill_MissingServers(t *testing.T) {
	c := newTestClient(t, &recorder{status: http.StatusUnprocessableEntity, body: `{"status":422,
		"title":"Unprocessable Entity","detail":"skill requires servers the registry does not have",
		"errors":[{"location":"metadata.mcpfleet-requires","message":"server \"github\" is not in the registry","value":"github"},
		{"location":"metadata.mcpfleet-requires","message":"server \"slack\" is not in the registry","value":"slack"}]}`})

	_, err := c.PutSkill(context.Background(), "notes", []byte("x"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrValidation)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, []string{"github", "slack"}, apiErr.MissingServers())
	assert.Contains(t, err.Error(), `metadata.mcpfleet-requires: server "github" is not in the registry`)
}

func TestPutSkill_SpecViolationsAreNotMissingServers(t *testing.T) {
	c := newTestClient(t, &recorder{status: http.StatusUnprocessableEntity, body: `{"status":422,
		"detail":"skill does not conform to the Agent Skills specification",
		"errors":[{"location":"name","message":"must be lowercase (got \"Notes\") (lowercase)"}]}`})

	_, err := c.PutSkill(context.Background(), "notes", []byte("x"))
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Empty(t, apiErr.MissingServers())
	assert.EqualError(t, err, `registry: validation failed: skill does not conform to the Agent Skills specification: name: must be lowercase (got "Notes") (lowercase)`)
}

func TestPutSkill_TooLarge(t *testing.T) {
	c := newTestClient(t, &recorder{status: http.StatusRequestEntityTooLarge,
		body: `{"status":413,"detail":"request body is too large limit=1048577 bytes"}`})

	_, err := c.PutSkill(context.Background(), "notes", []byte("x"))
	assert.ErrorIs(t, err, ErrTooLarge)
	assert.EqualError(t, err, "registry: archive too large: request body is too large limit=1048577 bytes")
}

// archiveHandler serves GET /v1/skills/notes/archive with body and, when
// sha is not empty, the X-Skill-Sha256 header.
type archiveHandler struct {
	body   []byte
	sha    string
	accept string
	path   string
}

func (h *archiveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.accept = r.Header.Get("Accept")
	h.path = r.URL.Path
	w.Header().Set("Content-Type", "application/gzip")
	if h.sha != "" {
		w.Header().Set("X-Skill-Sha256", h.sha)
	}
	_, _ = w.Write(h.body)
}

func TestGetSkillArchive(t *testing.T) {
	body := []byte("\x1f\x8b\x08 archive bytes")
	h := &archiveHandler{body: body, sha: "abc123"}
	c := newTestClient(t, h)

	archive, sha, err := c.GetSkillArchive(context.Background(), "notes")
	require.NoError(t, err)
	assert.Equal(t, body, archive)
	assert.Equal(t, "abc123", sha)
	assert.Equal(t, "/v1/skills/notes/archive", h.path)
	assert.Contains(t, h.accept, "application/gzip")
}

func TestGetSkillArchive_TooLarge(t *testing.T) {
	c := newTestClient(t, &archiveHandler{body: bytes.Repeat([]byte{0}, skillspec.MaxArchiveBytes+1), sha: "x"})

	_, _, err := c.GetSkillArchive(context.Background(), "notes")
	assert.ErrorContains(t, err, "more than 1048576 bytes")
}

func TestGetSkillArchive_NotFound(t *testing.T) {
	c := newTestClient(t, &recorder{status: http.StatusNotFound, body: `{"detail":"skill not found"}`})

	_, _, err := c.GetSkillArchive(context.Background(), "notes")
	assert.ErrorIs(t, err, ErrNotFound)
	assert.EqualError(t, err, "registry: not found: skill not found")
}

func TestDeleteSkill(t *testing.T) {
	rec := &recorder{status: http.StatusNoContent}
	c := newTestClient(t, rec)

	require.NoError(t, c.DeleteSkill(context.Background(), "notes"))
	r := rec.last(t)
	assert.Equal(t, http.MethodDelete, r.method)
	assert.Equal(t, "/v1/skills/notes", r.path)
}

func TestSkillPaths_EscapeName(t *testing.T) {
	ctx := context.Background()
	calls := map[string]func(c *Client) error{
		"get":     func(c *Client) error { _, err := c.GetSkill(ctx, "a/b c"); return err },
		"put":     func(c *Client) error { _, err := c.PutSkill(ctx, "a/b c", []byte("x")); return err },
		"delete":  func(c *Client) error { return c.DeleteSkill(ctx, "a/b c") },
		"archive": func(c *Client) error { _, _, err := c.GetSkillArchive(ctx, "a/b c"); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			rec := &recorder{status: http.StatusNotFound}
			c := newTestClient(t, rec)
			err := call(c)
			assert.True(t, errors.Is(err, ErrNotFound), "%v", err)
			r := rec.last(t)
			assert.Regexp(t, `^/v1/skills/a%2Fb%20c(/archive)?$`, r.escapedPath)
		})
	}
}

func TestSkillErrors_Unauthorized(t *testing.T) {
	c := newTestClient(t, &recorder{status: http.StatusUnauthorized})
	_, _, err := c.GetSkillArchive(context.Background(), "notes")
	assert.ErrorContains(t, err, "token invalid or expired")
	_, err = c.PutSkill(context.Background(), "notes", []byte("x"))
	assert.ErrorContains(t, err, "token invalid or expired")
}
