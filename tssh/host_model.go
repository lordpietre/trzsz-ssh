package tssh

import (
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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
	download   *downloadReq
}

type downloadReq struct {
	alias        string
	remotePaths  string
	localPath    string
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

type portInfo struct {
	Port    int
	Process string // process name from ss/netstat (e.g. "sshd", "nginx")
}

type dockerContainer struct {
	Name  string
	Ports string
}

type tunnelState struct {
	active           bool
	view             string // "menu", "manual_form", "auto_scanning", "auto_results", "form_ask_local"
	alias            string
	manualRemote     string
	manualLocal      string
	formField        int    // 0=remote, 1=local
	scanPorts        []portInfo
	scanCursor       int
	scanErr          string
	askLocalRemote   int // remote port being tunneled when asking for local port
	tunnels          []*tunnelEntry
	tunnelCursor     int
	tunnelDelMode    bool
	tunnelConfigPath string
	dockerContainers []dockerContainer
}

type editField struct {
	label     string
	value     string
	configKey string // "HostName", "Port", "User", "Password"
	kind      string // "text" or "password"
}

type editState struct {
	active bool
	fields []editField
	cursor int
	hostIdx int
}

type groupPickerState struct {
	active bool
	cursor int
	mode   string   // "select" or "new"
	groups []string
	input  string
}

type groupsState struct {
	active       bool
	view         string // "menu", "new_name", "new_hosts", "edit_group", "edit_hosts"
	cursor       int
	input        string
	err          string
	editingGroup string
	groupHosts   []*sshHost
	groupSele    []bool
}

type newHostField struct {
	label string
	value string
	kind  string // "text" or "password"
}

type newHostState struct {
	active bool
	fields []newHostField
	cursor int
	err    string
}

type downloadField struct {
	label string
	value string
	kind  string // "text" or "path"
}

type downloadState struct {
	active bool
	fields []downloadField
	cursor int
	alias  string
	err    string
}

type configField struct {
	key   string // config key name (lowercase)
	label string
	value string
	kind  string // "text", "number", "bool", "enum"
}

type configState struct {
	active  bool
	fields  []configField
	cursor  int
	saved   bool
	err     string
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
	showSystem         bool
	contextCursor      int
	termMgr            terminalManager
	width              int
	height             int
	done               bool
	result             hostChoiceMsg
	tunnel             tunnelState
	edit               editState
	newHost            newHostState
	config             configState
	download           downloadState
	groups             groupsState
	groupPicker        groupPickerState
	deleteAsk          bool
	deleteIdx          int
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
			m.newHost.active = true
			m.newHost.cursor = 0
			m.newHost.err = ""
			m.newHost.fields = []newHostField{
				{"Alias", "", "text"},
				{"HostName/IP", "", "text"},
				{"Port", "22", "text"},
				{"User", "", "text"},
				{"Password", "", "password"},
				{"Group", "", "group"},
			}
			return m, nil
		}},
		{"Search", func() (tea.Model, tea.Cmd) {
			m.search = true
			m.filter = []byte("/")
			m.actionCursor = -1
			return m, nil
		}},
		{"Groups", func() (tea.Model, tea.Cmd) {
			m.groups.active = true
			m.groups.view = "menu"
			m.groups.cursor = 0
			m.groups.input = ""
			m.groups.err = ""
			m.groups.editingGroup = ""
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
	}
	actions = append(actions, actionItem{"Config", func() (tea.Model, tea.Cmd) {
		m.config.active = true
		m.config.cursor = 0
		m.config.saved = false
		m.config.err = ""
		m.config.fields = buildConfigFields()
		return m, nil
	}})
	actions = append(actions, actionItem{"Quit", func() (tea.Model, tea.Cmd) {
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	}})
	return actions
}

