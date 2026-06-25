/*
MIT License

Copyright (c) 2023-2026 The Trzsz SSH Authors.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package tssh

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/trzsz/tsshd/tsshd"
)

var udpAttachSessionID uint64

var errAttachSessionSelection = errors.New("attach session selection failed")

var errAttachTsshdTooOld = errors.New("tsshd is too old and does not support the attach feature")

type previewResultMsg struct {
	idx     int
	content string
}

type attachModel struct {
	tsshd    string
	client   SshClient
	items    []tsshd.ServerItem
	infos    []*tsshd.BaseInfo
	width    int
	height   int
	cursor   int
	offset   int
	chosen   int
	preview  string
	quitting bool
}

func (m *attachModel) Init() tea.Cmd {
	return tea.Batch(doTick(), m.fetchPreviewCmd(m.cursor))
}

func doTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *attachModel) fetchPreviewCmd(idx int) tea.Cmd {
	return func() tea.Msg {
		if idx == 0 {
			return previewResultMsg{idx: idx, content: "✨ Starting a brand new session..."}
		}

		item, info := m.items[idx-1], m.infos[idx-1]
		if info == nil {
			return previewResultMsg{idx: idx, content: item.Info}
		}
		if len(info.Sessions) == 0 {
			return previewResultMsg{idx: idx, content: "<< NO SESSION >>"}
		}

		output, err := execTsshdCommand(m.client, m.tsshd, fmt.Sprintf(" --view %d.%d", item.Pid, info.Sessions[0].ID))
		if err != nil {
			return previewResultMsg{idx: idx, content: fmt.Sprintf("ERROR: %v", err)}
		}

		return previewResultMsg{idx: idx, content: string(output)}
	}
}

func (m *attachModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		return m, tea.Batch(doTick(), m.fetchPreviewCmd(m.cursor))

	case previewResultMsg:
		if msg.idx == m.cursor {
			m.preview = msg.content
		}
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.chosen = -1
			m.quitting = true
			return m, tea.Quit
		case "up", "k", "shift+tab":
			if m.cursor > 0 {
				m.cursor--
			} else {
				m.cursor = len(m.items)
			}
			m.preview = "Loading..."
			return m, m.fetchPreviewCmd(m.cursor)
		case "down", "j", "tab":
			if m.cursor < len(m.items) {
				m.cursor++
			} else {
				m.cursor = 0
			}
			m.preview = "Loading..."
			return m, m.fetchPreviewCmd(m.cursor)
		case "enter":
			m.chosen = m.cursor
			m.quitting = true
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m *attachModel) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}

	if m.width == 0 || m.height == 0 {
		v := tea.NewView("Initializing...")
		v.AltScreen = true
		return v
	}

	// ncurses colors
	ncursesBlue := lipgloss.Color("4")
	ncursesGrey := lipgloss.Color("8")

	titleStyle := lipgloss.NewStyle().Background(ncursesBlue).Foreground(lipgloss.Color("15")).Bold(true)
	activeStyle := lipgloss.NewStyle().Background(ncursesBlue).Foreground(lipgloss.Color("15")).Bold(true)
	inactiveStyle := lipgloss.NewStyle()
	helpStyle := lipgloss.NewStyle().Foreground(ncursesGrey)

	bgLine := func(s string) string {
		w := lipgloss.Width(s)
		if w < m.width {
			s += strings.Repeat(" ", m.width-w)
		}
		return s
	}

	footerHeight := 2
	boxHeight := m.height - footerHeight - 2
	boxWidth := (m.width / 2) - 2

	if boxHeight < 5 || boxWidth < 10 {
		v := tea.NewView("Terminal window too small")
		v.AltScreen = true
		return v
	}

	var b strings.Builder

	// top border
	b.WriteString(bgLine("┌"+strings.Repeat("─", m.width-2)+"┐") + "\n")

	// title
	title := "  Attach to Session  "
	b.WriteString(bgLine(titleStyle.Render(" "+title+repeatSafe(m.width-3-ansi.StringWidth(title))+" ")) + "\n")

	// separator
	b.WriteString(bgLine("├"+strings.Repeat("─", m.width-2)+"┤") + "\n")

	// Two-panel content
	leftWidth := m.width/2 - 2
	rightWidth := m.width - leftWidth - 4

	listAvailableHeight := boxHeight
	if m.cursor < m.offset {
		m.offset = m.cursor
	} else if m.cursor >= m.offset+listAvailableHeight {
		m.offset = m.cursor - listAvailableHeight + 1
	}

	totalItems := len(m.items) + 1
	var leftLines []string
	for i := m.offset; i < m.offset+listAvailableHeight && i < totalItems; i++ {
		cursorStr := "  "
		if m.cursor == i {
			cursorStr = "▐█ "
		}

		var row string
		if i == 0 {
			row = "New session"
		} else {
			idx := i - 1
			item := m.items[idx]
			info := m.infos[idx]

			name := "-"
			startTime := "-"
			if info != nil {
				if info.Name != "" {
					name = info.Name
				}
				if info.Time > 0 {
					startTime = time.Unix(info.Time, 0).Local().Format("2006-01-02 15:04")
				}
			}
			row = fmt.Sprintf("%s | %s | %s", strconv.Itoa(item.Pid), name, startTime)
		}
		row = clipString(cursorStr+row, leftWidth)
		if m.cursor == i {
			leftLines = append(leftLines, activeStyle.Render(row))
		} else {
			leftLines = append(leftLines, inactiveStyle.Render(row))
		}
	}

	// fill remaining height
	for len(leftLines) < listAvailableHeight {
		leftLines = append(leftLines, inactiveStyle.Render(""))
	}

	// Render left + right panels side by side
	previewLines := strings.Split(clipText(m.preview, rightWidth, boxHeight), "\n")
	for i := 0; i < boxHeight; i++ {
		left := leftLines[i] + repeatSafe(leftWidth-lipgloss.Width(leftLines[i]))
		right := ""
		if i < len(previewLines) {
			right = previewLines[i] + repeatSafe(rightWidth-ansi.StringWidth(previewLines[i]))
		} else {
			right = repeatSafe(rightWidth)
		}
		b.WriteString(bgLine("  " + left + "  │  " + right + "  ") + "\n")
	}

	// separator
	b.WriteString(bgLine("├"+strings.Repeat("─", m.width-2)+"┤") + "\n")

	// footer
	footer := "  ↑/↓ Navigate  Enter Select  q Quit  "
	b.WriteString(bgLine(helpStyle.Render(footer+repeatSafe(m.width-1-ansi.StringWidth(footer)))) + "\n")

	// bottom border
	b.WriteString(bgLine("└"+strings.Repeat("─", m.width-2)+"┘") + "\n")

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func clipString(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= maxW {
		return s
	}
	if maxW == 1 {
		return "…"
	}

	return runewidth.Truncate(s, maxW-1, "") + "…"
}

func clipText(text string, maxW, maxH int) string {
	if maxW <= 0 || maxH <= 0 {
		return ""
	}

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) > maxH {
		lines = lines[:maxH]
	}

	for i, line := range lines {
		if runewidth.StringWidth(line) > maxW {
			lines[i] = runewidth.Truncate(line, maxW, "")
		}
	}

	return strings.Join(lines, "\n")
}

func attachToSession(tcpClient SshClient, tsshdPath, sessionName string) (*strings.Builder, error) {
	listOutput, err := execTsshdCommand(tcpClient, tsshdPath, " --list")
	if err != nil {
		return nil, err
	}
	if len(listOutput) == 0 {
		return nil, fmt.Errorf("tsshd list output is empty")
	}
	if bytes.HasPrefix(listOutput, []byte("\a{\"")) {
		return nil, errAttachTsshdTooOld
	}

	var items []tsshd.ServerItem
	if err := json.Unmarshal(listOutput, &items); err != nil {
		return nil, fmt.Errorf("tsshd list failed: %s", string(listOutput))
	}

	infos := make([]*tsshd.BaseInfo, len(items))
	for i, item := range items {
		var info tsshd.BaseInfo
		if err := json.Unmarshal([]byte(item.Info), &info); err == nil {
			infos[i] = &info
		}
	}

	if sessionName != "" {
		for i, info := range infos {
			if info != nil && info.Name == sessionName {
				return getAttachCommand(tsshdPath, items[i].Pid, info), nil
			}
		}
		for i, info := range infos {
			if info == nil {
				warning("get info of tsshd process [%d] failed: %v", items[i].Pid, items[i].Info)
			}
		}
		return nil, nil
	}

	if !isTerminal {
		return nil, fmt.Errorf("not a terminal")
	}

	model := &attachModel{
		tsshd:  tsshdPath,
		client: tcpClient,
		items:  items,
		infos:  infos,
	}
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		return nil, fmt.Errorf("%w: %v", errAttachSessionSelection, err)
	}

	if model.chosen < 0 {
		return nil, fmt.Errorf("%w: canceled by user", errAttachSessionSelection)
	}
	if model.chosen == 0 {
		return nil, nil
	}
	idx := model.chosen - 1
	return getAttachCommand(tsshdPath, items[idx].Pid, infos[idx]), nil
}

func getAttachCommand(tsshdPath string, pid int, info *tsshd.BaseInfo) *strings.Builder {
	if info != nil && len(info.Sessions) > 0 {
		udpAttachSessionID = info.Sessions[0].ID
	}
	var buf strings.Builder
	buf.WriteString(tsshdPath)
	buf.WriteString(" --attach ")
	buf.WriteString(strconv.Itoa(pid))
	return &buf
}

func execTsshdCommand(client SshClient, tsshdPath, tsshdCmd string) ([]byte, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("new session for tsshd command failed: %v", err)
	}
	defer func() { _ = session.Close() }()

	cmd := tsshdPath + tsshdCmd
	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return nil, fmt.Errorf("session exec command [%s] failed: %v", cmd, err)
	}

	return bytes.TrimSpace(output), nil
}
