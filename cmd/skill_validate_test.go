package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func skillFixture(verdict, name string) string {
	return filepath.Join("..", "pkg", "skillspec", "testdata", verdict, name)
}

func TestSkillValidate(t *testing.T) {
	tests := []struct {
		name       string
		dir        string
		wantCode   int
		wantStdout []string
		wantStderr string
	}{
		{
			name:       "valid skill",
			dir:        skillFixture("valid", "minimal"),
			wantCode:   0,
			wantStdout: []string{"valid: " + skillFixture("valid", "minimal")},
		},
		{
			name:     "warnings keep exit code 0",
			dir:      skillFixture("valid", "long-body"),
			wantCode: 0,
			wantStdout: []string{
				"warning: SKILL.md: has 514 lines, the spec recommends at most 500",
				"valid: " + skillFixture("valid", "long-body"),
			},
		},
		{
			name:       "one violation per line",
			dir:        skillFixture("invalid", "leading-hyphen"),
			wantCode:   1,
			wantStdout: []string{"error: name: must not start or end with a hyphen", `error: name: must match the directory name "leading-hyphen" (got "-leading-hyphen")`},
			wantStderr: "Error: " + skillFixture("invalid", "leading-hyphen") + " is not a valid skill (2 errors)\n",
		},
		{
			name:       "missing SKILL.md",
			dir:        skillFixture("invalid", "missing-skill-md"),
			wantCode:   1,
			wantStdout: []string{"error: SKILL.md: missing required file SKILL.md"},
			wantStderr: "Error: " + skillFixture("invalid", "missing-skill-md") + " is not a valid skill (1 error)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runForTest(t, []string{"skill", "validate", tt.dir}, &stdout, &stderr)
			assert.Equal(t, tt.wantCode, code)
			assert.Equal(t, strings.Join(tt.wantStdout, "\n")+"\n", stdout.String())
			assert.Equal(t, tt.wantStderr, stderr.String())
		})
	}
}

func TestSkillValidate_NeedsOneDirectory(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runForTest(t, []string{"skill", "validate"}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "accepts 1 arg(s), received 0")
}