func (m *hostModel) getContextItems() []contextItem {
	alias := ""
	hostIdx := -1
	if m.cursor >= 0 && m.cursor < len(m.filtered) {
		alias = m.filtered[m.cursor].Alias
		hostIdx = m.cursor
	}
	return []contextItem{
		{"Edit", func() (tea.Model, tea.Cmd) {
			m.showContextMenu = false
			if alias == "" {
				return m, nil
			}
			m.edit.active = true
			m.edit.cursor = 0
			m.edit.hostIdx = hostIdx
			m.edit.fields = buildEditFields(alias)
			return m, nil
		}},
		{"Tunnels", func() (tea.Model, tea.Cmd) {
			m.showContextMenu = false
			m.tunnel.active = true
			m.tunnel.view = "menu"
			m.tunnel.alias = alias
			m.tunnel.tunnels = tunnelLoadConfig(m.tunnel.tunnelConfigPath, alias)
			return m, nil
		}},
		{"Group", func() (tea.Model, tea.Cmd) {
			m.showContextMenu = false
			if alias == "" {
				return m, nil
			}
			m.groups.active = true
			m.groups.view = "menu"
			m.groups.cursor = 0
			m.groups.input = ""
			m.groups.err = ""
			m.groups.editingGroup = ""
			return m, nil
		}},
		{"Download", func() (tea.Model, tea.Cmd) {
			m.showContextMenu = false
			if alias == "" {
				return m, nil
			}
			m.download.active = true
			m.download.cursor = 0
			m.download.alias = alias
			m.download.err = ""
			m.download.fields = []downloadField{
				{"Remote paths (space-separated)", "", "text"},
				{"Local download path", userConfig.defaultDownloadPath, "path"},
			}
			return m, nil
		}},
		{"Delete", func() (tea.Model, tea.Cmd) {
			m.showContextMenu = false
			if alias == "" {
				return m, nil
			}
			// Find real index in m.hosts (not m.filtered)
			m.deleteIdx = -1
			for i, h := range m.hosts {
				if h.Alias == alias {
					m.deleteIdx = i
					break
				}
			}
			m.deleteAsk = true
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
	m.loadMetaIntoHosts()
	m.initStyles()
	m.applyFilter()
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
	white := lipgloss.Color("15")
	black := lipgloss.Color("0")
	blue := lipgloss.Color("4")
	grey := lipgloss.Color("8")
	green := lipgloss.Color("10")

	m.bgStyle = lipgloss.NewStyle().
		Background(white).
		Foreground(black)
	m.titleStyle = lipgloss.NewStyle().
		Background(white).
		Foreground(blue).
		Bold(true)
	m.helpStyle = lipgloss.NewStyle().
		Background(white).
		Foreground(grey)
	m.labelStyle = lipgloss.NewStyle().
		Background(white).
		Foreground(blue)
	m.actionStyle = lipgloss.NewStyle().
		Background(white).
		Foreground(black).
		Bold(true)
	m.actionFocusStyle = lipgloss.NewStyle().
		Background(white).
		Foreground(blue).
		Bold(true).
		Underline(true)
	m.activeStyle = lipgloss.NewStyle().
		Background(white).
		Foreground(blue).
		Bold(true)
	m.inactiveStyle = lipgloss.NewStyle().
		Background(white).
		Foreground(black)
	m.activeSeleStyle = lipgloss.NewStyle().
		Background(white).
		Foreground(green).
		Bold(true)
	m.inactiveSeleStyle = lipgloss.NewStyle().
		Background(white).
		Foreground(green).
		Bold(true)
	m.contextMenuStyle = lipgloss.NewStyle().
		Background(white).
		Foreground(black)
	m.contextActStyle = lipgloss.NewStyle().
		Background(white).
		Foreground(blue).
		Bold(true).
		Underline(true)
}

func (m *hostModel) applyFilter() {
	var raw string
	if len(m.filter) > 0 {
		raw = string(m.filter)
	}
	if raw == "" || raw == "/" {
		if m.showSystem {
			m.filtered = m.hosts
		} else {
			m.filtered = nil
			for _, h := range m.hosts {
				if !h.System {
					m.filtered = append(m.filtered, h)
				}
			}
		}
		return
	}
	trimmed := strings.TrimPrefix(raw, "/")
	keywords := strings.Fields(strings.ToLower(trimmed))
	if len(keywords) == 0 {
		if m.showSystem {
			m.filtered = m.hosts
		} else {
			m.filtered = nil
			for _, h := range m.hosts {
				if !h.System {
					m.filtered = append(m.filtered, h)
				}
			}
		}
		return
	}
	var filtered []*sshHost
	for _, h := range m.hosts {
		if !m.showSystem && h.System {
			continue
		}
		if matchHost(h, keywords) {
			filtered = append(filtered, h)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		gi, gj := groupSortKey(filtered[i]), groupSortKey(filtered[j])
		if gi != gj {
			// "~" sorts after letters so ungrouped hosts appear last
			if gi == "~" {
				return false
			}
			if gj == "~" {
				return true
			}
			return gi < gj
		}
		return filtered[i].Alias < filtered[j].Alias
	})
	m.filtered = filtered
}

func groupSortKey(h *sshHost) string {
	if h.GroupLabels == "" {
		return "~"
	}
	first := strings.Fields(h.GroupLabels)[0]
	return strings.ToLower(first)
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
			m.tunnel.dockerContainers = msg.containers
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
	if m.newHost.active {
		return m.handleNewHost(msg)
	}
	if m.tunnel.active {
		return m.handleTunnel(msg)
	}
	if m.edit.active {
		return m.handleEdit(msg)
	}
	if m.config.active {
		return m.handleConfig(msg)
	}
	if m.download.active {
		return m.handleDownload(msg)
	}
	if m.groupPicker.active {
		return m.handleGroupPicker(msg)
	}
	if m.groups.active {
		return m.handleGroups(msg)
	}
	if m.deleteAsk {
		return m.handleDeleteConfirm(msg)
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
			m.tunnel.dockerContainers = nil
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
	totalItems := len(m.tunnel.scanPorts) + len(m.tunnel.dockerContainers)
	switch s {
	case "up", "k":
		if m.tunnel.scanCursor > 0 {
			m.tunnel.scanCursor--
		}
	case "down", "j":
		if m.tunnel.scanCursor < totalItems-1 {
			m.tunnel.scanCursor++
		}
	case "enter":
		if totalItems == 0 {
			break
		}
		if m.tunnel.scanCursor < len(m.tunnel.scanPorts) {
			m.tunnel.askLocalRemote = m.tunnel.scanPorts[m.tunnel.scanCursor].Port
		} else {
			dockerIdx := m.tunnel.scanCursor - len(m.tunnel.scanPorts)
			if dockerIdx < len(m.tunnel.dockerContainers) {
				ports := parseDockerHostPorts(m.tunnel.dockerContainers[dockerIdx].Ports)
				if len(ports) > 0 {
					m.tunnel.askLocalRemote = ports[0]
				} else {
					break
				}
			} else {
				break
			}
		}
		m.tunnel.view = "form_ask_local"
		m.tunnel.formField = 0
		m.tunnel.manualLocal = ""
	case "esc":
		m.tunnel.view = "menu"
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	}
	return m, nil
}

func parseDockerHostPorts(portsStr string) []int {
	var hostPorts []int
	for _, part := range strings.Split(portsStr, ",") {
		part = strings.TrimSpace(part)
		idx := strings.Index(part, "->")
		if idx < 0 {
			continue
		}
		hostPart := strings.TrimSpace(part[:idx])
		colonIdx := strings.LastIndex(hostPart, ":")
		if colonIdx >= 0 {
			portStr := hostPart[colonIdx+1:]
			if p, err := strconv.Atoi(portStr); err == nil {
				hostPorts = append(hostPorts, p)
			}
		}
	}
	return hostPorts
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
		if len(m.filtered) == 0 {
			m.actionCursor = 0
			return m, nil
		}
		m.showContextMenu = true
		m.contextCursor = 0
		return m, nil
	case "right":
		if len(m.filtered) == 0 {
			m.actionCursor = 0
			return m, nil
		}
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
	case "S":
		m.showSystem = !m.showSystem
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
	// Update last login timestamp
	for _, h := range sel {
		metaUpdateLastLogin(h.Alias)
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
	// Update last login timestamp
	for _, h := range sel {
		metaUpdateLastLogin(h.Alias)
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

	// --- title bar ---
	title := "  tssh — SSH Connection Manager  "
	titleRow := m.titleStyle.Render(" " + title + repeatSafe(m.width-2-runewidth.StringWidth(title)) + " ")
	b.WriteString(titleRow + "\n")

	// --- separator ---
	b.WriteString(m.bgLine("├"+strings.Repeat("─", m.width-2)+"┤") + "\n")

	// --- filter / host count bar ---
	filterText := string(m.filter)
	if m.search {
		b.WriteString(m.bgLine("  " + filterText) + "\n")
	} else {
		info := fmt.Sprintf("  %d hosts", len(m.filtered))
		sysCount := 0
		if !m.showSystem {
			for _, h := range m.hosts {
				if h.System {
					sysCount++
				}
			}
			if sysCount > 0 {
				info += fmt.Sprintf(" (hiding %d system, S to show)", sysCount)
			}
		}
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

	if m.edit.active {
		m.renderEditView(&b, availableHeight+detailLines+2)
	} else if m.newHost.active {
		m.renderNewHost(&b, availableHeight+detailLines+2)
	} else if m.config.active {
		m.renderConfigView(&b, availableHeight+detailLines+2)
	} else if m.download.active {
		m.renderDownloadView(&b, availableHeight+detailLines+2)
	} else if m.groups.active {
		m.renderGroupsView(&b, availableHeight+detailLines+2)
	} else if m.deleteAsk {
		m.renderDeleteAsk(&b, availableHeight+detailLines+2)
	} else if m.tunnel.active {
		m.renderTunnelView(&b, availableHeight+detailLines+2)
	} else if m.showHelp {
		// --- help overlay instead of host list ---
		m.renderHelp(&b, availableHeight)
	} else {
		// --- host list ---
		// Column widths
		aliasW := m.width / 3
		if aliasW < 15 {
			aliasW = 15
		}
		ipW := m.width / 3
		if ipW < 15 {
			ipW = 15
		}
		loginW := m.width - aliasW - ipW - 6
		if loginW < 10 {
			loginW = 10
		}
		// Reserve space for the header
		listHeight := availableHeight - 1
		if listHeight < 1 {
			listHeight = 1
		}

		// Build display positions: interleave group headers between hosts
		type dispPos struct {
			isHeader bool
			hostIdx  int
			label    string
		}
		var display []dispPos
		lastGroup := ""
		for hi, h := range m.filtered {
			g := groupSortKey(h)
			if g == "~" {
				g = ""
			}
			if g != "" && g != lastGroup {
				display = append(display, dispPos{isHeader: true, label: g})
			}
			lastGroup = g
			display = append(display, dispPos{hostIdx: hi})
		}

		// Find cursor's display row
		cursorDisplay := 0
		for di, d := range display {
			if !d.isHeader && d.hostIdx == m.cursor {
				cursorDisplay = di
				break
			}
		}

		// Compute scroll offset in display rows
		if cursorDisplay >= listHeight {
			scrollOffset = cursorDisplay - listHeight + 1
		} else {
			scrollOffset = 0
		}

		// Column header
		aliasHdr := "Host" + repeatSafe(aliasW-4)
		ipHdr := "HostName" + repeatSafe(ipW-8)
		loginHdr := "Last Login" + repeatSafe(loginW-10)
		header := m.labelStyle.Render("  " + aliasHdr + " " + ipHdr + " " + loginHdr)
		b.WriteString(m.bgLine(header) + "\n")

		for i := 0; i < listHeight; i++ {
			dIdx := scrollOffset + i
			if dIdx < len(display) {
				d := display[dIdx]
				if d.isHeader {
					headerLine := m.labelStyle.Render("  ── " + d.label + " ──")
					b.WriteString(m.bgLine(headerLine) + "\n")
				} else {
					h := m.filtered[d.hostIdx]
					isActive := d.hostIdx == m.cursor
					m.renderHost(&b, h, isActive)
				}
			} else {
				b.WriteString(m.bgLine("") + "\n")
			}
		}
	}

	if !m.tunnel.active && !m.edit.active && !m.newHost.active && !m.config.active && !m.download.active && !m.deleteAsk && !m.groups.active {
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
	if m.edit.active {
		statusStr = "  Editing " + m.edit.fields[0].value + "  "
	} else if m.newHost.active {
		statusStr = "  Adding new host  "
	} else if m.groups.active {
		statusStr = "  Groups  "
	} else if m.download.active {
		statusStr = "  Download from " + m.download.alias + "  "
	} else if m.config.active {
		if m.config.saved {
			statusStr = "  Config saved — restart to apply  "
		} else {
			statusStr = "  Config  "
		}
	} else if m.deleteAsk && m.deleteIdx >= 0 && m.deleteIdx < len(m.hosts) {
		statusStr = "  Delete " + m.hosts[m.deleteIdx].Alias + "?  "
	} else if m.tunnel.active {
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
	v.BackgroundColor = color.RGBA{255, 255, 255, 255}
	return v
}

func (m *hostModel) bgLine(s string) string {
	w := lipgloss.Width(s)
	if w < m.width {
		s += strings.Repeat(" ", m.width-w)
	}
	return s
}

func (m *hostModel) renderHost(b *strings.Builder, h *sshHost, isActive bool) {
	pad := "  "
	if isActive {
		pad = "▐█ "
	}
	selIcon := "  "
	selStyle := m.inactiveSeleStyle
	if h.Selected {
		selIcon = "▐✓ "
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

	// Column widths (proportional to terminal width)
	aliasW := m.width / 3
	if aliasW < 15 {
		aliasW = 15
	}
	ipW := m.width / 3
	if ipW < 15 {
		ipW = 15
	}
	loginW := m.width - aliasW - ipW - 8
	if loginW < 10 {
		loginW = 10
	}

	// Build alias column
	alias := h.Alias
	if ansi.StringWidth(alias) > aliasW-1 {
		alias = ansi.Truncate(alias, aliasW-1, "")
	}

	// Build IP column
	ip := h.Host
	if ip == "" {
		ip = h.Alias
	}
	if ansi.StringWidth(ip) > ipW-1 {
		ip = ansi.Truncate(ip, ipW-1, "")
	}

	// Build last login column
	login := h.LastLogin
	if login == "" {
		login = "—"
	}
	if ansi.StringWidth(login) > loginW-1 {
		login = ansi.Truncate(login, loginW-1, "")
	}

	// Groups badge
	groups := ""
	if h.GroupLabels != "" {
		groups = m.labelStyle.Render(" [" + h.GroupLabels + "]")
	}

	// Pad each column to its width
	aliasPad := repeatSafe(aliasW - ansi.StringWidth(alias))
	ipPad := repeatSafe(ipW - ansi.StringWidth(ip))

	line := fmt.Sprintf("%s%s%s%s%s%s",
		pad,
		selStyle.Render(selIcon),
		style.Render(" "+alias+aliasPad+" "),
		m.helpStyle.Render(ip+ipPad),
		m.helpStyle.Render(" "+login),
		groups)

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
	b.WriteString(m.bgLine(bar.String()) + "\n")
}

func (m *hostModel) renderHelp(b *strings.Builder, maxLines int) {
	helpLines := []string{
		"  ↑/↓  Navigate list               ←/→  Navigate actions",
		"  j/k  Navigate (vim)              g/G  First/Last",
		"  /    Search filter                Esc  Clear search",
		"  Enter  Select host                n    Add new host",
		"  Space  Toggle select              ?    Toggle help",
		"  S    Toggle system hosts          q/Ctrl+C  Quit",
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
	b.WriteString(m.bgLine("  Scanning remote ports and Docker containers...") + "\n")
	b.WriteString(m.bgLine("  (this may take a moment)") + "\n")
	for i := 3; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func formatPortLabel(p portInfo) string {
	label := fmt.Sprintf("  %d", p.Port)
	if p.Process != "" {
		svc, known := wellKnownPorts[p.Port]
		if known && svc != p.Process {
			label += fmt.Sprintf("  %s (%s)", svc, p.Process)
		} else {
			label += fmt.Sprintf("  %s", p.Process)
		}
	} else if svc, known := wellKnownPorts[p.Port]; known {
		label += fmt.Sprintf("  (%s)", svc)
	}
	return label
}

func (m *hostModel) renderTunnelAutoResults(b *strings.Builder, maxLines int) {
	title := fmt.Sprintf("  ── Open Ports on %s ──", m.tunnel.alias)
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")

	if m.tunnel.scanErr != "" {
		b.WriteString(m.bgLine("  Error: "+m.tunnel.scanErr) + "\n")
		return
	}

	totalPorts := len(m.tunnel.scanPorts)
	totalDocker := len(m.tunnel.dockerContainers)
	totalItems := totalPorts + totalDocker
	dockerOffset := totalPorts

	if totalPorts == 0 && totalDocker == 0 {
		b.WriteString(m.bgLine("  No open ports or containers found.") + "\n")
	}

	showPorts := totalPorts
	showDocker := totalDocker
	available := maxLines - 3 // leave room for help line
	if totalItems > available && totalDocker > 0 {
		// Try to show at least some of both
		portLines := available * totalPorts / totalItems
		if portLines < 1 {
			portLines = 1
		}
		dockerLines := available - portLines - 1 // 1 for docker header
		if dockerLines < 1 {
			showDocker = 0
			showPorts = available
		} else {
			showPorts = portLines
			showDocker = dockerLines
		}
	} else if totalItems > available {
		showPorts = available
		showDocker = 0
	}

	// Render ports
	rendered := 0
	for i := 0; i < totalPorts && rendered < showPorts; i++ {
		p := m.tunnel.scanPorts[i]
		line := formatPortLabel(p)
		if i == m.tunnel.scanCursor {
			b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+line)) + "\n")
		} else {
			b.WriteString(m.bgLine(m.inactiveStyle.Render("   "+line)) + "\n")
		}
		rendered++
	}

	// Render Docker containers after ports
	if totalDocker > 0 && showDocker > 0 {
		b.WriteString(m.bgLine("") + "\n")
		rendered++
		dockerTitle := m.helpStyle.Render("  ── Docker Containers ──")
		b.WriteString(m.bgLine(dockerTitle) + "\n")
		rendered++
		for i := 0; i < totalDocker && i < showDocker; i++ {
			c := m.tunnel.dockerContainers[i]
			line := fmt.Sprintf("  · %s   %s", c.Name, c.Ports)
			cursorIdx := dockerOffset + i
			if cursorIdx == m.tunnel.scanCursor {
				b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+line)) + "\n")
			} else {
				b.WriteString(m.bgLine(m.inactiveStyle.Render("   "+line)) + "\n")
			}
			rendered++
		}
	}

	// fill remaining lines
	for i := rendered; i < maxLines-2; i++ {
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

func (m *hostModel) renderEditView(b *strings.Builder, maxLines int) {
	title := fmt.Sprintf("  ── Edit %s ──", m.edit.fields[0].value)
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")
	b.WriteString(m.bgLine("") + "\n")

	for i, f := range m.edit.fields {
		if i >= maxLines-3 {
			break
		}
		label := fmt.Sprintf("  %s: ", f.label)
		display := f.value
		if f.kind == "password" {
			if display != "" {
				display = strings.Repeat("•", len([]rune(display)))
			}
		}
		line := label + display
		if i == m.edit.cursor {
			b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+line+"▌")) + "\n")
		} else {
			b.WriteString(m.bgLine("   " + line) + "\n")
		}
	}
	if m.groupPicker.active {
		m.renderGroupPicker(b, maxLines-len(m.edit.fields)-3)
		return
	}
	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(m.helpStyle.Render("  ↑↓ Tab navigate  Enter save  Esc cancel")) + "\n")
	for i := len(m.edit.fields) + 3; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func (m *hostModel) renderNewHost(b *strings.Builder, maxLines int) {
	title := "  ── New Host ──"
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")
	b.WriteString(m.bgLine("") + "\n")

	for i, f := range m.newHost.fields {
		if i >= maxLines-4 {
			break
		}
		label := fmt.Sprintf("  %s: ", f.label)
		display := f.value
		if f.kind == "password" && display != "" {
			display = strings.Repeat("•", len([]rune(display)))
		}
		line := label + display
		if i == m.newHost.cursor {
			b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+line+"▌")) + "\n")
		} else {
			b.WriteString(m.bgLine("   " + line) + "\n")
		}
	}

	if m.groupPicker.active {
		m.renderGroupPicker(b, maxLines-len(m.newHost.fields)-3)
		return
	}
	if m.newHost.err != "" {
		b.WriteString(m.bgLine("") + "\n")
		b.WriteString(m.bgLine(m.activeStyle.Render("  ⚠ " + m.newHost.err)) + "\n")
	}
	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(m.helpStyle.Render("  ↑↓ Tab navigate  Enter save  Esc cancel")) + "\n")

	used := len(m.newHost.fields) + 3
	if m.newHost.err != "" {
		used += 2
	}
	for i := used; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func (m *hostModel) renderDeleteAsk(b *strings.Builder, maxLines int) {
	alias := ""
	if m.deleteIdx >= 0 && m.deleteIdx < len(m.hosts) {
		alias = m.hosts[m.deleteIdx].Alias
	}
	title := "  ── Confirm Delete ──"
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")
	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(fmt.Sprintf("  Delete host \"%s\"?", alias)) + "\n")
	b.WriteString(m.bgLine("  This will remove it from "+userConfig.configPath) + "\n")
	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(m.activeStyle.Render("▐█ [Yes]")) + "\n")
	b.WriteString(m.bgLine("   [No]") + "\n")
	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(m.helpStyle.Render("  Enter to confirm  Esc to cancel")) + "\n")
	for i := 8; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func (m *hostModel) renderGroupsView(b *strings.Builder, maxLines int) {
	switch m.groups.view {
	case "menu":
		m.renderGroupsMenu(b, maxLines)
	case "new_name":
		m.renderGroupsNewName(b, maxLines)
	case "new_hosts":
		m.renderGroupsHostList(b, maxLines, true)
	case "edit_group":
		m.renderGroupsEditGroup(b, maxLines)
	case "edit_hosts":
		m.renderGroupsHostList(b, maxLines, false)
	}
}

