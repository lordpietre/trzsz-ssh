package tssh

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

type hostChoiceMsg struct {
	alias      string
	quit       bool
	newHost    bool
	editConfig bool
}

type actionItem struct {
	label string
	exec  func() (tea.Model, tea.Cmd)
}

type contextItem struct {
	label string
	exec  func() (tea.Model, tea.Cmd)
}

type tunnelEntry struct {
	Alias      string `json:"alias"`
	LocalPort  string `json:"local_port"`
	RemotePort string `json:"remote_port"`
	Mode       string `json:"mode"` // "manual" or "auto"
	Active     bool   `json:"-"`
	Cmd        *exec.Cmd `json:"-"`
}

type tunnelState struct {
	active         bool
	view           string // "menu", "manual_form", "auto_scanning", "auto_results", "form_ask_local"
	alias          string
	manualRemote   string
	manualLocal    string
	formField      int    // 0=remote, 1=local
	scanPorts      []int
	scanCursor     int
	scanErr        string
	askLocalRemote int // remote port being tunneled when asking for local port
	tunnels        []*tunnelEntry
	tunnelCursor   int
	tunnelDelMode  bool
	tunnelConfigPath string
}

type hostModel struct {
	hosts              []*sshHost
	filtered           []*sshHost
	cursor             int
	actionCursor       int
	filter             []byte
	search             bool
	showHelp           bool
	showContextMenu    bool
	contextCursor      int
	termMgr            terminalManager
	width              int
	height             int
	done               bool
	result             hostChoiceMsg
	tunnel             tunnelState
	titleStyle         lipgloss.Style
	bgStyle            lipgloss.Style
	helpStyle          lipgloss.Style
	labelStyle         lipgloss.Style
	actionStyle        lipgloss.Style
	activeStyle        lipgloss.Style
	inactiveStyle      lipgloss.Style
	activeSeleStyle    lipgloss.Style
	inactiveSeleStyle  lipgloss.Style
	actionFocusStyle   lipgloss.Style
	contextMenuStyle   lipgloss.Style
	contextActStyle    lipgloss.Style
}

func (m *hostModel) getActions() []actionItem {
	actions := []actionItem{
		{"New", func() (tea.Model, tea.Cmd) {
			m.done = true
			m.result = hostChoiceMsg{newHost: true}
			return m, tea.Quit
		}},
		{"Search", func() (tea.Model, tea.Cmd) {
			m.search = true
			m.filter = []byte("/")
			m.actionCursor = -1
			return m, nil
		}},
		{"Select", func() (tea.Model, tea.Cmd) {
			if m.termMgr != nil && m.cursor >= 0 && m.cursor < len(m.filtered) {
				m.filtered[m.cursor].Selected = !m.filtered[m.cursor].Selected
			}
			return m, nil
		}},
	}
	if m.termMgr != nil {
		actions = append(actions,
			actionItem{"Win", func() (tea.Model, tea.Cmd) {
				return m.confirmBatch(openTermWindow)
			}},
			actionItem{"Tab", func() (tea.Model, tea.Cmd) {
				return m.confirmBatch(openTermTab)
			}},
			actionItem{"Pane", func() (tea.Model, tea.Cmd) {
				return m.confirmBatch(openTermPane)
			}},
		)
	} else {
		actions = append(actions, actionItem{"Enter", func() (tea.Model, tea.Cmd) {
			if len(m.filtered) > 0 {
				return m.confirm(m.cursor)
			}
			return m, nil
		}})
	}
	actions = append(actions, actionItem{"Quit", func() (tea.Model, tea.Cmd) {
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	}})
	return actions
}

func (m *hostModel) getContextItems() []contextItem {
	alias := ""
	if m.cursor >= 0 && m.cursor < len(m.filtered) {
		alias = m.filtered[m.cursor].Alias
	}
	return []contextItem{
		{"Edit", func() (tea.Model, tea.Cmd) {
			m.showContextMenu = false
			m.done = true
			m.result = hostChoiceMsg{editConfig: true}
			return m, tea.Quit
		}},
		{"Tunnels", func() (tea.Model, tea.Cmd) {
			m.showContextMenu = false
			m.tunnel.active = true
			m.tunnel.view = "menu"
			m.tunnel.alias = alias
			m.tunnel.tunnels = tunnelLoadConfig(m.tunnel.tunnelConfigPath, alias)
			return m, nil
		}},
		{"Delete", func() (tea.Model, tea.Cmd) {
			m.showContextMenu = false
			if alias != "" {
				deleteHost(alias)
				m.hosts = getAllHosts()
				m.applyFilter()
				m.clampCursor()
			}
			return m, nil
		}},
	}
}

func newHostModel(keywords string, hosts []*sshHost, termMgr terminalManager) *hostModel {
	m := &hostModel{
		hosts:        hosts,
		filtered:     hosts,
		termMgr:      termMgr,
		actionCursor: -1,
		tunnel: tunnelState{
			tunnelConfigPath: tunnelDefaultPath(),
		},
	}
	m.initStyles()
	if keywords != "" {
		m.search = true
		m.filter = []byte(keywords)
		m.applyFilter()
	} else if strings.ToLower(userConfig.promptDefaultMode) == "search" {
		m.search = true
		m.filter = []byte("/")
	}
	return m
}

