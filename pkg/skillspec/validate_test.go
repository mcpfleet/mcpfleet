package skillspec

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fieldRule struct{ Field, Rule string }

func fieldRules(vs []Violation) []fieldRule {
	out := make([]fieldRule, 0, len(vs))
	for _, v := range vs {
		out = append(out, fieldRule{v.Field, v.Rule})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].Rule < out[j].Rule
	})
	return out
}

func fixtureDirs(t *testing.T, verdict string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("testdata", verdict))
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	require.NotEmpty(t, names)
	return names
}

// Every fixture under testdata/valid passes and every one under
// testdata/invalid fails: the same verdict scripts/skills-ref-parity.sh
// expects from skills-ref.
func TestValidate_FixtureVerdicts(t *testing.T) {
	for _, name := range fixtureDirs(t, "valid") {
		t.Run("valid/"+name, func(t *testing.T) {
			res := Validate(filepath.Join("testdata", "valid", name))
			assert.Empty(t, res.Errors)
			assert.True(t, res.Valid())
		})
	}
	for _, name := range fixtureDirs(t, "invalid") {
		t.Run("invalid/"+name, func(t *testing.T) {
			res := Validate(filepath.Join("testdata", "invalid", name))
			assert.NotEmpty(t, res.Errors)
			assert.False(t, res.Valid())
		})
	}
}

func TestValidate_InvalidFixtures(t *testing.T) {
	tests := []struct {
		fixture string
		want    []fieldRule
	}{
		{"missing-skill-md", []fieldRule{{"SKILL.md", RuleRequiredFile}}},
		{"no-frontmatter", []fieldRule{{"SKILL.md", RuleFrontmatter}}},
		{"unclosed-frontmatter", []fieldRule{{"SKILL.md", RuleFrontmatter}}},
		{"byte-order-mark", []fieldRule{{"SKILL.md", RuleFrontmatter}}},
		{"frontmatter-not-mapping", []fieldRule{{"SKILL.md", RuleFrontmatter}}},
		{"empty-frontmatter", []fieldRule{{"SKILL.md", RuleFrontmatter}}},
		{"flow-style", []fieldRule{{"SKILL.md", RuleYAML}}},
		{"duplicate-key", []fieldRule{{"SKILL.md", RuleYAML}}},
		{"anchor-alias", []fieldRule{{"SKILL.md", RuleYAML}}},
		{"yaml-tag", []fieldRule{{"SKILL.md", RuleYAML}}},
		{"unknown-field", []fieldRule{{"version", RuleUnknownField}}},
		{"missing-name", []fieldRule{{"name", RuleRequired}}},
		{"empty-name", []fieldRule{{"name", RuleNonEmpty}}},
		{"name-not-string", []fieldRule{{"name", RuleType}}},
		{"Uppercase", []fieldRule{{"name", RuleLowercase}}},
		{"trailing-", []fieldRule{{"name", RuleHyphenEdge}}},
		{"leading-hyphen", []fieldRule{{"name", RuleDirectoryMatch}, {"name", RuleHyphenEdge}}},
		{"double--hyphen", []fieldRule{{"name", RuleConsecutiveHyphens}}},
		{"under_score", []fieldRule{{"name", RuleCharset}}},
		{strings.Repeat("b", 65), []fieldRule{{"name", RuleMaxLength}}},
		{"directory-mismatch", []fieldRule{{"name", RuleDirectoryMatch}}},
		{"missing-description", []fieldRule{{"description", RuleRequired}}},
		{"empty-description", []fieldRule{{"description", RuleNonEmpty}}},
		{"description-too-long", []fieldRule{{"description", RuleMaxLength}}},
		{"compatibility-too-long", []fieldRule{{"compatibility", RuleMaxLength}}},
		{"compatibility-not-string", []fieldRule{{"compatibility", RuleType}}},
	}
	covered := map[string]bool{}
	for _, tt := range tests {
		covered[tt.fixture] = true
		t.Run(tt.fixture, func(t *testing.T) {
			res := Validate(filepath.Join("testdata", "invalid", tt.fixture))
			assert.Equal(t, tt.want, fieldRules(res.Errors))
			for _, v := range res.Errors {
				assert.NotEmpty(t, v.Message)
			}
		})
	}
	for _, name := range fixtureDirs(t, "invalid") {
		assert.True(t, covered[name], "invalid fixture %q has no row in this table", name)
	}
}

func TestValidate_Messages(t *testing.T) {
	res := Validate(filepath.Join("testdata", "invalid", "directory-mismatch"))
	require.Len(t, res.Errors, 1)
	assert.Equal(t, `name: must match the directory name "directory-mismatch" (got "other-name")`, res.Errors[0].Error())

	res = Validate(filepath.Join("testdata", "invalid", "unknown-field"))
	require.Len(t, res.Errors, 1)
	assert.Equal(t, "version: unknown field (allowed: allowed-tools, compatibility, description, license, metadata, name)", res.Errors[0].Error())
}

func TestValidate_NameMatchesAbsoluteDirectory(t *testing.T) {
	// "." has no name of its own; the directory it points at does.
	dir, err := filepath.Abs(filepath.Join("testdata", "valid", "minimal"))
	require.NoError(t, err)
	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(wd) })
	res := Validate(".")
	assert.Empty(t, res.Errors)
}

func TestValidate_NameIsNFKCNormalized(t *testing.T) {
	// U+FB01 (the "fi" ligature) is "fi" after NFKC, as in skills-ref.
	dir := filepath.Join(t.TempDir(), "fine")
	require.NoError(t, os.Mkdir(dir, 0o755))
	content := "---\nname: ﬁne\ndescription: A ligature in the name.\n---\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644))
	res := Validate(dir)
	assert.Empty(t, res.Errors)
}

func TestValidate_MissingDirectory(t *testing.T) {
	res := Validate(filepath.Join(t.TempDir(), "nope"))
	assert.Equal(t, []fieldRule{{FieldDirectory, RuleDirectory}}, fieldRules(res.Errors))
}