func (m *hostModel) renderGroupsMenu(b *strings.Builder, maxLines int) {
	b.WriteString(m.bgLine(m.activeStyle.Render("  ── Groups ──")) + "\n")
	b.WriteString(m.bgLine("") + "\n")
	items := []string{"New", "Edit"}
	for i, item := range items {
		if i == m.groups.cursor {
			b.WriteString(m.bgLine(m.activeStyle.Render("▐█ ["+item+"]")) + "\n")
		} else {
			b.WriteString(m.bgLine("   ["+item+"]") + "\n")
		}
	}
	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(m.helpStyle.Render("  ↑↓ Navigate  Enter select  Esc back")) + "\n")
	for i := 5; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func (m *hostModel) renderGroupsNewName(b *strings.Builder, maxLines int) {
	b.WriteString(m.bgLine(m.activeStyle.Render("  ── New Group ──")) + "\n")
	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine("  Group name:") + "\n")
	name := m.groups.input
	b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+name+"▌")) + "\n")
	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(m.helpStyle.Render("  Enter to confirm  Esc back")) + "\n")
	for i := 6; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func (m *hostModel) renderGroupsEditGroup(b *strings.Builder, maxLines int) {
	b.WriteString(m.bgLine(m.activeStyle.Render("  ── Select Group ──")) + "\n")
	b.WriteString(m.bgLine("") + "\n")
	groups := m.groups.groupHosts
	if len(groups) == 0 {
		b.WriteString(m.bgLine("  No groups defined yet.") + "\n")
	} else {
		for i, h := range groups {
			line := "  " + h.Alias
			if i == m.groups.cursor {
				b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+line)) + "\n")
			} else {
				b.WriteString(m.bgLine("   "+line) + "\n")
			}
		}
	}
	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(m.helpStyle.Render("  ↑↓ Navigate  Enter select  Esc back")) + "\n")
	for i := len(groups) + 4; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func (m *hostModel) renderGroupsHostList(b *strings.Builder, maxLines int, isNew bool) {
	title := "  ── Select Hosts ──"
	if !isNew {
		title = "  ── " + m.groups.editingGroup + " ──"
	}
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")
	b.WriteString(m.bgLine("") + "\n")

	hosts := m.groups.groupHosts
	sele := m.groups.groupSele
	if len(hosts) == 0 {
		b.WriteString(m.bgLine("  No hosts available.") + "\n")
	} else {
		for i, h := range hosts {
			check := " "
			if sele[i] {
				check = "✓"
			}
			line := fmt.Sprintf(" [%s] %s", check, h.Alias)
			if i == m.groups.cursor {
				b.WriteString(m.bgLine(m.activeStyle.Render("▐█"+line)) + "\n")
			} else {
				b.WriteString(m.bgLine("  "+line) + "\n")
			}
		}
	}
	b.WriteString(m.bgLine("") + "\n")
	if isNew {
		b.WriteString(m.bgLine(m.helpStyle.Render("  Space toggle  Enter save  Esc back")) + "\n")
	} else {
		b.WriteString(m.bgLine(m.helpStyle.Render("  Space toggle removal  Enter confirm  Esc back")) + "\n")
	}
	for i := len(hosts) + 4; i < maxLines; i++ {
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
	ports       []portInfo
	containers  []dockerContainer
	err         error
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

var wellKnownPorts = map[int]string{
	20: "ftp-data", 21: "ftp", 22: "ssh", 23: "telnet",
	25: "smtp", 53: "dns", 80: "http", 110: "pop3",
	143: "imap", 443: "https", 465: "smtps", 587: "submission",
	993: "imaps", 995: "pop3s", 1433: "mssql", 1521: "oracle-db",
	1701: "l2tp", 1723: "pptp", 2082: "cpanel", 2083: "cpanels",
	2086: "webmin", 2087: "webmins", 2222: "directadmin",
	3306: "mysql", 3389: "rdp", 3690: "svn", 5432: "postgresql",
	5900: "vnc", 5901: "vnc-1", 5902: "vnc-2",
	5985: "winrm-http", 5986: "winrm-https",
	6379: "redis", 6443: "kube-api", 8000: "http-alt",
	8080: "http-proxy", 8443: "https-alt", 9000: "php-fpm",
	9090: "cockpit", 9100: "node-exporter", 9200: "elasticsearch",
	9300: "es-cluster", 9443: "webmin-alt",
	10000: "webmin-1", 11211: "memcached",
	27017: "mongodb", 27018: "mongo-shard", 27019: "mongo-config",
	32400: "plex",
}

func extractPortFromAddr(addr string) int {
	last := strings.LastIndex(addr, ":")
	if last < 0 {
		return 0
	}
	portStr := addr[last+1:]
	portStr = strings.TrimRight(portStr, "]")
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return 0
	}
	return port
}

func extractProcessSS(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	last := fields[len(fields)-1]
	start := strings.Index(last, `(("`)
	if start < 0 {
		return ""
	}
	start += 3
	end := strings.Index(last[start:], `"`)
	if end < 0 {
		return ""
	}
	return last[start : start+end]
}

func extractProcessNetstat(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	last := fields[len(fields)-1]
	idx := strings.Index(last, "/")
	if idx >= 0 && idx+1 < len(last) {
		return last[idx+1:]
	}
	return last
}

func parsePortLine(line string) (port int, process string, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return 0, "", false
	}

	first := fields[0]
	switch {
	case first == "LISTEN":
		// ss format: LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",...))
		port = extractPortFromAddr(fields[3])
		if port == 0 {
			return 0, "", false
		}
		process = extractProcessSS(fields)
		return port, process, true

	case first == "tcp" || first == "tcp6":
		// netstat format: tcp 0 0 0.0.0.0:22 0.0.0.0:* LISTEN 1234/sshd
		if len(fields) < 7 {
			return 0, "", false
		}
		port = extractPortFromAddr(fields[3])
		if port == 0 {
			return 0, "", false
		}
		process = extractProcessNetstat(fields)
		return port, process, true

	default:
		// Fallback: scan all fields for :port (original behavior)
		for _, f := range fields {
			if strings.Contains(f, ":") {
				parts := strings.Split(f, ":")
				portStr := parts[len(parts)-1]
				if strings.Contains(portStr, "-") {
					continue
				}
				p, err := strconv.Atoi(portStr)
				if err == nil && p > 0 && p < 65536 {
					return p, "", true
				}
			}
		}
		return 0, "", false
	}
}