func (m *hostModel) initStyles() {
	ncursesBg := lipgloss.Color("15")  // white
	ncursesFg := lipgloss.Color("0")   // black
	ncursesBlue := lipgloss.Color("4") // dark blue

	m.bgStyle = lipgloss.NewStyle().
		Background(ncursesBg).
		Foreground(ncursesFg)
	m.titleStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("4")).
		Foreground(lipgloss.Color("15")).
		Bold(true)
	m.helpStyle = lipgloss.NewStyle().
		Background(ncursesBg).
		Foreground(lipgloss.Color("8"))
	m.labelStyle = lipgloss.NewStyle().
		Background(ncursesBg).
		Foreground(ncursesBlue)
	m.actionStyle = lipgloss.NewStyle().
		Background(ncursesBg).
		Foreground(ncursesFg).
		Bold(true)
	m.actionFocusStyle = lipgloss.NewStyle().
		Background(ncursesBlue).
		Foreground(lipgloss.Color("15")).
		Bold(true)
	m.activeStyle = lipgloss.NewStyle().
		Background(ncursesBlue).
		Foreground(lipgloss.Color("15")).
		Bold(true)
	m.inactiveStyle = lipgloss.NewStyle().
		Background(ncursesBg).
		Foreground(ncursesFg)
	m.activeSeleStyle = lipgloss.NewStyle().
		Background(ncursesBlue).
		Foreground(lipgloss.Color("10")).
		Bold(true)
	m.inactiveSeleStyle = lipgloss.NewStyle().
		Background(ncursesBg).
		Foreground(lipgloss.Color("10")).
		Bold(true)
	m.contextMenuStyle = lipgloss.NewStyle().
		Background(ncursesBg).
		Foreground(ncursesFg)
	m.contextActStyle = lipgloss.NewStyle().
		Background(ncursesBlue).
		Foreground(lipgloss.Color("15")).
		Bold(true)
}

func (m *hostModel) applyFilter() {
	var raw string
	if len(m.filter) > 0 {
		raw = string(m.filter)
	}
	if raw == "" || raw == "/" {
		m.filtered = m.hosts
		return
	}
	trimmed := strings.TrimPrefix(raw, "/")
	keywords := strings.Fields(strings.ToLower(trimmed))
	if len(keywords) == 0 {
		m.filtered = m.hosts
		return
	}
	var filtered []*sshHost
	for _, h := range m.hosts {
		if matchHost(h, keywords) {
			filtered = append(filtered, h)
		}
	}
	m.filtered = filtered
}

func (m *hostModel) Init() tea.Cmd {
	return nil
}

func (m *hostModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tunnelScanResultMsg:
		if msg.err != nil {
			m.tunnel.scanErr = msg.err.Error()
		} else {
			m.tunnel.scanPorts = msg.ports
		}
		m.tunnel.view = "auto_results"
		return m, nil
	case tunnelStartMsg:
		if msg.err != nil {
			m.tunnel.scanErr = msg.err.Error()
			m.tunnel.view = "menu"
		} else {
			m.tunnel.tunnels = append(m.tunnel.tunnels, msg.entry)
			m.tunnel.view = "menu"
			tunnelSaveConfig(m.tunnel.tunnels, m.tunnel.tunnelConfigPath)
		}
		return m, nil
	case tunnelStopMsg:
		if msg.err == nil {
			var keep []*tunnelEntry
			for _, t := range m.tunnel.tunnels {
				if t != msg.entry {
					keep = append(keep, t)
				}
			}
			m.tunnel.tunnels = keep
			m.tunnel.formField = 0
			tunnelSaveConfig(m.tunnel.tunnels, m.tunnel.tunnelConfigPath)
		}
		return m, nil
	}
	return m, nil
}

func (m *hostModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.search {
		return m.handleSearch(msg)
	}
	if m.tunnel.active {
		return m.handleTunnel(msg)
	}
	return m.handleNormal(msg.String())
}

func (m *hostModel) handleSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	switch s {
	case "enter":
		if len(m.filtered) > 0 {
			m.search = false
			return m.confirm(m.cursor)
		}
		return m, nil
	case "esc":
		m.search = false
		m.filter = nil
		m.applyFilter()
		m.clampCursor()
		return m, nil
	case "backspace":
		if len(m.filter) > 1 {
			m.filter = m.filter[:len(m.filter)-1]
			m.applyFilter()
			m.clampCursor()
		} else {
			m.search = false
			m.filter = nil
			m.applyFilter()
		}
		return m, nil
	case "up", "shift+tab":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "down", "tab":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
		return m, nil
	case "left", "pgup":
		m.pageMove(-1)
		return m, nil
	case "right", "pgdown":
		m.pageMove(1)
		return m, nil
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	default:
		txt := msg.Key().Text
		if txt == "" {
			return m, nil
		}
		m.filter = append(m.filter, txt...)
		m.applyFilter()
		m.clampCursor()
		return m, nil
	}
}

func (m *hostModel) handleTunnel(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.tunnel.view {
	case "menu":
		return m.handleTunnelMenu(msg)
	case "manual_form":
		return m.handleTunnelManualForm(msg)
	case "auto_scanning":
		return m, nil // ignore keys while scanning
	case "auto_results":
		return m.handleTunnelAutoResults(msg)
	case "form_ask_local":
		return m.handleTunnelAskLocal(msg)
	}
	return m, nil
}

