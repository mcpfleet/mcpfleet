package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mcpfleet/mcpfleet/internal/skillstate"
)

// SkillRow is one registry skill with its state on every agent.
type SkillRow struct {
	Name        string
	Description string
	Requires    []string
	// States holds the skill's state by agent name.
	States map[string]skillstate.Status
}

// SkillsData is what the Skills tab shows: the agents, in column order, and
// the registry's skills.
type SkillsData struct {
	Agents []string
	Skills []SkillRow
}

// SkillBackend loads the skills and applies them. The TUI holds no apply
// logic of its own: the command layer implements this on top of the same
// code "mcpfleet apply --skill" runs, and tests inject a fake.
type SkillBackend interface {
	// LoadSkills returns the registry's skills with their state per agent.
	LoadSkills(ctx context.Context) (SkillsData, error)
	// ApplySkill installs skill, with the servers it requires, on agent and
	// returns a one-line summary. An error wrapping skillstate.ErrConflict
	// means the local copy was changed and was left alone; force replaces
	// it.
	ApplySkill(ctx context.Context, agent, skill string, force bool) (string, error)
}

// skillsLoadedMsg carries the result of SkillBackend.LoadSkills.
type skillsLoadedMsg struct {
	data SkillsData
	err  error
}

// skillTarget is a skill on an agent.
type skillTarget struct {
	skill, agent string
}

// skillAppliedMsg carries the result of SkillBackend.ApplySkill.
type skillAppliedMsg struct {
	target  skillTarget
	summary string
	err     error
}

var (
	selectedCellStyle = lipgloss.NewStyle().
				Foreground(colorBg).
				Background(colorAccent).
				Bold(true)

	warnStyle = lipgloss.NewStyle().
			Foreground(colorYellow)
)

// loadSkills returns the command that loads the skills, or nil without a
// backend.
func (m model) loadSkills() tea.Cmd {
	backend := m.backend
	if backend == nil {
		return nil
	}
	return func() tea.Msg {
		data, err := backend.LoadSkills(context.Background())
		return skillsLoadedMsg{data: data, err: err}
	}
}

// applySkill returns the command that applies target.
func (m model) applySkill(target skillTarget, force bool) tea.Cmd {
	backend := m.backend
	return func() tea.Msg {
		summary, err := backend.ApplySkill(context.Background(), target.agent, target.skill, force)
		return skillAppliedMsg{target: target, summary: summary, err: err}
	}
}

// selected returns the skill and agent under the cursor.
func (m model) selected() (SkillRow, string, bool) {
	if len(m.skills.Skills) == 0 || len(m.skills.Agents) == 0 {
		return SkillRow{}, "", false
	}
	return m.skills.Skills[m.skillRow], m.skills.Agents[m.skillAgent], true
}

// updateSkillsKey handles a key on the Skills tab while no question is open.
func (m model) updateSkillsKey(k string) (model, tea.Cmd) {
	switch k {
	case "up", "k":
		if m.skillRow > 0 {
			m.skillRow--
		}
	case "down", "j":
		if m.skillRow < len(m.skills.Skills)-1 {
			m.skillRow++
		}
	case "left", "h":
		if m.skillAgent > 0 {
			m.skillAgent--
		}
	case "right", "l":
		if m.skillAgent < len(m.skills.Agents)-1 {
			m.skillAgent++
		}
	case "a":
		if m.applying || m.backend == nil {
			return m, nil
		}
		skill, agent, ok := m.selected()
		if !ok {
			return m, nil
		}
		if skill.States[agent] == skillstate.Unsupported {
			m.skillNote, m.skillNoteErr = fmt.Sprintf("%s does not support skills", agent), true
			return m, nil
		}
		target := skillTarget{skill: skill.Name, agent: agent}
		m.applying = true
		m.skillNote, m.skillNoteErr = fmt.Sprintf("Applying %s to %s…", target.skill, target.agent), false
		return m, m.applySkill(target, false)
	}
	return m, nil
}

// updateConfirmKey handles a key while the overwrite question is open:
// y applies with force, n or esc cancels, anything else is ignored.
func (m model) updateConfirmKey(k string) (model, tea.Cmd) {
	target := *m.confirm
	switch k {
	case "y", "Y":
		m.confirm = nil
		m.applying = true
		m.skillNote, m.skillNoteErr = fmt.Sprintf("Overwriting %s on %s…", target.skill, target.agent), false
		return m, m.applySkill(target, true)
	case "n", "N", "esc":
		m.confirm = nil
		m.skillNote, m.skillNoteErr = fmt.Sprintf("Kept the local copy of %s on %s", target.skill, target.agent), false
	}
	return m, nil
}

// skillApplied records the result of an apply: a conflict opens the
// overwrite question, anything else is shown and the skills are reloaded.
func (m model) skillApplied(msg skillAppliedMsg) (model, tea.Cmd) {
	m.applying = false
	switch {
	case errors.Is(msg.err, skillstate.ErrConflict):
		t := msg.target
		m.confirm = &t
		m.skillNote, m.skillNoteErr = msg.err.Error(), true
		return m, nil
	case msg.err != nil:
		m.skillNote, m.skillNoteErr = msg.err.Error(), true
	default:
		m.skillNote, m.skillNoteErr = msg.summary, false
	}
	return m, m.loadSkills()
}

