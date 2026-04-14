package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mcpfleet/mcpfleet/internal/adapters"
	"github.com/mcpfleet/mcpfleet/internal/registry"
	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// Everforest-inspired color palette
var (
	// Core colors
	colorBg     = lipgloss.Color("#2d353b")
	colorFg     = lipgloss.Color("#d3c6aa")
	colorSubtle = lipgloss.Color("#859289")
	colorAccent = lipgloss.Color("#a7c080") // green
	colorTeal   = lipgloss.Color("#83c092") // teal
	colorBlue   = lipgloss.Color("#7fbbb3") // blue
	colorOrange = lipgloss.Color("#e69875") // orange
	colorYellow = lipgloss.Color("#dbbc7f") // yellow
	colorRed    = lipgloss.Color("#e67e80") // red
	colorBorder = lipgloss.Color("#475258")
)

// Styles
var (
	headerStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true).
			MarginBottom(1)

	titleStyle = lipgloss.NewStyle().
			Foreground(colorTeal).
			Bold(true).
			MarginRight(2)

	subtleStyle = lipgloss.NewStyle().
			Foreground(colorSubtle)

	accentStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2).
			MarginRight(2).
			MarginBottom(1)

	activeTabStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true).
			Padding(0, 2).
			Background(lipgloss.Color("#3d484d"))

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(colorSubtle).
				Padding(0, 2)

	statusOkStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	statusErrorStyle = lipgloss.NewStyle().
				Foreground(colorRed)

	footerStyle = lipgloss.NewStyle().
			Foreground(colorSubtle).
			MarginTop(1)
)

type tabView int

const (
	dashboardTab tabView = iota
	serversTab
	adaptersTab
)

type model struct {
	width     int
	height    int
	activeTab tabView
	servers   []schema.Server
	adapters  []adapters.Adapter
	err       error
	ready     bool
}

type initDataMsg struct {
	servers  []schema.Server
	adapters []adapters.Adapter
}

func initialModel() model {
	return model{
		activeTab: dashboardTab,
		ready:     false,
	}
}

func (m model) Init() tea.Cmd {
	return loadData
}

func loadData() tea.Msg {
	// Load servers from registry
	ctx := context.Background()
	regClient, err := registry.New()
	if err != nil {
		return initDataMsg{servers: nil, adapters: nil}
	}

	servers, err := regClient.ListServers(ctx)
	if err != nil {
		servers = []schema.Server{}
	}

	// Get all adapters
	allAdapters := adapters.All()

	return initDataMsg{
		servers:  servers,
		adapters: allAdapters,
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("q", "ctrl+c"))):
			return m, tea.Quit
		case key.Matches(msg, key.NewBinding(key.WithKeys("tab"))):
			m.activeTab = (m.activeTab + 1) % 3
		case key.Matches(msg, key.NewBinding(key.WithKeys("1"))):
			m.activeTab = dashboardTab
		case key.Matches(msg, key.NewBinding(key.WithKeys("2"))):
			m.activeTab = serversTab
		case key.Matches(msg, key.NewBinding(key.WithKeys("3"))):
			m.activeTab = adaptersTab
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case initDataMsg:
		m.servers = msg.servers
		m.adapters = msg.adapters
		m.ready = true
	}

	return m, nil
}