func (m *hostModel) handleTunnelMenu(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	switch s {
	case "up", "k":
		if m.tunnel.tunnelDelMode {
			if m.tunnel.tunnelCursor > 0 {
				m.tunnel.tunnelCursor--
			}
		} else {
			if m.tunnel.formField > 0 {
				m.tunnel.formField--
			}
		}
	case "down", "j":
		if m.tunnel.tunnelDelMode {
			if m.tunnel.tunnelCursor < len(m.tunnel.tunnels)-1 {
				m.tunnel.tunnelCursor++
			}
		} else {
			if m.tunnel.formField < 1 {
				m.tunnel.formField++
			}
		}
	case "enter":
		if m.tunnel.tunnelDelMode {
			if m.tunnel.tunnelCursor >= 0 && m.tunnel.tunnelCursor < len(m.tunnel.tunnels) {
				entry := m.tunnel.tunnels[m.tunnel.tunnelCursor]
				m.tunnel.tunnelDelMode = false
				return m, tunnelStopProcess(entry)
			}
			m.tunnel.tunnelDelMode = false
			return m, nil
		}
		switch m.tunnel.formField {
		case 0:
			m.tunnel.view = "manual_form"
			m.tunnel.formField = 0
			m.tunnel.manualRemote = ""
			m.tunnel.manualLocal = ""
		case 1:
			m.tunnel.view = "auto_scanning"
			m.tunnel.scanPorts = nil
			m.tunnel.scanErr = ""
			return m, tunnelScanRemote(m.tunnel.alias)
		}
	case "d":
		if len(m.tunnel.tunnels) > 0 {
			m.tunnel.tunnelDelMode = !m.tunnel.tunnelDelMode
			if m.tunnel.tunnelDelMode {
				m.tunnel.tunnelCursor = 0
			}
		}
	case "esc", "q":
		if m.tunnel.tunnelDelMode {
			m.tunnel.tunnelDelMode = false
		} else {
			m.tunnel.active = false
		}
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	}
	return m, nil
}

func (m *hostModel) handleTunnelManualForm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	txt := msg.Key().Text
	switch s {
	case "tab", "down", "j":
		m.tunnel.formField = (m.tunnel.formField + 1) % 2
		return m, nil
	case "shift+tab", "up", "k":
		m.tunnel.formField = (m.tunnel.formField + 1) % 2
		return m, nil
	case "enter":
		if m.tunnel.manualRemote == "" || m.tunnel.manualLocal == "" {
			return m, nil
		}
		_, retCmd := tunnelStartProcess(m.tunnel.alias, m.tunnel.manualLocal, m.tunnel.manualRemote, "manual")
		return m, retCmd
	case "esc":
		m.tunnel.view = "menu"
		return m, nil
	case "backspace":
		if m.tunnel.formField == 0 && len(m.tunnel.manualRemote) > 0 {
			m.tunnel.manualRemote = m.tunnel.manualRemote[:len(m.tunnel.manualRemote)-1]
		} else if m.tunnel.formField == 1 && len(m.tunnel.manualLocal) > 0 {
			m.tunnel.manualLocal = m.tunnel.manualLocal[:len(m.tunnel.manualLocal)-1]
		}
		return m, nil
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	default:
		if txt == "" {
			return m, nil
		}
		for _, r := range txt {
			if r >= '0' && r <= '9' {
				if m.tunnel.formField == 0 {
					m.tunnel.manualRemote += string(r)
				} else {
					m.tunnel.manualLocal += string(r)
				}
			}
		}
		return m, nil
	}
}

func (m *hostModel) handleTunnelAutoResults(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	switch s {
	case "up", "k":
		if m.tunnel.scanCursor > 0 {
			m.tunnel.scanCursor--
		}
	case "down", "j":
		if m.tunnel.scanCursor < len(m.tunnel.scanPorts)-1 {
			m.tunnel.scanCursor++
		}
	case "enter":
		if len(m.tunnel.scanPorts) > 0 {
			m.tunnel.askLocalRemote = m.tunnel.scanPorts[m.tunnel.scanCursor]
			m.tunnel.view = "form_ask_local"
			m.tunnel.formField = 0
			m.tunnel.manualLocal = ""
		}
	case "esc":
		m.tunnel.view = "menu"
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	}
	return m, nil
}

func (m *hostModel) handleTunnelAskLocal(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	txt := msg.Key().Text
	switch s {
	case "enter":
		if m.tunnel.manualLocal == "" {
			return m, nil
		}
		_, retCmd := tunnelStartProcess(m.tunnel.alias, m.tunnel.manualLocal, fmt.Sprintf("%d", m.tunnel.askLocalRemote), "auto")
		return m, retCmd
	case "esc":
		m.tunnel.view = "auto_results"
		return m, nil
	case "backspace":
		if len(m.tunnel.manualLocal) > 0 {
			m.tunnel.manualLocal = m.tunnel.manualLocal[:len(m.tunnel.manualLocal)-1]
		}
		return m, nil
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	default:
		if txt == "" {
			return m, nil
		}
		for _, r := range txt {
			if r >= '0' && r <= '9' {
				m.tunnel.manualLocal += string(r)
			}
		}
		return m, nil
	}
}

func (m *hostModel) clampCursor() {
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
		if m.cursor < 0 {
			m.cursor = 0
		}
	}
}