// skillsLoaded stores loaded skills and keeps the cursor inside them.
func (m model) skillsLoaded(msg skillsLoadedMsg) model {
	m.skills, m.skillsErr, m.skillsReady = msg.data, msg.err, true
	m.skillRow = clamp(m.skillRow, len(m.skills.Skills))
	m.skillAgent = clamp(m.skillAgent, len(m.skills.Agents))
	return m
}

func clamp(i, n int) int {
	if i >= n {
		i = n - 1
	}
	if i < 0 {
		i = 0
	}
	return i
}

// statusStyle colours a state label.
func statusStyle(s skillstate.Status) lipgloss.Style {
	switch s {
	case skillstate.UpToDate:
		return statusOkStyle
	case skillstate.OutOfDate:
		return lipgloss.NewStyle().Foreground(colorBlue)
	case skillstate.Modified:
		return lipgloss.NewStyle().Foreground(colorOrange)
	case skillstate.NotConformant:
		return statusErrorStyle
	}
	return subtleStyle
}

func (m model) renderSkills() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render(fmt.Sprintf("Skills (%d)", len(m.skills.Skills))))
	b.WriteString("\n\n")

	switch {
	case !m.skillsReady:
		b.WriteString(subtleStyle.Render("Loading skills…"))
		return b.String()
	case m.skillsErr != nil:
		b.WriteString(statusErrorStyle.Render("Cannot load skills: " + m.skillsErr.Error()))
		return b.String()
	case len(m.skills.Skills) == 0:
		b.WriteString(subtleStyle.Render("No skills in the registry yet.\n"))
		b.WriteString(subtleStyle.Render("Run ") + accentStyle.Render("mcpfleet skill push <dir>") + subtleStyle.Render(" to add one."))
		return b.String()
	}

	// Column widths: the longest entry of each column.
	nameW, reqW := len("SKILL"), len("REQUIRES")
	for _, s := range m.skills.Skills {
		nameW = max(nameW, len(s.Name))
		reqW = max(reqW, len(requiresText(s)))
	}
	agentW := make([]int, len(m.skills.Agents))
	for i, a := range m.skills.Agents {
		agentW[i] = len(a)
		for _, s := range m.skills.Skills {
			agentW[i] = max(agentW[i], len(stateText(s, a)))
		}
	}

	header := "  " + pad("SKILL", nameW) + "  " + pad("REQUIRES", reqW)
	for i, a := range m.skills.Agents {
		cell := pad(a, agentW[i])
		if i == m.skillAgent {
			cell = accentStyle.Render(cell)
		} else {
			cell = subtleStyle.Render(cell)
		}
		header += "  " + cell
	}
	b.WriteString(header + "\n")

	for r, s := range m.skills.Skills {
		cursor := "  "
		name := pad(s.Name, nameW)
		if r == m.skillRow {
			cursor = accentStyle.Render("▸ ")
			name = titleStyle.UnsetMarginRight().Render(name)
		}
		line := cursor + name + "  " + subtleStyle.Render(pad(requiresText(s), reqW))
		for i, a := range m.skills.Agents {
			cell := pad(stateText(s, a), agentW[i])
			if r == m.skillRow && i == m.skillAgent {
				cell = selectedCellStyle.Render(cell)
			} else {
				cell = statusStyle(stateOf(s, a)).Render(cell)
			}
			line += "  " + cell
		}
		b.WriteString(line + "\n")
	}

	if skill, agent, ok := m.selected(); ok {
		b.WriteString("\n")
		b.WriteString(accentStyle.Render(skill.Name) + subtleStyle.Render(" — "+oneLine(skill.Description)))
		b.WriteString("\n")
		b.WriteString(subtleStyle.Render("on "+agent+": ") + statusStyle(stateOf(skill, agent)).Render(stateText(skill, agent)))
	}

	if m.skillNote != "" {
		b.WriteString("\n\n")
		if m.skillNoteErr {
			b.WriteString(warnStyle.Render(m.skillNote))
		} else {
			b.WriteString(statusOkStyle.Render(m.skillNote))
		}
	}
	if m.confirm != nil {
		b.WriteString("\n")
		b.WriteString(warnStyle.Bold(true).Render(fmt.Sprintf(
			"Overwrite local changes to %s on %s with the registry's copy? [y/n]", m.confirm.skill, m.confirm.agent)))
	}
	return b.String()
}

// stateOf returns the state of s on agent; an agent the backend left out
// reads as unsupported.
func stateOf(s SkillRow, agent string) skillstate.Status {
	st, ok := s.States[agent]
	if !ok {
		return skillstate.Unsupported
	}
	return st
}

func stateText(s SkillRow, agent string) string { return stateOf(s, agent).String() }

func requiresText(s SkillRow) string {
	if len(s.Requires) == 0 {
		return "-"
	}
	return strings.Join(s.Requires, ",")
}

// pad right-pads s with spaces to n bytes.
func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// oneLine joins the lines of s with spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