func parseDockerOutput(output string) []dockerContainer {
	var containers []dockerContainer
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			containers = append(containers, dockerContainer{
				Name:  strings.TrimSpace(parts[0]),
				Ports: strings.TrimSpace(parts[1]),
			})
		}
	}
	return containers
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

		// Combined command: port scan + docker detection
		output, err := session.Output(
			`(ss -tlnp 2>/dev/null || netstat -tlnp 2>/dev/null || ss -tln 2>/dev/null || true) ` +
				`&& echo "===DOCKER===" ` +
				`&& (docker ps --format '{{.Names}}` + "\t" + `{{.Ports}}' 2>/dev/null || true)`,
		)
		if err != nil {
			return tunnelScanResultMsg{err: fmt.Errorf("scan command failed: %v", err)}
		}

		parts := strings.SplitN(string(output), "===DOCKER===\n", 2)
		scanOut := parts[0]
		dockerOut := ""
		if len(parts) > 1 {
			dockerOut = strings.TrimSpace(parts[1])
		}

		// Parse listening ports with process info
		var ports []portInfo
		seen := make(map[int]bool)
		for _, line := range strings.Split(scanOut, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			first := strings.Fields(line)[0]
			if first == "State" || first == "Proto" || first == "Netid" {
				continue
			}
			port, process, ok := parsePortLine(line)
			if ok && !seen[port] {
				seen[port] = true
				ports = append(ports, portInfo{Port: port, Process: process})
			}
		}
		sort.Slice(ports, func(i, j int) bool { return ports[i].Port < ports[j].Port })

		// Parse Docker containers
		containers := parseDockerOutput(dockerOut)

		return tunnelScanResultMsg{ports: ports, containers: containers, err: nil}
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

// --- Edit functions ---

func buildEditFields(alias string) []editField {
	host, port, user := resolveHostPortUser(alias)
	pw := getExConfig(alias, "Password")
	group := getGroupLabels(alias)
	return []editField{
		{"Alias", alias, "", "text"},
		{"HostName", host, "HostName", "text"},
		{"Port", port, "Port", "text"},
		{"User", user, "User", "text"},
		{"Password", pw, "Password", "password"},
		{"Group", group, "", "group"},
	}
}