func (m *hostModel) handleNormal(s string) (tea.Model, tea.Cmd) {
	actions := m.getActions()

	// Context menu mode
	if m.showContextMenu {
		ctxItems := m.getContextItems()
		switch s {
		case "up", "k":
			if m.contextCursor > 0 {
				m.contextCursor--
			}
			return m, nil
		case "down", "j":
			if m.contextCursor < len(ctxItems)-1 {
				m.contextCursor++
			}
			return m, nil
		case "enter":
			return ctxItems[m.contextCursor].exec()
		case "esc", "left":
			m.showContextMenu = false
			return m, nil
		case "ctrl+c", "ctrl+q":
			m.done = true
			m.result = hostChoiceMsg{quit: true}
			return m, tea.Quit
		default:
			m.showContextMenu = false
		}
	}

	// Action bar mode
	if m.actionCursor >= 0 {
		switch s {
		case "left":
			if m.actionCursor > 0 {
				m.actionCursor--
			}
			return m, nil
		case "right":
			if m.actionCursor < len(actions)-1 {
				m.actionCursor++
			}
			return m, nil
		case "up", "k", "shift+tab":
			m.actionCursor = -1
			m.cursor = len(m.filtered) - 1
			if m.cursor < 0 {
				m.cursor = 0
			}
			return m, nil
		case "down", "j", "tab":
			m.actionCursor = -1
			return m, nil
		case "enter":
			return actions[m.actionCursor].exec()
		case "ctrl+c", "ctrl+q":
			m.done = true
			m.result = hostChoiceMsg{quit: true}
			return m, tea.Quit
		default:
			m.actionCursor = -1
		}
	}

	// Host list mode
	switch s {
	case "up", "k", "shift+tab":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j", "tab":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		} else {
			m.actionCursor = 0
			return m, nil
		}
	case "left":
		m.showContextMenu = true
		m.contextCursor = 0
		return m, nil
	case "right":
		m.showContextMenu = true
		m.contextCursor = 0
		return m, nil
	case "pgup", "ctrl+h", "ctrl+u", "ctrl+b":
		m.pageMove(-1)
	case "pgdown", "ctrl+l", "ctrl+d", "ctrl+f":
		m.pageMove(1)
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = len(m.filtered) - 1
		if m.cursor < 0 {
			m.cursor = 0
		}
	case "/":
		m.search = true
		m.filter = []byte("/")
		return m, nil
	case "?":
		m.showHelp = !m.showHelp
	case "enter":
		if len(m.filtered) > 0 {
			return m.confirm(m.cursor)
		}
	case "n", "N":
		m.done = true
		m.result = hostChoiceMsg{newHost: true}
		return m, tea.Quit
	case "ctrl+c", "ctrl+q", "q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	case "e", "ctrl+e":
		m.filter = nil
		m.applyFilter()
	case " ", "ctrl+x", "ctrl+space":
		if m.termMgr != nil && m.cursor >= 0 && m.cursor < len(m.filtered) {
			m.filtered[m.cursor].Selected = !m.filtered[m.cursor].Selected
		}
	case "a", "ctrl+a":
		if m.termMgr != nil {
			for _, h := range m.filtered {
				h.Selected = true
			}
		}
	case "o", "ctrl+o":
		if m.termMgr != nil {
			for _, h := range m.filtered {
				h.Selected = !h.Selected
			}
		}
	case "w", "ctrl+w":
		if m.termMgr != nil {
			return m.confirmBatch(openTermWindow)
		}
	case "t", "ctrl+t":
		if m.termMgr != nil {
			return m.confirmBatch(openTermTab)
		}
	case "p", "ctrl+p":
		if m.termMgr != nil {
			return m.confirmBatch(openTermPane)
		}
	}
	return m, nil
}

func (m *hostModel) pageMove(dir int) {
	pageSize := getPromptPageSize()
	if dir < 0 {
		if m.cursor >= pageSize {
			m.cursor -= pageSize
		} else {
			m.cursor = 0
		}
	} else {
		if m.cursor+pageSize < len(m.filtered) {
			m.cursor += pageSize
		} else {
			m.cursor = len(m.filtered) - 1
		}
	}
}

func (m *hostModel) hasSelected() bool {
	for _, h := range m.filtered {
		if h.Selected {
			return true
		}
	}
	return false
}

func (m *hostModel) getSelected() []*sshHost {
	var selected []*sshHost
	if m.hasSelected() {
		for _, h := range m.filtered {
			if h.Selected {
				selected = append(selected, h)
			}
		}
	}
	return selected
}

func (m *hostModel) confirm(idx int) (tea.Model, tea.Cmd) {
	if idx < 0 || len(m.filtered) == 0 {
		m.result = hostChoiceMsg{quit: true}
		m.done = true
		return m, tea.Quit
	}
	sel := m.getSelected()
	if len(sel) == 0 && idx >= 0 && idx < len(m.filtered) {
		sel = []*sshHost{m.filtered[idx]}
	}
	m.result = hostChoiceMsg{alias: sel[0].Alias}
	m.done = true
	if m.termMgr != nil && len(sel) > 1 {
		keywords := strings.TrimLeft(string(m.filter), "/")
		m.termMgr.openTerminals(keywords, 0, sel)
	}
	return m, tea.Quit
}

func (m *hostModel) confirmBatch(openType int) (tea.Model, tea.Cmd) {
	if m.termMgr == nil {
		return m, nil
	}
	sel := m.getSelected()
	if len(sel) == 0 && m.cursor >= 0 && m.cursor < len(m.filtered) {
		m.filtered[m.cursor].Selected = true
		sel = []*sshHost{m.filtered[m.cursor]}
	}
	m.result = hostChoiceMsg{alias: sel[0].Alias}
	m.done = true
	keywords := strings.TrimLeft(string(m.filter), "/")
	m.termMgr.openTerminals(keywords, openType, sel)
	return m, tea.Quit
}

