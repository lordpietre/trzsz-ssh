package tssh

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

type hostChoiceMsg struct {
	alias   string
	quit    bool
	newHost bool
}

type hostModel struct {
	hosts              []*sshHost
	filtered           []*sshHost
	cursor             int
	filter             []byte
	search             bool
	showHelp           bool
	termMgr            terminalManager
	width              int
	height             int
	done               bool
	result             hostChoiceMsg
	titleStyle         lipgloss.Style
	bgStyle            lipgloss.Style
	helpStyle          lipgloss.Style
	labelStyle         lipgloss.Style
	actionStyle        lipgloss.Style
	activeStyle        lipgloss.Style
	inactiveStyle      lipgloss.Style
	activeSeleStyle    lipgloss.Style
	inactiveSeleStyle  lipgloss.Style
}

func newHostModel(keywords string, hosts []*sshHost, termMgr terminalManager) *hostModel {
	m := &hostModel{
		hosts:    hosts,
		filtered: hosts,
		termMgr:  termMgr,
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
	}
	return m, nil
}

func (m *hostModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.search {
		return m.handleSearch(msg)
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

func (m *hostModel) clampCursor() {
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
		if m.cursor < 0 {
			m.cursor = 0
		}
	}
}

func (m *hostModel) handleNormal(s string) (tea.Model, tea.Cmd) {
	switch s {
	case "up", "k", "shift+tab":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j", "tab":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	case "left", "pgup", "ctrl+h", "ctrl+u", "ctrl+b":
		m.pageMove(-1)
	case "right", "pgdown", "ctrl+l", "ctrl+d", "ctrl+f":
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

	// --- top border ---
	b.WriteString(m.bgLine("┌" + strings.Repeat("─", m.width-2) + "┐") + "\n")

	// --- title bar with blue background ---
	title := "  tssh — SSH Connection Manager  "
	p := m.width - 3 - runewidth.StringWidth(title)
	if p < 0 {
		p = 0
	}
	titleRow := m.titleStyle.Render(" " + title + strings.Repeat(" ", p) + " ")
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

	// --- separator ---
	b.WriteString(m.bgLine("├"+strings.Repeat("─", m.width-2)+"┤") + "\n")

	// --- details (limited to detailLines so we never overflow) ---
	if m.cursor >= 0 && m.cursor < len(m.filtered) {
		m.renderDetails(&b, m.filtered[m.cursor], detailLines)
	}

	// --- bottom border ---
	b.WriteString(m.bgLine("└"+strings.Repeat("─", m.width-2)+"┘") + "\n")

	// --- action buttons bar ---
	actionBar := "   [ New ]   [ Search ]   [ Select ]   [ Enter ]   [ Quit ]"
	if m.termMgr != nil {
		actionBar = "   [ New ]   [ Search ]   [ Select ]   [ Win ]   [ Tab ]   [ Pane ]   [ Quit ]"
	}
	actionsPadded := actionBar + strings.Repeat(" ", m.width-runewidth.StringWidth(actionBar)-1)
	b.WriteString(m.actionStyle.Render(actionsPadded) + "\n")

	// --- status line ---
	statusStr := fmt.Sprintf("  %s | %d/%d  ",
		userConfig.configPath, m.cursor+1, len(m.filtered))
	if len(m.filtered) == 0 {
		statusStr = "  No hosts found. Press [N] to add one.  "
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

		alias := model.result.alias
		if alias != "" {
			fmt.Fprintf(os.Stderr, "\033[0;32m➜ %s\033[0m\r\n", alias)
		}
		return alias, false, nil
	}
}