func (m model) View() string {
	if !m.ready {
		return subtleStyle.Render("Loading mcpfleet...")
	}

	var b strings.Builder

	// Header with logo
	logo := `
 ┏┳┓┏━╸┏━┓┏━╸╻  ┏━╸┏━╸╻
 ┃┃┃┃  ┣━┛┣╸ ┃  ┣╸ ┣╸ ┃
 ╹ ╹┗━╸╹  ╹  ┗━╸╹  ╹  ╹
`
	b.WriteString(headerStyle.Render(logo))
	b.WriteString("\n")
	b.WriteString(subtleStyle.Render("Vendor-agnostic MCP server manager"))
	b.WriteString("\n\n")

	// Tabs
	tabs := []string{"Dashboard", "Servers", "Adapters"}
	var renderedTabs []string
	for i, tab := range tabs {
		if tabView(i) == m.activeTab {
			renderedTabs = append(renderedTabs, activeTabStyle.Render(tab))
		} else {
			renderedTabs = append(renderedTabs, inactiveTabStyle.Render(tab))
		}
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, renderedTabs...))
	b.WriteString("\n\n")

	// Content based on active tab
	switch m.activeTab {
	case dashboardTab:
		b.WriteString(m.renderDashboard())
	case serversTab:
		b.WriteString(m.renderServers())
	case adaptersTab:
		b.WriteString(m.renderAdapters())
	}

	// Footer with keybindings
	b.WriteString("\n\n")
	keybindings := footerStyle.Render(
		"[tab] switch tabs • [1-3] quick switch • [q] quit",
	)
	b.WriteString(keybindings)

	return b.String()
}

func (m model) renderDashboard() string {
	var b strings.Builder

	// Overview stats
	statsCard := cardStyle.Render(
		titleStyle.Render(fmt.Sprintf("📊 Overview\n\n")) +
			fmt.Sprintf("%s %s\n", accentStyle.Render("Servers:"), fmt.Sprintf("%d registered", len(m.servers))) +
			fmt.Sprintf("%s %s\n", accentStyle.Render("Adapters:"), fmt.Sprintf("%d supported", len(m.adapters))),
	)

	// Quick actions
	actionsCard := cardStyle.Render(
		titleStyle.Render("⚡ Quick Actions\n\n") +
			accentStyle.Render("• mcpfleet list") + subtleStyle.Render(" - List all servers\n") +
			accentStyle.Render("• mcpfleet apply --all cursor") + subtleStyle.Render(" - Apply to Cursor\n") +
			accentStyle.Render("• mcpfleet auth login") + subtleStyle.Render(" - Login to registry"),
	)

	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, statsCard, actionsCard))
	b.WriteString("\n\n")

	// Status
	status := statusOkStyle.Render("✓ Ready")
	if len(m.servers) == 0 {
		status = statusErrorStyle.Render("⚠ No servers registered")
	}
	b.WriteString(titleStyle.Render("Status: ") + status)

	return b.String()
}

func (m model) renderServers() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render(fmt.Sprintf("MCP Servers (%d)", len(m.servers))))
	b.WriteString("\n\n")

	if len(m.servers) == 0 {
		b.WriteString(subtleStyle.Render("No servers registered yet.\n"))
		b.WriteString(subtleStyle.Render("Run ") + accentStyle.Render("mcpfleet pull") + subtleStyle.Render(" to fetch from registry."))
		return b.String()
	}

	for i, server := range m.servers {
		if i > 0 {
			b.WriteString("\n")
		}

		serverCard := cardStyle.Width(m.width - 8).Render(
			titleStyle.Render(server.Name) + "\n" +
				subtleStyle.Render(server.Description) + "\n\n" +
				fmt.Sprintf("%s %s\n", accentStyle.Render("Transport:"), server.Transport) +
				fmt.Sprintf("%s %s", accentStyle.Render("Command:"), server.Command),
		)
		b.WriteString(serverCard)
		b.WriteString("\n")
	}

	return b.String()
}

func (m model) renderAdapters() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render(fmt.Sprintf("Supported Adapters (%d)", len(m.adapters))))
	b.WriteString("\n\n")

	for i, adapter := range m.adapters {
		if i > 0 {
			b.WriteString("\n")
		}

		adapterCard := cardStyle.Width(m.width - 8).Render(
			titleStyle.Render(adapter.Name()) + "\n" +
				fmt.Sprintf("%s %s", accentStyle.Render("Config Path:"), adapter.ConfigPath()),
		)
		b.WriteString(adapterCard)
		b.WriteString("\n")
	}

	return b.String()
}

// Run starts the TUI
func Run() error {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
