package tui

import (
	"context"
	"errors"
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mcpfleet/mcpfleet/internal/skillstate"
)

// applyCall is one call a fakeBackend received.
type applyCall struct {
	agent, skill string
	force        bool
}

// fakeBackend serves fixed skills and records applies; it touches no HOME
// and no network.
type fakeBackend struct {
	data    SkillsData
	loadErr error
	loads   int
	calls   []applyCall
	// results answers the calls in order; a call past the end succeeds.
	results []error
}

func (f *fakeBackend) LoadSkills(context.Context) (SkillsData, error) {
	f.loads++
	return f.data, f.loadErr
}

func (f *fakeBackend) ApplySkill(_ context.Context, agent, skill string, force bool) (string, error) {
	f.calls = append(f.calls, applyCall{agent, skill, force})
	if n := len(f.calls) - 1; n < len(f.results) && f.results[n] != nil {
		return "", f.results[n]
	}
	return fmt.Sprintf("applied %s to %s", skill, agent), nil
}

func testSkillsData() SkillsData {
	return SkillsData{
		Agents: []string{"claude-code", "cursor", "windsurf"},
		Skills: []SkillRow{
			{
				Name:        "gh-notes",
				Description: "Keep notes on GitHub issues.",
				Requires:    []string{"github"},
				States: map[string]skillstate.Status{
					"claude-code": skillstate.UpToDate,
					"cursor":      skillstate.Unsupported,
					"windsurf":    skillstate.Modified,
				},
			},
			{
				Name:        "pdf",
				Description: "Read PDF files.",
				States: map[string]skillstate.Status{
					"claude-code": skillstate.OutOfDate,
					"cursor":      skillstate.Unsupported,
					"windsurf":    skillstate.NotConformant,
				},
			},
			{
				Name:        "lint",
				Description: "Lint the repo.",
				States: map[string]skillstate.Status{
					"claude-code": skillstate.NotInstalled,
					"cursor":      skillstate.Unsupported,
					"windsurf":    skillstate.NotInstalled,
				},
			},
		},
	}
}

// update feeds msg to m and returns the new model and command.
func update(t *testing.T, m model, msg tea.Msg) (model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	nm, ok := next.(model)
	require.True(t, ok, "Update returns a model")
	return nm, cmd
}

func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func press(t *testing.T, m model, keys ...string) (model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		m, cmd = update(t, m, keyMsg(k))
	}
	return m, cmd
}

// skillsModel returns a ready model on the Skills tab with the skills of
// backend loaded.
func skillsModel(t *testing.T, backend *fakeBackend) model {
	t.Helper()
	m := newModel(backend)
	m, _ = update(t, m, initDataMsg{})
	msg := m.loadSkills()()
	m, _ = update(t, m, msg)
	m, _ = press(t, m, "4")
	require.Equal(t, skillsTab, m.activeTab)
	return m
}

// run executes cmd and returns its message; cmd must not be nil.
func run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	require.NotNil(t, cmd, "a command is returned")
	return cmd()
}

func TestSkillsTab_ListsSkillsWithRequiresAndStates(t *testing.T) {
	backend := &fakeBackend{data: testSkillsData()}
	m := skillsModel(t, backend)
	assert.Equal(t, 1, backend.loads)

	view := m.View()
	assert.Contains(t, view, "Skills")
	for _, want := range []string{
		"gh-notes", "pdf", "lint", "github",
		"claude-code", "cursor", "windsurf",
		"installed", "not installed", "out of date", "modified locally",
		"not spec-conformant", "no skills support",
		"Keep notes on GitHub issues.", // description of the selected skill
	} {
		assert.Contains(t, view, want)
	}
}

func TestSkillsTab_TabKeyReachesSkills(t *testing.T) {
	m := newModel(&fakeBackend{data: testSkillsData()})
	m, _ = update(t, m, initDataMsg{})
	m, _ = press(t, m, "tab", "tab", "tab")
	assert.Equal(t, skillsTab, m.activeTab)
	m, _ = press(t, m, "tab")
	assert.Equal(t, dashboardTab, m.activeTab)
}

func TestSkillsTab_LoadErrorIsShown(t *testing.T) {
	m := skillsModel(t, &fakeBackend{loadErr: errors.New("registry: not authenticated")})
	assert.Contains(t, m.View(), "registry: not authenticated")
}

func TestSkillsTab_EmptyRegistry(t *testing.T) {
	m := skillsModel(t, &fakeBackend{data: SkillsData{Agents: []string{"claude-code"}}})
	assert.Contains(t, m.View(), "No skills")
}

func TestSkillsTab_NavigationStaysInBounds(t *testing.T) {
	m := skillsModel(t, &fakeBackend{data: testSkillsData()})
	m, _ = press(t, m, "up", "left")
	assert.Equal(t, 0, m.skillRow)
	assert.Equal(t, 0, m.skillAgent)
	m, _ = press(t, m, "down", "down", "down", "right", "right", "right")
	assert.Equal(t, 2, m.skillRow)
	assert.Equal(t, 2, m.skillAgent)
	m, _ = press(t, m, "k", "h")
	assert.Equal(t, 1, m.skillRow)
	assert.Equal(t, 1, m.skillAgent)
	assert.Contains(t, m.View(), "Read PDF files.")
}