func buildConfigFields() []configField {
	alive := fmt.Sprintf("%d", userConfig.defaultServerAliveInterval)
	theme := userConfig.promptThemeLayout
	if theme == "" {
		theme = "simple"
	}
	mode := userConfig.promptDefaultMode
	if mode == "" {
		mode = "normal"
	}
	pageSize := fmt.Sprintf("%d", userConfig.promptPageSize)
	if pageSize == "0" {
		pageSize = "10"
	}
	title := userConfig.setTerminalTitle
	if title == "" {
		title = "no"
	}
	uploadCmd := userConfig.dragFileUploadCommand
	downloadPath := userConfig.defaultDownloadPath
	lang := userConfig.language
	if lang == "" {
		lang = "english"
	}
	return []configField{
		{"defaultserveraliveinterval", "ServerAliveInterval (0=off)", alive, "number"},
		{"setterminaltitle", "Set terminal title", title, "bool"},
		{"dragfileuploadcommand", "Drag-file upload command", uploadCmd, "text"},
		{"defaultdownloadpath", "Default download path", downloadPath, "text"},
		{"promptthemelayout", "Theme layout (tiny/simple/table)", theme, "text"},
		{"promptdefaultmode", "Default mode (search/normal)", mode, "text"},
		{"promptpagesize", "Page size", pageSize, "number"},
		{"language", "Language (english/chinese)", lang, "text"},
	}
}

func (m *hostModel) handleConfig(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.config.saved {
		if msg.String() == "enter" || msg.String() == "esc" {
			m.config.active = false
			m.config.saved = false
		}
		return m, nil
	}
	s := msg.String()
	txt := msg.Key().Text
	switch s {
	case "up", "k", "shift+tab":
		if m.config.cursor > 0 {
			m.config.cursor--
		}
		return m, nil
	case "down", "j", "tab":
		if m.config.cursor < len(m.config.fields)-1 {
			m.config.cursor++
		}
		return m, nil
	case "enter":
		return m.saveConfig()
	case "esc":
		m.config.active = false
		m.config.err = ""
		return m, nil
	case "backspace":
		f := &m.config.fields[m.config.cursor]
		if len(f.value) > 0 {
			f.value = f.value[:len(f.value)-1]
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
		f := &m.config.fields[m.config.cursor]
		if f.kind == "number" {
			for _, r := range txt {
				if r >= '0' && r <= '9' {
					f.value += string(r)
				}
			}
		} else {
			f.value += txt
		}
		return m, nil
	}
}

func (m *hostModel) saveConfig() (tea.Model, tea.Cmd) {
	if err := m.writeTsshConfig(); err != nil {
		m.config.err = err.Error()
		return m, nil
	}
	m.config.saved = true
	m.config.err = ""
	return m, nil
}

func getTsshConfWritePath() string {
	return getTsshConfigPath(true)
}

func (m *hostModel) writeTsshConfig() error {
	path := getTsshConfWritePath()
	if path == "" {
		return fmt.Errorf("cannot determine config path")
	}

	var lines []string
	for _, f := range m.config.fields {
		val := f.value
		if val == "" && f.key == "defaultserveraliveinterval" {
			val = "0"
		}
		if val != "" {
			switch f.key {
			case "defaultserveraliveinterval":
				lines = append(lines, fmt.Sprintf("DefaultServerAliveInterval = %s", val))
			case "setterminaltitle":
				lines = append(lines, fmt.Sprintf("SetTerminalTitle = %s", val))
			case "dragfileuploadcommand":
				lines = append(lines, fmt.Sprintf("DragFileUploadCommand = %s", val))
			case "defaultdownloadpath":
				lines = append(lines, fmt.Sprintf("DefaultDownloadPath = %s", val))
			case "promptthemelayout":
				lines = append(lines, fmt.Sprintf("PromptThemeLayout = %s", val))
			case "promptdefaultmode":
				lines = append(lines, fmt.Sprintf("PromptDefaultMode = %s", val))
			case "promptpagesize":
				lines = append(lines, fmt.Sprintf("PromptPageSize = %s", val))
			case "language":
				lines = append(lines, fmt.Sprintf("Language = %s", val))
			}
		}
	}

	data := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(data), 0600)
}

func (m *hostModel) renderConfigView(b *strings.Builder, maxLines int) {
	if m.config.saved {
		title := "  ── Config Saved ──"
		b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")
		b.WriteString(m.bgLine("") + "\n")
		b.WriteString(m.bgLine("  Configuration saved to "+getTsshConfWritePath()) + "\n")
		b.WriteString(m.bgLine("") + "\n")
		b.WriteString(m.bgLine(m.activeStyle.Render("  ⚠  Please close and restart tssh for changes")) + "\n")
		b.WriteString(m.bgLine(m.activeStyle.Render("     to take effect.")) + "\n")
		b.WriteString(m.bgLine("") + "\n")
		b.WriteString(m.bgLine(m.helpStyle.Render("  Enter or Esc to go back")) + "\n")
		for i := 7; i < maxLines; i++ {
			b.WriteString(m.bgLine("") + "\n")
		}
		return
	}

	title := "  ── Config ──"
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")
	b.WriteString(m.bgLine("") + "\n")

	for i, f := range m.config.fields {
		if i >= maxLines-4 {
			break
		}
		display := f.value
		if display == "" {
			display = "(empty)"
		}
		line := fmt.Sprintf("  %s: %s", f.label, display)
		if i == m.config.cursor {
			b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+line+"▌")) + "\n")
		} else {
			b.WriteString(m.bgLine("   " + line) + "\n")
		}
	}

	if m.config.err != "" {
		b.WriteString(m.bgLine("") + "\n")
		b.WriteString(m.bgLine(m.activeStyle.Render("  ⚠ " + m.config.err)) + "\n")
	}

	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(m.helpStyle.Render("  ↑↓ Tab navigate  Enter save  Esc cancel")) + "\n")

	used := len(m.config.fields) + 3
	if m.config.err != "" {
		used += 2
	}
	for i := used; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func (m *hostModel) handleDownload(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	txt := msg.Key().Text
	switch s {
	case "up", "k", "shift+tab":
		if m.download.cursor > 0 {
			m.download.cursor--
		}
		return m, nil
	case "down", "j", "tab":
		if m.download.cursor < len(m.download.fields)-1 {
			m.download.cursor++
		}
		return m, nil
	case "enter":
		paths := m.download.fields[0].value
		if paths == "" {
			m.download.err = "Remote paths are required"
			return m, nil
		}
		m.done = true
		m.result = hostChoiceMsg{
			download: &downloadReq{
				alias:       m.download.alias,
				remotePaths: paths,
				localPath:   m.download.fields[1].value,
			},
		}
		return m, tea.Quit
	case "esc":
		m.download.active = false
		m.download.err = ""
		return m, nil
	case "backspace":
		f := &m.download.fields[m.download.cursor]
		if len(f.value) > 0 {
			f.value = f.value[:len(f.value)-1]
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
		f := &m.download.fields[m.download.cursor]
		f.value += txt
		return m, nil
	}
}

func (m *hostModel) renderDownloadView(b *strings.Builder, maxLines int) {
	title := fmt.Sprintf("  ── Download from %s ──", m.download.alias)
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")
	b.WriteString(m.bgLine("") + "\n")

	for i, f := range m.download.fields {
		if i >= maxLines-4 {
			break
		}
		display := f.value
		if display == "" {
			display = "(empty)"
		}
		line := fmt.Sprintf("  %s: %s", f.label, display)
		if i == m.download.cursor {
			b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+line+"▌")) + "\n")
		} else {
			b.WriteString(m.bgLine("   " + line) + "\n")
		}
	}

	if m.download.err != "" {
		b.WriteString(m.bgLine("") + "\n")
		b.WriteString(m.bgLine(m.activeStyle.Render("  ⚠ " + m.download.err)) + "\n")
	}

	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(m.helpStyle.Render("  Enter to download  Esc cancel")) + "\n")

	used := len(m.download.fields) + 3
	if m.download.err != "" {
		used += 2
	}
	for i := used; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func (m *hostModel) handleEdit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	txt := msg.Key().Text
	switch s {
	case "up", "k", "shift+tab":
		if m.edit.cursor > 0 {
			m.edit.cursor--
		}
		return m, nil
	case "down", "j", "tab":
		if m.edit.cursor < len(m.edit.fields)-1 {
			m.edit.cursor++
		}
		return m, nil
	case "enter":
		f := &m.edit.fields[m.edit.cursor]
		if f.kind == "group" {
			return m.activateGroupPicker("edit")
		}
		return m.saveEdit()
	case "esc":
		m.edit.active = false
		return m, nil
	case "backspace":
		f := &m.edit.fields[m.edit.cursor]
		if len(f.value) > 0 {
			f.value = f.value[:len(f.value)-1]
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
		f := &m.edit.fields[m.edit.cursor]
		if f.configKey == "Port" {
			for _, r := range txt {
				if r >= '0' && r <= '9' {
					f.value += string(r)
				}
			}
		} else if f.kind != "group" {
			f.value += txt
		}
		return m, nil
	}
}

