package skillspec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_FullFields(t *testing.T) {
	s, err := Parse(filepath.Join("testdata", "valid", "full-fields"))
	require.NoError(t, err)

	assert.Equal(t, "full-fields", s.Name)
	assert.Equal(t, "Uses every field the spec defines.", s.Description)
	assert.Equal(t, "Apache-2.0", s.License)
	assert.Equal(t, "Requires git and network access.", s.Compatibility)
	assert.Equal(t, "Bash(git:*) Read", s.AllowedTools)
	assert.Equal(t, map[string]string{
		"author":            "mcpfleet",
		"version":           "1.0",
		"mcpfleet-requires": "github filesystem",
	}, s.Metadata)
	assert.Equal(t, []string{"github", "filesystem"}, s.Requires)
	assert.Equal(t, "# Full fields\n\nStep one.", s.Body)
	assert.Empty(t, s.Extra)
}

func TestParse_ScalarsAreStrings(t *testing.T) {
	// strictyaml, which skills-ref uses, reads every scalar as a string.
	s, err := Parse(filepath.Join("testdata", "valid", "123"))
	require.NoError(t, err)
	assert.Equal(t, "123", s.Name)
}

func TestParse_PreservesUnknownFields(t *testing.T) {
	s, err := Parse(filepath.Join("testdata", "invalid", "unknown-field"))
	require.NoError(t, err)

	require.Contains(t, s.Extra, "version")
	node := s.Extra["version"]
	assert.Equal(t, "1.0.0", node.Value)
}

func TestParse_NoRequiresWithoutMetadata(t *testing.T) {
	s, err := Parse(filepath.Join("testdata", "valid", "minimal"))
	require.NoError(t, err)
	assert.Nil(t, s.Requires)
	assert.Equal(t, "Use this skill when nothing else applies.", s.Body)
}

func TestParse_LowercaseFileName(t *testing.T) {
	s, err := Parse(filepath.Join("testdata", "valid", "lowercase-file"))
	require.NoError(t, err)
	assert.Equal(t, "lowercase-file", s.Name)
}

func TestParse_CRLFLineEndings(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crlf")
	require.NoError(t, os.Mkdir(dir, 0o755))
	content := "---\r\nname: crlf\r\ndescription: Windows line endings.\r\n---\r\nBody.\r\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644))

	s, err := Parse(dir)
	require.NoError(t, err)
	assert.Equal(t, "crlf", s.Name)
	assert.Equal(t, "Windows line endings.", s.Description)
	assert.Equal(t, "Body.", s.Body)
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		fixture string
		rule    string
	}{
		{"missing-skill-md", RuleRequiredFile},
		{"no-frontmatter", RuleFrontmatter},
		{"unclosed-frontmatter", RuleFrontmatter},
		{"byte-order-mark", RuleFrontmatter},
		{"frontmatter-not-mapping", RuleFrontmatter},
		{"empty-frontmatter", RuleFrontmatter},
		{"flow-style", RuleYAML},
		{"duplicate-key", RuleYAML},
		{"anchor-alias", RuleYAML},
		{"yaml-tag", RuleYAML},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			_, err := Parse(filepath.Join("testdata", "invalid", tt.fixture))
			require.Error(t, err)
			var v Violation
			require.ErrorAs(t, err, &v)
			assert.Equal(t, tt.rule, v.Rule)
			assert.Equal(t, FieldSkillMD, v.Field)
		})
	}
}