func (m *hostModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	if m.width == 0 || m.height == 0 {
		return tea.NewView("Initializing...")
	}

	var b strings.Builder

	// Guard against tiny terminals
	if m.width < 10 {
		m.width = 80
	}
	if m.height < 10 {
		m.height = 24
	}

	// --- top border ---
	b.WriteString(m.bgLine("┌" + strings.Repeat("─", m.width-2) + "┐") + "\n")

	// --- title bar with blue background ---
	title := "  tssh — SSH Connection Manager  "
	titleRow := m.titleStyle.Render(" " + title + repeatSafe(m.width-3-runewidth.StringWidth(title)) + " ")
	b.WriteString(titleRow + "\n")

	// --- separator ---
	b.WriteString(m.bgLine("├"+strings.Repeat("─", m.width-2)+"┤") + "\n")

	// --- filter / host count bar ---
	filterText := string(m.filter)
	if m.search {
		b.WriteString(m.bgLine("  " + filterText) + "\n")
	} else {
		info := fmt.Sprintf("  %d hosts", len(m.filtered))
		if len(m.filtered) != len(m.hosts) {
			info += fmt.Sprintf(" (%d total)", len(m.hosts))
		}
		b.WriteString(m.bgLine(info) + "\n")
	}

	// --- separator ---
	b.WriteString(m.bgLine("├"+strings.Repeat("─", m.width-2)+"┤") + "\n")

	// Pre-count detail lines so the list + details + chrome always fit in the terminal.
	// Fixed chrome: top_border(1)+title(1)+sep(1)+filter(1)+sep(1)+sep(1)+bottom_border(1)+action(1)+status(1) = 9
	detailLines := m.countDetailLines()
	availableHeight := m.height - 9 - detailLines
	if availableHeight < 3 {
		availableHeight = 3
	}

	scrollOffset := 0
	if m.cursor >= availableHeight {
		scrollOffset = m.cursor - availableHeight + 1
	}

	if m.tunnel.active {
		m.renderTunnelView(&b, availableHeight+detailLines+2)
	} else if m.showHelp {
		// --- help overlay instead of host list ---
		m.renderHelp(&b, availableHeight)
	} else {
		// --- host list ---
		for i := 0; i < availableHeight; i++ {
			idx := scrollOffset + i
			if idx < len(m.filtered) {
				h := m.filtered[idx]
				isActive := idx == m.cursor
				m.renderHost(&b, h, isActive)
			} else {
				b.WriteString(m.bgLine("") + "\n")
			}
		}
	}

	if !m.tunnel.active {
		// --- separator ---
		b.WriteString(m.bgLine("├"+strings.Repeat("─", m.width-2)+"┤") + "\n")

		// --- details or context menu (limited to detailLines so we never overflow) ---
		if !m.showHelp && m.cursor >= 0 && m.cursor < len(m.filtered) {
			if m.showContextMenu {
				m.renderContextMenu(&b, detailLines)
			} else {
				m.renderDetails(&b, m.filtered[m.cursor], detailLines)
			}
		}

		// --- bottom border ---
		b.WriteString(m.bgLine("└"+strings.Repeat("─", m.width-2)+"┘") + "\n")

		// --- action buttons bar ---
		m.renderActions(&b)
	}

	// --- status line ---
	var statusStr string
	if m.tunnel.active {
		statusStr = "  Tunnels for " + m.tunnel.alias + "  "
	} else if len(m.filtered) == 0 {
		statusStr = "  No hosts found. Press [N] to add one.  "
	} else {
		statusStr = fmt.Sprintf("  %s | %d/%d  ",
			userConfig.configPath, m.cursor+1, len(m.filtered))
	}
	b.WriteString(m.bgLine(m.helpStyle.Render(clipString(statusStr, m.width-1))) + "\n")

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m *hostModel) bgLine(s string) string {
	// Use lipgloss.Width which strips ANSI escape codes before measuring.
	w := lipgloss.Width(s)
	if w < m.width {
		s += strings.Repeat(" ", m.width-w)
	}
	return m.bgStyle.Render(s)
}

func (m *hostModel) renderHost(b *strings.Builder, h *sshHost, isActive bool) {
	pad := "  "
	if isActive {
		pad = "▐█ " // ncurses-style solid cursor
	}
	selIcon := "  "
	selStyle := m.inactiveSeleStyle
	if h.Selected {
		selIcon = "▐✓ " // checkmark with border
		if isActive {
			selStyle = m.activeSeleStyle
		}
	}

	var style lipgloss.Style
	if isActive {
		style = m.activeStyle
	} else {
		style = m.inactiveStyle
	}

	line := fmt.Sprintf("%s%s%s", pad, selStyle.Render(selIcon), style.Render(" "+h.Alias+" "))
	if h.Host != "" && h.Host != h.Alias {
		line += m.helpStyle.Render(fmt.Sprintf("  (%s)", h.Host))
	}
	if h.GroupLabels != "" {
		line += m.labelStyle.Render(fmt.Sprintf("  [%s]", h.GroupLabels))
	}
	// ansi.Truncate is ANSI-aware: it measures visible width only, preserving colour codes.
	if lipgloss.Width(line) > m.width-1 {
		line = ansi.Truncate(line, m.width-1, "")
	}
	b.WriteString(m.bgLine(line) + "\n")
}

// countDetailLines returns how many non-empty detail lines the current host would render.
func (m *hostModel) countDetailLines() int {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return 0
	}
	h := m.filtered[m.cursor]
	count := 0
	for _, item := range getPromptDetailItems() {
		var value string
		switch strings.ToLower(item) {
		case "alias":
			value = h.Alias
		case "host":
			value = h.Host
		case "port":
			if h.Port != "" && h.Port != "22" {
				value = h.Port
			}
		case "user":
			value = h.User
		case "grouplabels":
			value = h.GroupLabels
		case "identityfile":
			value = h.IdentityFile
		case "proxycommand":
			value = h.ProxyCommand
		case "proxyjump":
			value = h.ProxyJump
		case "remotecommand":
			value = h.RemoteCommand
		default:
			value = getExConfig(h.Alias, item)
		}
		if value != "" {
			count++
		}
	}
	return count
}