func (m *hostModel) handleNewHost(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	txt := msg.Key().Text
	switch s {
	case "up", "k", "shift+tab":
		if m.newHost.cursor > 0 {
			m.newHost.cursor--
		}
	case "down", "j", "tab":
		if m.newHost.cursor < len(m.newHost.fields)-1 {
			m.newHost.cursor++
		}
	case "enter":
		f := &m.newHost.fields[m.newHost.cursor]
		if f.kind == "group" {
			return m.activateGroupPicker("newHost")
		}
		return m.saveNewHost()
	case "esc":
		m.newHost.active = false
		m.newHost.err = ""
	case "backspace":
		f := &m.newHost.fields[m.newHost.cursor]
		if len(f.value) > 0 {
			f.value = f.value[:len(f.value)-1]
		}
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	default:
		if txt == "" {
			return m, nil
		}
		f := &m.newHost.fields[m.newHost.cursor]
		if f.label == "Port" {
			for _, r := range txt {
				if r >= '0' && r <= '9' {
					f.value += string(r)
				}
			}
		} else if f.kind != "group" {
			f.value += txt
		}
	}
	return m, nil
}

func (m *hostModel) saveNewHost() (tea.Model, tea.Cmd) {
	fields := m.newHost.fields
	if len(fields) < 4 {
		return m, nil
	}
	alias := fields[0].value
	hostName := fields[1].value
	port := fields[2].value
	user := fields[3].value
	password := fields[4].value
	group := ""
	for _, f := range fields {
		if f.kind == "group" {
			group = f.value
		}
	}

	if alias == "" || hostName == "" || user == "" {
		m.newHost.err = "Alias, HostName/IP, and User are required"
		return m, nil
	}
	if port == "" {
		port = "22"
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		m.newHost.err = "Port must be a number between 1 and 65535"
		return m, nil
	}
	// Check for duplicate alias
	for _, h := range m.hosts {
		if h.Alias == alias {
			m.newHost.err = "Host alias already exists"
			return m, nil
		}
	}

	writeNewHostConfig(alias, hostName, port, user, password)

	// Save group
	if group != "" {
		updateGroupLabelsConfig(alias, group)
	}

	// Add to in-memory list
	host := &sshHost{
		Alias:       alias,
		Host:        hostName,
		Port:        port,
		User:        user,
		GroupLabels: group,
	}
	m.hosts = append(m.hosts, host)
	m.filtered = append(m.filtered, host)

	// Re-sort filtered to maintain group order
	m.applyFilter()

	m.newHost.active = false
	m.newHost.err = ""
	return m, nil
}

func writeNewHostConfig(alias, hostName, port, user, password string) {
	path := userConfig.configPath
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "\nHost %s\n    HostName %s\n    Port %s\n    User %s\n", alias, hostName, port, user)
	if password != "" {
		secret, err := encodeSecret([]byte(password))
		if err != nil {
			return
		}
		fmt.Fprintf(f, "    #!! encPassword %s\n    #!! encQuestionAnswer1 %s\n", secret, secret)
	}
}

func (m *hostModel) saveEdit() (tea.Model, tea.Cmd) {
	fields := m.edit.fields
	if len(fields) < 4 {
		m.edit.active = false
		return m, nil
	}
	newAlias := fields[0].value
	hostName := fields[1].value
	port := fields[2].value
	user := fields[3].value
	pw := fields[4].value
	group := ""
	for _, f := range fields {
		if f.kind == "group" {
			group = f.value
		}
	}

	origAlias := ""
	hostIdx := m.edit.hostIdx
	if hostIdx >= 0 && hostIdx < len(m.hosts) {
		origAlias = m.hosts[hostIdx].Alias
	}

	if origAlias == "" {
		m.edit.active = false
		return m, nil
	}

	// Update main SSH config
	updateHostConfig(origAlias, newAlias, hostName, port, user)

	// Update password in extended config
	updatePasswordConfig(origAlias, pw)

	// Update group in extended config
	if group != getGroupLabels(origAlias) {
		updateGroupLabelsConfig(origAlias, group)
	}

	// Fix filtered reference
	if hostIdx >= 0 && hostIdx < len(m.hosts) {
		m.hosts[hostIdx].Alias = newAlias
		m.hosts[hostIdx].Host = hostName
		m.hosts[hostIdx].Port = port
		m.hosts[hostIdx].User = user
		m.hosts[hostIdx].GroupLabels = group
	}
	// If alias changed, also update filtered entries
	for i := range m.filtered {
		if m.filtered[i] == m.hosts[hostIdx] {
			m.filtered[i] = m.hosts[hostIdx]
			break
		}
	}

	// Refresh filter to re-sort by group
	m.applyFilter()

	m.edit.active = false
	return m, nil
}

func resolveHostPortUser(alias string) (string, string, string) {
	h := getConfig(alias, "HostName")
	if h == "" {
		h = alias
	}
	p := getConfig(alias, "Port")
	if p == "" {
		p = "22"
	}
	u := getConfig(alias, "User")
	return h, p, u
}

func updateHostConfig(origAlias, newAlias, hostName, port, user string) {
	path := userConfig.configPath
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	inBlock := false
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lowTrim := strings.ToLower(trimmed)

		if strings.HasPrefix(lowTrim, "host ") {
			fields := strings.Fields(trimmed)
			if len(fields) >= 2 && fields[1] == origAlias {
				inBlock = true
				// Replace Host line if alias changed
				if newAlias != origAlias {
					line = strings.Replace(line, origAlias, newAlias, 1)
				}
				result = append(result, line)
				continue
			}
			inBlock = false
			result = append(result, line)
			continue
		}

		if !inBlock {
			result = append(result, line)
			continue
		}

		// Inside the target host block — update directives
		lowLine := strings.ToLower(trimmed)
		// Skip lines we are replacing
		if strings.HasPrefix(lowLine, "hostname ") ||
			strings.HasPrefix(lowLine, "port ") ||
			strings.HasPrefix(lowLine, "user ") {
			// Skip old line, we'll add new one at end of block
			continue
		}
		result = append(result, line)
	}

	// Write back
	_ = os.WriteFile(path, []byte(strings.Join(result, "\n")), 0600)

	// Now append new directives if they differ
	if inBlock {
		appendDirectives(origAlias, newAlias, hostName, port, user)
	}
}

func appendDirectives(origAlias, newAlias, hostName, port, user string) {
	path := userConfig.configPath
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")

	// Find the host block and insert directives inside it
	alias := origAlias
	if newAlias != origAlias {
		alias = newAlias
	}

	var result []string
	inserted := false
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lowTrim := strings.ToLower(trimmed)

		if strings.HasPrefix(lowTrim, "host ") {
			fields := strings.Fields(trimmed)
			if len(fields) >= 2 && fields[1] == alias {
				inBlock = true
				result = append(result, line)
				continue
			}
			if inBlock {
				inBlock = false
			}
		}
		if !inBlock {
			result = append(result, line)
			continue
		}
		result = append(result, line)
	}
	// Insert directives after the Host line
	insertIdx := -1
	for i, line := range result {
		trimmed := strings.TrimSpace(line)
		lowTrim := strings.ToLower(trimmed)
		if strings.HasPrefix(lowTrim, "host ") {
			fields := strings.Fields(trimmed)
			if len(fields) >= 2 && fields[1] == alias {
				insertIdx = i + 1
				break
			}
		}
	}

	if insertIdx >= 0 {
		// Find where block ends (next Host or end of file)
		endIdx := len(result)
		for i := insertIdx; i < len(result); i++ {
			trimmed := strings.TrimSpace(result[i])
			lowTrim := strings.ToLower(trimmed)
			if strings.HasPrefix(lowTrim, "host ") && !strings.HasPrefix(lowTrim, "hostname ") {
				endIdx = i
				break
			}
		}

		// Remove old directives within the block
		var cleaned []string
		cleaned = append(cleaned, result[:insertIdx]...)
		for i := insertIdx; i < endIdx; i++ {
			trimmed := strings.TrimSpace(result[i])
			lowTrim := strings.ToLower(trimmed)
			if strings.HasPrefix(lowTrim, "hostname ") ||
				strings.HasPrefix(lowTrim, "port ") ||
				strings.HasPrefix(lowTrim, "user ") {
				continue
			}
			cleaned = append(cleaned, result[i])
		}
		cleaned = append(cleaned, result[endIdx:]...)
		result = cleaned

		// Find new insert position
		newInsertIdx := -1
		for i, line := range result {
			trimmed := strings.TrimSpace(line)
			lowTrim := strings.ToLower(trimmed)
			if strings.HasPrefix(lowTrim, "host ") {
				fields := strings.Fields(trimmed)
				if len(fields) >= 2 && fields[1] == alias {
					newInsertIdx = i + 1
					break
				}
			}
		}

		if newInsertIdx >= 0 && !inserted {
			var withDirectives []string
			withDirectives = append(withDirectives, result[:newInsertIdx]...)
			if hostName != "" && hostName != alias {
				withDirectives = append(withDirectives, "    HostName "+hostName)
			}
			if port != "" && port != "22" {
				withDirectives = append(withDirectives, "    Port "+port)
			}
			if user != "" {
				withDirectives = append(withDirectives, "    User "+user)
			}
			withDirectives = append(withDirectives, result[newInsertIdx:]...)
			result = withDirectives
			inserted = true
		}
	}

	_ = os.WriteFile(path, []byte(strings.Join(result, "\n")), 0600)
}

