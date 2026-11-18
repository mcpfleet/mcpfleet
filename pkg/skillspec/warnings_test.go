package skillspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeSkill(t *testing.T, name, content string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644))
	return dir
}

func TestWarnings(t *testing.T) {
	tests := []struct {
		fixture string
		want    []fieldRule
	}{
		{"long-body", []fieldRule{{"SKILL.md", RuleLineCount}}},
		{"metadata-nested", []fieldRule{{"metadata.owner", RuleStringValues}}},
		{"metadata-not-mapping", []fieldRule{{"metadata", RuleType}}},
		{"minimal", []fieldRule{}},
		{"full-fields", []fieldRule{}},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			res := Validate(filepath.Join("testdata", "valid", tt.fixture))
			assert.Empty(t, res.Errors)
			assert.True(t, res.Valid())
			assert.Equal(t, tt.want, fieldRules(res.Warnings))
		})
	}
}

func TestWarnings_LineCountBoundary(t *testing.T) {
	header := "---\nname: lines\ndescription: Line count boundary.\n---\n"
	// The header is 4 lines; 496 more make exactly 500.
	at500 := header + strings.Repeat("x\n", 496)
	res := Validate(writeSkill(t, "lines", at500))
	assert.Empty(t, res.Warnings, "500 lines is within the recommendation")

	res = Validate(writeSkill(t, "lines", at500+"x\n"))
	require.Len(t, res.Warnings, 1)
	assert.Equal(t, "SKILL.md: has 501 lines, the spec recommends at most 500", res.Warnings[0].Error())
}

func TestWarnings_NonStringLicenseAndAllowedTools(t *testing.T) {
	res := Validate(writeSkill(t, "lists", "---\nname: lists\ndescription: Lists where strings belong.\nlicense:\n  - MIT\nallowed-tools:\n  - Read\n---\n"))
	assert.Empty(t, res.Errors)
	assert.Equal(t, []fieldRule{{"allowed-tools", RuleType}, {"license", RuleType}}, fieldRules(res.Warnings))
}

func TestWarnings_NoneWhenInvalid(t *testing.T) {
	res := Validate(filepath.Join("testdata", "invalid", "no-frontmatter"))
	assert.Empty(t, res.Warnings)
}

func TestWarnings_EmptyMetadataIsFine(t *testing.T) {
	res := Validate(writeSkill(t, "empty-meta", "---\nname: empty-meta\ndescription: An empty metadata field.\nmetadata:\n---\n"))
	assert.Empty(t, res.Errors)
	assert.Empty(t, res.Warnings)
}