func (m *hostModel) renderDetails(b *strings.Builder, h *sshHost, maxLines int) {
	items := getPromptDetailItems()
	width := m.width - 2
	if width < 20 {
		width = 20
	}

	written := 0
	for _, item := range items {
		if maxLines > 0 && written >= maxLines {
			break
		}
		var value string
		switch strings.ToLower(item) {
		case "alias":
			value = h.Alias
		case "host":
			value = h.Host
		case "port":
			if h.Port != "" && h.Port != "22" {
				value = h.Port
			}
		case "user":
			value = h.User
		case "grouplabels":
			value = h.GroupLabels
		case "identityfile":
			value = h.IdentityFile
		case "proxycommand":
			value = h.ProxyCommand
		case "proxyjump":
			value = h.ProxyJump
		case "remotecommand":
			value = h.RemoteCommand
		default:
			value = getExConfig(h.Alias, item)
		}
		if value != "" {
			line := fmt.Sprintf("  %s:  %s", item, value)
			line = clipString(line, width)
			b.WriteString(m.bgLine(m.helpStyle.Render(line)) + "\n")
			written++
		}
	}
}

func (m *hostModel) renderContextMenu(b *strings.Builder, maxLines int) {
	items := m.getContextItems()
	width := m.width - 2
	if width < 20 {
		width = 20
	}

	// title bar
	title := "  ── Context Menu ──"
	titleLine := clipString(title, width)
	b.WriteString(m.bgLine(m.activeStyle.Render(titleLine)) + "\n")

	// menu items
	for i, item := range items {
		if maxLines > 0 && i > maxLines-1 {
			break
		}
		icon := "  "
		if i == m.contextCursor {
			icon = "▐█ " // ncurses cursor
			label := icon + item.label + "  "
			b.WriteString(m.bgLine(m.activeStyle.Render(clipString(label, width))) + "\n")
		} else {
			label := icon + item.label + "  "
			b.WriteString(m.bgLine(m.inactiveStyle.Render(clipString(label, width))) + "\n")
		}
	}

	// fill remaining lines
	for i := len(items); i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func repeatSafe(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

func (m *hostModel) renderActions(b *strings.Builder) {
	actions := m.getActions()
	var bar strings.Builder
	bar.WriteString("  ")
	for i, a := range actions {
		if i > 0 {
			bar.WriteString("   ")
		}
		label := "[" + a.label + "]"
		if i == m.actionCursor {
			bar.WriteString(m.actionFocusStyle.Render(label))
		} else {
			bar.WriteString(m.actionStyle.Render(label))
		}
	}
	visible := ansi.StringWidth(bar.String())
	padded := bar.String() + repeatSafe(m.width-visible-1)
	b.WriteString(m.bgStyle.Render(padded) + "\n")
}

func (m *hostModel) renderHelp(b *strings.Builder, maxLines int) {
	helpLines := []string{
		"  ↑/↓  Navigate list               ←/→  Navigate actions",
		"  j/k  Navigate (vim)              g/G  First/Last",
		"  /    Search filter                Esc  Clear search",
		"  Enter  Select host                n    Add new host",
		"  Space  Toggle select              ?    Toggle help",
		"  q/Ctrl+C  Quit",
	}
	if m.termMgr != nil {
		helpLines = append(helpLines,
			"  w/Ctrl+W  Open in window         t/Ctrl+T  Open in tab",
			"  p/Ctrl+P  Open in pane           a/Ctrl+A  Select all",
			"  o/Ctrl+O  Invert selection",
		)
	}
	for i := 0; i < maxLines; i++ {
		if i < len(helpLines) {
			b.WriteString(m.bgLine(m.helpStyle.Render(helpLines[i])) + "\n")
		} else {
			b.WriteString(m.bgLine("") + "\n")
		}
	}
}

func (m *hostModel) renderTunnelView(b *strings.Builder, maxLines int) {
	switch m.tunnel.view {
	case "menu":
		m.renderTunnelMenu(b, maxLines)
	case "manual_form":
		m.renderTunnelManualForm(b, maxLines)
	case "auto_scanning":
		m.renderTunnelScanning(b, maxLines)
	case "auto_results":
		m.renderTunnelAutoResults(b, maxLines)
	case "form_ask_local":
		m.renderTunnelAskLocal(b, maxLines)
	}
}

func (m *hostModel) renderTunnelMenu(b *strings.Builder, maxLines int) {
	// title
	title := fmt.Sprintf("  ── Tunnels for %s ──", m.tunnel.alias)
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")

	// existing tunnels
	if len(m.tunnel.tunnels) > 0 {
		b.WriteString(m.bgLine("  Saved tunnels:") + "\n")
		for i, t := range m.tunnel.tunnels {
			line := fmt.Sprintf("  · :%s → localhost:%s (%s)", t.LocalPort, t.RemotePort, t.Mode)
			if t.Active {
				line += " [ACTIVE]"
			}
			if m.tunnel.tunnelDelMode && i == m.tunnel.tunnelCursor {
				b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+line)) + "\n")
			} else {
				b.WriteString(m.bgLine(m.helpStyle.Render("   "+line)) + "\n")
			}
		}
		if m.tunnel.tunnelDelMode {
			b.WriteString(m.bgLine(m.helpStyle.Render("  [Delete mode] Select tunnel to remove, Enter confirm, Esc cancel")) + "\n")
		}
		b.WriteString(m.bgLine("") + "\n")
	}

	// options
	items := []string{"[Manual]  Configure remote and local port manually",
		"[Automatic]  Scan remote ports and select"}
	lineCount := 3 + len(m.tunnel.tunnels)
	if len(m.tunnel.tunnels) > 0 {
		lineCount += 2
	}
	if m.tunnel.tunnelDelMode {
		lineCount++
	}
	for i, item := range items {
		if m.tunnel.tunnelDelMode {
			b.WriteString(m.bgLine(m.inactiveStyle.Render("   "+item)) + "\n")
		} else if i == m.tunnel.formField {
			b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+item)) + "\n")
		} else {
			b.WriteString(m.bgLine(m.inactiveStyle.Render("   "+item)) + "\n")
		}
		lineCount++
	}
	b.WriteString(m.bgLine("") + "\n")
	lineCount++

	if m.tunnel.tunnelDelMode {
		b.WriteString(m.bgLine(m.helpStyle.Render("  ↑↓ Select tunnel  Enter delete  Esc cancel  q Quit")) + "\n")
	} else {
		b.WriteString(m.bgLine(m.helpStyle.Render("  ↑↓ Navigate  Enter Select  d Delete tunnel  Esc Back  q Quit")) + "\n")
	}
	lineCount++

	for i := lineCount; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func (m *hostModel) renderTunnelManualForm(b *strings.Builder, maxLines int) {
	title := fmt.Sprintf("  ── New Tunnel (Manual) for %s ──", m.tunnel.alias)
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")
	b.WriteString(m.bgLine("") + "\n")

	// remote port
	remoteLabel := "  Remote Port: "
	if m.tunnel.formField == 0 {
		b.WriteString(m.bgLine(m.activeStyle.Render(remoteLabel + m.tunnel.manualRemote + "▌")) + "\n")
	} else {
		b.WriteString(m.bgLine(remoteLabel + m.tunnel.manualRemote) + "\n")
	}

	// local port
	localLabel := "  Local Port:  "
	if m.tunnel.formField == 1 {
		b.WriteString(m.bgLine(m.activeStyle.Render(localLabel + m.tunnel.manualLocal + "▌")) + "\n")
	} else {
		b.WriteString(m.bgLine(localLabel + m.tunnel.manualLocal) + "\n")
	}

	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(m.helpStyle.Render("  Enter to create  Esc to cancel  Tab to switch field")) + "\n")

	for i := 5; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func (m *hostModel) renderTunnelScanning(b *strings.Builder, maxLines int) {
	title := fmt.Sprintf("  ── Scanning %s ──", m.tunnel.alias)
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")
	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine("  Scanning remote ports...") + "\n")
	b.WriteString(m.bgLine("  (this may take a moment)") + "\n")
	for i := 3; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func (m *hostModel) renderTunnelAutoResults(b *strings.Builder, maxLines int) {
	title := fmt.Sprintf("  ── Open Ports on %s ──", m.tunnel.alias)
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")

	if m.tunnel.scanErr != "" {
		b.WriteString(m.bgLine("  Error: "+m.tunnel.scanErr) + "\n")
		return
	}

	if len(m.tunnel.scanPorts) == 0 {
		b.WriteString(m.bgLine("  No open ports found.") + "\n")
	}

	for i, port := range m.tunnel.scanPorts {
		if i >= maxLines-3 {
			break
		}
		line := fmt.Sprintf("  Port %d", port)
		if i == m.tunnel.scanCursor {
			b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+line)) + "\n")
		} else {
			b.WriteString(m.bgLine(m.inactiveStyle.Render("   "+line)) + "\n")
		}
	}
	// fill
	for i := len(m.tunnel.scanPorts); i < maxLines-2; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
	b.WriteString(m.bgLine(m.helpStyle.Render("  ↑↓ Select  Enter confirm  Esc back")) + "\n")
}