func updatePasswordConfig(alias, password string) {
	path := userConfig.exConfigPath
	if path == "" {
		return
	}
	data, _ := os.ReadFile(path)
	lines := strings.Split(string(data), "\n")

	var result []string
	found := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lowTrim := strings.ToLower(trimmed)

		// Check for Password <alias> = ...
		if strings.HasPrefix(lowTrim, "password ") {
			parts := strings.SplitN(trimmed, " ", 3)
			if len(parts) >= 2 && parts[1] == alias {
				if password == "" {
					continue // remove line
				}
				found = true
				result = append(result, "Password "+alias+" = "+password)
				continue
			}
		}
		// Check for encPassword <alias> = ... (skip encrypted entries, we'll handle)
		if strings.HasPrefix(lowTrim, "encpassword ") {
			parts := strings.SplitN(trimmed, " ", 3)
			if len(parts) >= 2 && parts[1] == alias {
				if password == "" {
					continue
				}
				// Replace encPassword with plaintext
				found = true
				result = append(result, "Password "+alias+" = "+password)
				continue
			}
		}
		result = append(result, line)
	}

	if !found && password != "" {
		result = append(result, "Password "+alias+" = "+password)
	}

	_ = os.WriteFile(path, []byte(strings.Join(result, "\n")), 0600)

	// Reset cache so next getExConfig reads fresh data
	userConfig.exConfig = nil
	userConfig.loadExConfig = sync.Once{}
}

func updateGroupLabelsConfig(alias, group string) {
	path := userConfig.exConfigPath
	if path == "" {
		return
	}
	data, _ := os.ReadFile(path)
	lines := strings.Split(string(data), "\n")

	var result []string
	found := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lowTrim := strings.ToLower(trimmed)

		if strings.HasPrefix(lowTrim, "grouplabels ") {
			parts := strings.SplitN(trimmed, " ", 3)
			if len(parts) >= 2 && parts[1] == alias {
				if group == "" {
					continue // remove line
				}
				found = true
				result = append(result, "GroupLabels "+alias+" = "+group)
				continue
			}
		}
		result = append(result, line)
	}

	if !found && group != "" {
		result = append(result, "GroupLabels "+alias+" = "+group)
	}

	_ = os.WriteFile(path, []byte(strings.Join(result, "\n")), 0600)

	// Reset cache so next getGroupLabels reads fresh data
	userConfig.exConfig = nil
	userConfig.loadExConfig = sync.Once{}
}

func (m *hostModel) handleDeleteConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	switch s {
	case "enter":
		if m.deleteIdx >= 0 && m.deleteIdx < len(m.hosts) {
			alias := m.hosts[m.deleteIdx].Alias
			// Remove from in-memory lists
			m.hosts = append(m.hosts[:m.deleteIdx], m.hosts[m.deleteIdx+1:]...)
			// Also remove from filtered
			for i := 0; i < len(m.filtered); i++ {
				if m.filtered[i].Alias == alias {
					m.filtered = append(m.filtered[:i], m.filtered[i+1:]...)
					break
				}
			}
			m.clampCursor()
			// Reset deleteIdx so View won't access stale index
			m.deleteIdx = -1
			// Update config file for persistence
			removeHostFromConfig(alias)
		}
		m.deleteAsk = false
		return m, nil
	case "esc", "q", "n":
		m.deleteAsk = false
		return m, nil
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	}
	return m, nil
}

func (m *hostModel) handleGroups(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.groups.view {
	case "menu":
		return m.handleGroupsMenu(msg)
	case "new_name":
		return m.handleGroupsNewName(msg)
	case "new_hosts":
		return m.handleGroupsHostList(msg, true)
	case "edit_group":
		return m.handleGroupsEditGroup(msg)
	case "edit_hosts":
		return m.handleGroupsHostList(msg, false)
	}
	return m, nil
}

func (m *hostModel) handleGroupsMenu(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	items := []string{"New", "Edit"}
	switch s {
	case "up", "k":
		if m.groups.cursor > 0 {
			m.groups.cursor--
		}
	case "down", "j":
		if m.groups.cursor < len(items)-1 {
			m.groups.cursor++
		}
	case "enter":
		switch m.groups.cursor {
		case 0: // New
			m.groups.view = "new_name"
			m.groups.cursor = 0
			m.groups.input = ""
		case 1: // Edit
			allGroups := collectAllGroups()
			if len(allGroups) == 0 {
				m.groups.err = "No groups defined"
				return m, nil
			}
			m.groups.groupHosts = allGroups
			m.groups.view = "edit_group"
			m.groups.cursor = 0
		}
	case "esc":
		m.groups.active = false
		m.groups.input = ""
		m.groups.err = ""
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	}
	return m, nil
}

func (m *hostModel) handleGroupsNewName(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	txt := msg.Key().Text
	switch s {
	case "enter":
		name := strings.TrimSpace(m.groups.input)
		if name == "" {
			m.groups.err = "Name cannot be empty"
			return m, nil
		}
		// Build host list with all hosts (none pre-selected)
		var hosts []*sshHost
		for _, h := range m.hosts {
			if !h.System {
				hosts = append(hosts, h)
			}
		}
		m.groups.input = name
		m.groups.groupHosts = hosts
		m.groups.groupSele = make([]bool, len(hosts))
		m.groups.cursor = 0
		m.groups.err = ""
		m.groups.view = "new_hosts"
	case "esc":
		m.groups.view = "menu"
		m.groups.cursor = 0
		m.groups.input = ""
		m.groups.err = ""
	case "backspace":
		if len(m.groups.input) > 0 {
			m.groups.input = m.groups.input[:len(m.groups.input)-1]
		}
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	default:
		if txt == "" {
			return m, nil
		}
		m.groups.input += txt
	}
	return m, nil
}

func (m *hostModel) handleGroupsEditGroup(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	groups := m.groups.groupHosts
	switch s {
	case "up", "k":
		if m.groups.cursor > 0 {
			m.groups.cursor--
		}
	case "down", "j":
		if m.groups.cursor < len(groups)-1 {
			m.groups.cursor++
		}
	case "enter":
		if len(groups) == 0 {
			return m, nil
		}
		groupName := groups[m.groups.cursor].Alias
		m.groups.editingGroup = groupName
		// Find hosts in this group
		var hosts []*sshHost
		for _, h := range m.hosts {
			if !h.System && hasGroup(h, groupName) {
				hosts = append(hosts, h)
			}
		}
		m.groups.groupHosts = hosts
		m.groups.groupSele = make([]bool, len(hosts))
		for i := range m.groups.groupSele {
			m.groups.groupSele[i] = true // pre-selected (keep in group)
		}
		m.groups.cursor = 0
		m.groups.err = ""
		m.groups.view = "edit_hosts"
	case "esc":
		m.groups.view = "menu"
		m.groups.cursor = 0
		m.groups.input = ""
		m.groups.err = ""
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	}
	return m, nil
}

func (m *hostModel) handleGroupsHostList(msg tea.KeyPressMsg, isNew bool) (tea.Model, tea.Cmd) {
	s := msg.String()
	hosts := m.groups.groupHosts
	switch s {
	case "up", "k":
		if m.groups.cursor > 0 {
			m.groups.cursor--
		}
	case "down", "j":
		if m.groups.cursor < len(hosts)-1 {
			m.groups.cursor++
		}
	case " ":
		if m.groups.cursor >= 0 && m.groups.cursor < len(m.groups.groupSele) {
			m.groups.groupSele[m.groups.cursor] = !m.groups.groupSele[m.groups.cursor]
		}
	case "enter":
		return m, m.saveGroups(isNew)
	case "esc":
		if isNew {
			m.groups.view = "new_name"
		} else {
			m.groups.view = "edit_group"
			m.groups.cursor = 0
			m.groups.groupHosts = collectAllGroups()
		}
		m.groups.err = ""
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	}
	return m, nil
}