func TestSkillsApply_AppliesSelectedSkillToSelectedAgent(t *testing.T) {
	backend := &fakeBackend{data: testSkillsData()}
	m := skillsModel(t, backend)

	// pdf on claude-code: out of date.
	m, cmd := press(t, m, "down", "a")
	assert.True(t, m.applying)
	msg := run(t, cmd)
	assert.Equal(t, []applyCall{{"claude-code", "pdf", false}}, backend.calls)

	m, cmd = update(t, m, msg)
	assert.False(t, m.applying)
	assert.Contains(t, m.View(), "applied pdf to claude-code")
	// The skills are reloaded, so the states follow the apply.
	reload := run(t, cmd)
	_, ok := reload.(skillsLoadedMsg)
	assert.True(t, ok, "apply reloads the skills, got %T", reload)
	assert.Equal(t, 2, backend.loads)
}

func TestSkillsApply_ErrorIsShown(t *testing.T) {
	backend := &fakeBackend{data: testSkillsData(), results: []error{errors.New("missing secrets in vault: gh_token")}}
	m := skillsModel(t, backend)

	m, cmd := press(t, m, "a")
	m, _ = update(t, m, run(t, cmd))
	assert.Contains(t, m.View(), "missing secrets in vault: gh_token")
	assert.Nil(t, m.confirm, "an error that is no conflict asks nothing")
	assert.Len(t, backend.calls, 1)
}

func TestSkillsApply_ConflictNeedsConfirmationThenForces(t *testing.T) {
	conflict := fmt.Errorf("1 skill conflict(s); nothing was changed for them: %w", skillstate.ErrConflict)
	backend := &fakeBackend{data: testSkillsData(), results: []error{conflict, conflict}}
	m := skillsModel(t, backend)

	// gh-notes on windsurf: modified locally.
	m, cmd := press(t, m, "right", "right", "a")
	m, _ = update(t, m, run(t, cmd))
	require.NotNil(t, m.confirm)
	view := m.View()
	assert.Contains(t, view, "Overwrite")
	assert.Contains(t, view, "[y/n]")

	// n cancels without applying.
	m, cmd = press(t, m, "n")
	assert.Nil(t, m.confirm)
	assert.Nil(t, cmd)
	assert.Len(t, backend.calls, 1)

	// Other keys do not move the cursor while the question is open.
	m, cmd = press(t, m, "a")
	m, _ = update(t, m, run(t, cmd))
	require.NotNil(t, m.confirm)
	m, _ = press(t, m, "left", "down")
	assert.Equal(t, 0, m.skillRow)
	assert.Equal(t, 2, m.skillAgent)

	// y applies the same skill to the same agent with force.
	m, cmd = press(t, m, "y")
	assert.Nil(t, m.confirm)
	m, _ = update(t, m, run(t, cmd))
	assert.Equal(t, []applyCall{
		{"windsurf", "gh-notes", false},
		{"windsurf", "gh-notes", false},
		{"windsurf", "gh-notes", true},
	}, backend.calls)
	assert.Contains(t, m.View(), "applied gh-notes to windsurf")
}

func TestSkillsApply_EscCancelsConfirmation(t *testing.T) {
	conflict := fmt.Errorf("conflict: %w", skillstate.ErrConflict)
	backend := &fakeBackend{data: testSkillsData(), results: []error{conflict}}
	m := skillsModel(t, backend)
	m, cmd := press(t, m, "right", "right", "a")
	m, _ = update(t, m, run(t, cmd))
	require.NotNil(t, m.confirm)
	m, cmd = press(t, m, "esc")
	assert.Nil(t, m.confirm)
	assert.Nil(t, cmd)
}

func TestSkillsApply_AgentWithoutSkillsSupportIsRefused(t *testing.T) {
	backend := &fakeBackend{data: testSkillsData()}
	m := skillsModel(t, backend)
	m, cmd := press(t, m, "right", "a")
	assert.Nil(t, cmd)
	assert.Empty(t, backend.calls)
	assert.Contains(t, m.View(), "cursor does not support skills")
}

func TestSkillsApply_IgnoredWhileApplying(t *testing.T) {
	backend := &fakeBackend{data: testSkillsData()}
	m := skillsModel(t, backend)
	m, cmd := press(t, m, "a")
	require.NotNil(t, cmd)
	m, cmd = press(t, m, "a")
	assert.Nil(t, cmd, "a second apply waits for the first")
	assert.True(t, m.applying)
}

func TestSkillsApply_OnlyOnSkillsTab(t *testing.T) {
	backend := &fakeBackend{data: testSkillsData()}
	m := skillsModel(t, backend)
	m, _ = press(t, m, "2", "a")
	assert.Empty(t, backend.calls)
}