func (m *hostModel) renderTunnelAskLocal(b *strings.Builder, maxLines int) {
	title := fmt.Sprintf("  ── Tunnel Port %d on %s ──", m.tunnel.askLocalRemote, m.tunnel.alias)
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")
	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(fmt.Sprintf("  Remote port: %d", m.tunnel.askLocalRemote)) + "\n")
	localLabel := "  Local port:  "
	if m.tunnel.formField == 0 {
		b.WriteString(m.bgLine(m.activeStyle.Render(localLabel + m.tunnel.manualLocal + "▌")) + "\n")
	} else {
		b.WriteString(m.bgLine(localLabel + m.tunnel.manualLocal) + "\n")
	}
	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(m.helpStyle.Render("  Enter to create  Esc to go back")) + "\n")
	for i := 5; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func openEditor(_ string) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}
	path := userConfig.configPath
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

func deleteHost(alias string) {
	path := userConfig.configPath
	data, err := os.ReadFile(path)
	if err != nil {
		warning("read config file failed: %v", err)
		return
	}
	lines := strings.Split(string(data), "\n")
	var result []string
	skip := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "host ") {
			fields := strings.Fields(trimmed)
			if len(fields) >= 2 && fields[1] == alias {
				skip = true
				continue
			}
		}
		if skip {
			if trimmed == "" || strings.HasPrefix(trimmed, "host ") {
				skip = false
				if trimmed != "" {
					result = append(result, line)
				}
				continue
			}
			continue
		}
		result = append(result, line)
	}
	if err := os.WriteFile(path, []byte(strings.Join(result, "\n")), 0600); err != nil {
		warning("write config file failed: %v", err)
	}
}

// --- Tunnel messages ---

type tunnelScanResultMsg struct {
	ports []int
	err   error
}

type tunnelStartMsg struct {
	entry *tunnelEntry
	err   error
}