func (m *hostModel) saveGroups(isNew bool) tea.Cmd {
	groupName := m.groups.input
	if !isNew {
		groupName = m.groups.editingGroup
	}
	hosts := m.groups.groupHosts
	sele := m.groups.groupSele

	for i, h := range hosts {
		currentGroups := strings.Fields(h.GroupLabels)
		hasGroupName := func() bool {
			for _, g := range currentGroups {
				if strings.EqualFold(g, groupName) {
					return true
				}
			}
			return false
		}
		if isNew {
			// Add to group if selected
			if sele[i] && !hasGroupName() {
				currentGroups = append(currentGroups, groupName)
			}
		} else {
			// Remove from group if deselected
			if !sele[i] && hasGroupName() {
				var keep []string
				for _, g := range currentGroups {
					if !strings.EqualFold(g, groupName) {
						keep = append(keep, g)
					}
				}
				currentGroups = keep
			} else if sele[i] {
				continue
			} else {
				continue
			}
		}
		newLabels := strings.Join(currentGroups, " ")
		updateGroupLabelsConfig(h.Alias, newLabels)
		h.GroupLabels = newLabels
	}

	// Refresh
	m.applyFilter()
	m.groups.active = false
	m.groups.input = ""
	m.groups.err = ""
	m.groups.editingGroup = ""
	m.groups.groupHosts = nil
	m.groups.groupSele = nil
	return nil
}

func hasGroup(h *sshHost, group string) bool {
	for _, g := range strings.Fields(h.GroupLabels) {
		if strings.EqualFold(g, group) {
			return true
		}
	}
	return false
}

func collectAllGroups() []*sshHost {
	seen := make(map[string]bool)
	var groups []*sshHost
	for _, h := range userConfig.allHosts {
		if h.System {
			continue
		}
		for _, g := range strings.Fields(h.GroupLabels) {
			low := strings.ToLower(g)
			if !seen[low] {
				seen[low] = true
				groups = append(groups, &sshHost{Alias: g})
			}
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		return strings.ToLower(groups[i].Alias) < strings.ToLower(groups[j].Alias)
	})
	return groups
}

func collectGroupNames() []string {
	groups := collectAllGroups()
	names := make([]string, len(groups))
	for i, g := range groups {
		names[i] = g.Alias
	}
	return names
}

func (m *hostModel) activateGroupPicker(form string) (tea.Model, tea.Cmd) {
	names := collectGroupNames()
	m.groupPicker.active = true
	m.groupPicker.mode = "select"
	m.groupPicker.cursor = 0
	m.groupPicker.groups = names
	m.groupPicker.input = ""
	return m, nil
}

func (m *hostModel) handleGroupPicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	txt := msg.Key().Text

	if m.groupPicker.mode == "select" {
		switch s {
		case "up", "k":
			if m.groupPicker.cursor > 0 {
				m.groupPicker.cursor--
			}
			return m, nil
		case "down", "j":
			total := len(m.groupPicker.groups) + 1 // +1 for "New..."
			if m.groupPicker.cursor < total-1 {
				m.groupPicker.cursor++
			}
			return m, nil
		case "enter":
			totalGroups := len(m.groupPicker.groups)
			if m.groupPicker.cursor < totalGroups {
				// Select existing group
				groupName := m.groupPicker.groups[m.groupPicker.cursor]
				m.setGroupFieldValue(groupName)
				m.groupPicker.active = false
			} else {
				// Select "New..."
				m.groupPicker.mode = "new"
				m.groupPicker.input = ""
				m.groupPicker.cursor = 0
			}
			return m, nil
		case "esc":
			m.groupPicker.active = false
			return m, nil
		case "ctrl+c", "ctrl+q":
			m.done = true
			m.result = hostChoiceMsg{quit: true}
			return m, tea.Quit
		}
		return m, nil
	}

	// mode == "new"
	switch s {
	case "enter":
		name := strings.TrimSpace(m.groupPicker.input)
		if name != "" {
			m.setGroupFieldValue(name)
		}
		m.groupPicker.active = false
		return m, nil
	case "esc":
		m.groupPicker.mode = "select"
		m.groupPicker.cursor = 0
		m.groupPicker.input = ""
		return m, nil
	case "backspace":
		if len(m.groupPicker.input) > 0 {
			m.groupPicker.input = m.groupPicker.input[:len(m.groupPicker.input)-1]
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
		m.groupPicker.input += txt
		return m, nil
	}
}

func (m *hostModel) setGroupFieldValue(groupName string) {
	if m.edit.active {
		for i := range m.edit.fields {
			if m.edit.fields[i].kind == "group" {
				m.edit.fields[i].value = groupName
				return
			}
		}
	}
	if m.newHost.active {
		for i := range m.newHost.fields {
			if m.newHost.fields[i].kind == "group" {
				m.newHost.fields[i].value = groupName
				return
			}
		}
	}
}

func (m *hostModel) renderGroupPicker(b *strings.Builder, maxLines int) {
	if m.groupPicker.mode == "select" {
		b.WriteString(m.bgLine("") + "\n")
		b.WriteString(m.bgLine(m.activeStyle.Render("  ── Select Group ──")) + "\n")
		for i, name := range m.groupPicker.groups {
			line := "  " + name
			if i == m.groupPicker.cursor {
				b.WriteString(m.bgLine(m.activeStyle.Render("▐█"+line)) + "\n")
			} else {
				b.WriteString(m.bgLine("  "+line) + "\n")
			}
		}
		// "New..." at the end
		newIdx := len(m.groupPicker.groups)
		line := "  + New..."
		if newIdx == m.groupPicker.cursor {
			b.WriteString(m.bgLine(m.activeStyle.Render("▐█"+line)) + "\n")
		} else {
			b.WriteString(m.bgLine("  "+line) + "\n")
		}
		b.WriteString(m.bgLine("") + "\n")
		b.WriteString(m.bgLine(m.helpStyle.Render("  ↑↓ Navigate  Enter select  Esc back")) + "\n")
		// Fill remaining
		for i := len(m.groupPicker.groups) + 6; i < maxLines; i++ {
			b.WriteString(m.bgLine("") + "\n")
		}
	} else {
		b.WriteString(m.bgLine("") + "\n")
		b.WriteString(m.bgLine(m.activeStyle.Render("  ── New Group Name ──")) + "\n")
		b.WriteString(m.bgLine("") + "\n")
		b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+m.groupPicker.input+"▌")) + "\n")
		b.WriteString(m.bgLine("") + "\n")
		b.WriteString(m.bgLine(m.helpStyle.Render("  Enter create  Esc back")) + "\n")
		for i := 6; i < maxLines; i++ {
			b.WriteString(m.bgLine("") + "\n")
		}
	}
}

func removeHostFromConfig(alias string) {
	path := userConfig.configPath
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	var result []string
	skip := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lowTrim := strings.ToLower(trimmed)
		if strings.HasPrefix(lowTrim, "host ") {
			fields := strings.Fields(trimmed)
			if len(fields) >= 2 && fields[1] == alias {
				skip = true
				continue
			}
		}
		if skip {
			if trimmed == "" || strings.HasPrefix(lowTrim, "host ") {
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
	_ = os.WriteFile(path, []byte(strings.Join(result, "\n")), 0600)
}

// --- Host metadata (LastLogin, etc.) ---

func metaDefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".tssh_meta")
}

func metaLoadAll() map[string]map[string]string {
	path := metaDefaultPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var result map[string]map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}
	return result
}

func metaSaveAll(meta map[string]map[string]string) {
	path := metaDefaultPath()
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0600)
}

func metaUpdateLastLogin(alias string) {
	meta := metaLoadAll()
	if meta == nil {
		meta = make(map[string]map[string]string)
	}
	entry, ok := meta[alias]
	if !ok {
		entry = make(map[string]string)
		meta[alias] = entry
	}
	entry["last_login"] = time.Now().Format("2006-01-02 15:04")
	metaSaveAll(meta)
}

func (m *hostModel) loadMetaIntoHosts() {
	meta := metaLoadAll()
	if meta == nil {
		return
	}
	for _, h := range m.hosts {
		if entry, ok := meta[h.Alias]; ok {
			if ll, ok := entry["last_login"]; ok {
				h.LastLogin = ll
			}
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

		if model.result.editConfig {
			openEditor("")
			keywords = ""
			continue
		}

		if d := model.result.download; d != nil {
			execDownload(d)
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

func execDownload(d *downloadReq) {
	alias := d.alias
	remotePaths := d.remotePaths
	localPath := d.localPath

	args := []string{"-t", "--client"}
	if localPath != "" {
		args = append(args, "--download-path", localPath)
	}
	args = append(args, alias, fmt.Sprintf("tsz -d %s", remotePaths))

	debug("exec download: tssh %v", args)

	exe, err := os.Executable()
	if err != nil {
		warning("get executable path failed: %v", err)
		return
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Fprintf(os.Stderr, "\r\n\033[0;32mDownloading from %s...\033[0m\r\n", alias)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "\r\n\033[0;31mDownload failed: %v\033[0m\r\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "\r\n\033[0;32mDownload complete.\033[0m\r\n")
	}
	waitForEnterOnDownload()
}

func waitForEnterOnDownload() {
	fmt.Fprintf(os.Stderr, "Press Enter to continue...")
	_, _ = fmt.Scanln()
}