type tunnelStopMsg struct {
	entry *tunnelEntry
	err   error
}

func tunnelDefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".tssh_tunnels")
}

func tunnelLoadConfig(path, alias string) []*tunnelEntry {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var all []*tunnelEntry
	if err := json.Unmarshal(data, &all); err != nil {
		return nil
	}
	var filtered []*tunnelEntry
	for _, t := range all {
		if t.Alias == alias {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func tunnelSaveConfig(tunnels []*tunnelEntry, path string) {
	if path == "" {
		return
	}
	// load existing
	var all []*tunnelEntry
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &all)
	}
	// remove old entries for the same alias
	var keep []*tunnelEntry
	for _, t := range all {
		found := false
		for _, nt := range tunnels {
			if t.Alias == nt.Alias && t.LocalPort == nt.LocalPort && t.RemotePort == nt.RemotePort {
				found = true
				break
			}
		}
		if !found {
			keep = append(keep, t)
		}
	}
	// add new entries
	keep = append(keep, tunnels...)
	data, err := json.MarshalIndent(keep, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0600)
}

func tunnelScanRemote(alias string) tea.Cmd {
	return func() tea.Msg {
		client, err := SshLogin(&SshArgs{
			Destination: alias,
			ConfigFile:  userConfig.configPath,
		})
		if err != nil {
			return tunnelScanResultMsg{err: fmt.Errorf("connect failed: %v", err)}
		}
		defer client.Close()

		session, err := client.NewSession()
		if err != nil {
			return tunnelScanResultMsg{err: fmt.Errorf("session failed: %v", err)}
		}
		defer session.Close()

		output, err := session.Output("ss -tlnp 2>/dev/null || netstat -tlnp 2>/dev/null || ss -tln 2>/dev/null")
		if err != nil {
			return tunnelScanResultMsg{err: fmt.Errorf("scan command failed: %v", err)}
		}

		// Parse output to find listening ports
		var ports []int
		seen := make(map[int]bool)
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			for _, f := range fields {
				if strings.Contains(f, ":") {
					parts := strings.Split(f, ":")
					portStr := parts[len(parts)-1]
					if strings.Contains(portStr, "-") {
						continue
					}
					port, err := strconv.Atoi(portStr)
					if err == nil && port > 0 && port < 65536 && !seen[port] {
						seen[port] = true
						ports = append(ports, port)
					}
				}
			}
		}
		sort.Ints(ports)
		return tunnelScanResultMsg{ports: ports, err: nil}
	}
}

func tunnelStartProcess(alias, localPort, remotePort, mode string) (tea.Model, tea.Cmd) {
	sshPath, _ := exec.LookPath("ssh")
	if sshPath == "" {
		return nil, func() tea.Msg {
			return tunnelStartMsg{err: fmt.Errorf("ssh not found in PATH")}
		}
	}
	entry := &tunnelEntry{
		Alias:      alias,
		LocalPort:  localPort,
		RemotePort: remotePort,
		Mode:       mode,
	}
	configPath := userConfig.configPath
	return nil, func() tea.Msg {
		cmd := exec.Command(sshPath,
			"-F", configPath,
			"-o", "BatchMode=yes",
			"-o", "ExitOnForwardFailure=yes",
			"-o", "ConnectTimeout=10",
			"-o", "ServerAliveInterval=30",
			"-o", "ServerAliveCountMax=3",
			"-NL", localPort+":localhost:"+remotePort,
			alias)
		// Start the process
		if err := cmd.Start(); err != nil {
			return tunnelStartMsg{entry: entry, err: fmt.Errorf("tunnel start failed: %v", err)}
		}
		entry.Active = true
		entry.Cmd = cmd
		return tunnelStartMsg{entry: entry, err: nil}
	}
}

func tunnelStopProcess(entry *tunnelEntry) tea.Cmd {
	return func() tea.Msg {
		if entry.Cmd != nil && entry.Cmd.Process != nil {
			if err := entry.Cmd.Process.Kill(); err != nil {
				return tunnelStopMsg{entry: entry, err: err}
			}
			_ = entry.Cmd.Wait()
		}
		entry.Active = false
		return tunnelStopMsg{entry: entry, err: nil}
	}
}

// chooseAlias opens the host selection TUI. It loops to handle "new host" flow.
func chooseAlias(keywords string) (string, bool, error) {
	for {
		hosts := getAllHosts()
		termMgr := getTerminalManager()

		model := newHostModel(keywords, hosts, termMgr)

		teaOpts, cancelReader := newTeaOptions(func(buf []byte) {
			if enableDebugLogging {
				if ch := stdinInputChan.Load(); ch != nil {
					*ch <- append([]byte(nil), buf...)
				}
			}
		})

		p := tea.NewProgram(model, teaOpts...)
		if _, err := p.Run(); err != nil {
			cancelReader()
			return "", false, fmt.Errorf("prompt choose alias failed: %v", err)
		}
		cancelReader()

		if model.result.quit {
			return "", true, nil
		}

		if model.result.newHost {
			args := &sshArgs{}
			if _, shouldQuit := execNewHost(args); shouldQuit {
				keywords = ""
				continue
			}
			fmt.Fprintf(os.Stderr, "\033[0;32m➜ %s\033[0m\r\n", args.Destination)
			return args.Destination, false, nil
		}

		if model.result.editConfig {
			openEditor("")
			keywords = ""
			continue
		}

		alias := model.result.alias
		if alias != "" {
			fmt.Fprintf(os.Stderr, "\033[0;32m➜ %s\033[0m\r\n", alias)
		}
		return alias, false, nil
	}
}
