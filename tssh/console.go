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
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync/atomic"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var wantExit atomic.Bool

type menuItem struct {
	key    string
	label  string
	action func() (tea.Model, tea.Cmd)
}

type menuModel struct {
	items           []*menuItem
	cursor          int
	menuWidth       int
	screenWidth     int
	quitting        bool
	backgroundStyle lipgloss.Style
	titleStyle      lipgloss.Style
	footerStyle     lipgloss.Style
	blankLineStyle  lipgloss.Style
	separatorStyle  lipgloss.Style
	activeItemStyle lipgloss.Style
	normalItemStyle lipgloss.Style
	activeBarStyle  lipgloss.Style
}

func initMenuModel(menuWidth, screenWidth int) *menuModel {
	ncursesBg := lipgloss.Color("15")  // white
	ncursesFg := lipgloss.Color("0")   // black
	ncursesBlue := lipgloss.Color("4") // dark blue
	ncursesGrey := lipgloss.Color("8") // grey
	return &menuModel{
		cursor:          0,
		menuWidth:       menuWidth,
		screenWidth:     screenWidth,
		backgroundStyle: lipgloss.NewStyle().Background(ncursesBg).Width(screenWidth).Align(lipgloss.Center),
		titleStyle:      lipgloss.NewStyle().Background(ncursesBlue).Foreground(lipgloss.Color("15")).Bold(true).Width(menuWidth).Align(lipgloss.Center),
		footerStyle:     lipgloss.NewStyle().Foreground(ncursesGrey).Background(ncursesBg).Width(menuWidth).Align(lipgloss.Center),
		blankLineStyle:  lipgloss.NewStyle().Background(ncursesBg).Width(menuWidth),
		separatorStyle:  lipgloss.NewStyle().Foreground(ncursesGrey).Background(ncursesBg).Width(menuWidth),
		activeItemStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(ncursesBlue),
		normalItemStyle: lipgloss.NewStyle().Foreground(ncursesFg).Background(ncursesBg),
		activeBarStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(ncursesBlue),
	}
}

func (m *menuModel) Init() tea.Cmd {
	return nil
}

func (m *menuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch s := msg.String(); s {
		case "ctrl+c", "esc", "q":
			m.quitting = true
			return m, tea.Quit
		case "up", "k", "left":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j", "right":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor >= 0 && m.cursor < len(m.items) {
				return m.items[m.cursor].action()
			}
			return m, nil
		default:
			for i, item := range m.items {
				if s == item.key {
					m.cursor = i
					return item.action()
				}
			}
		}
	}
	return m, nil
}

func (m *menuModel) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	var builder strings.Builder

	// Full screen background
	bgLine := func(s string) string {
		w := ansi.StringWidth(s)
		if w < m.screenWidth {
			s += strings.Repeat(" ", m.screenWidth-w)
		}
		const resetSeq = "\x1b[0m"
		const reApply = "\x1b[0m\x1b[47m\x1b[30m"
		s = strings.ReplaceAll(s, resetSeq, reApply)
		return "\x1b[47m\x1b[30m" + s + resetSeq
	}

	// top border
	builder.WriteString(bgLine("┌"+strings.Repeat("─", m.menuWidth)+"┐") + "\n")

	// title bar (blue bg)
	title := getText("console/title")
	p := m.menuWidth - ansi.StringWidth(title)
	builder.WriteString(bgLine(m.titleStyle.Render(title+repeatSafe(p))) + "\n")

	// separator
	builder.WriteString(bgLine("├"+strings.Repeat("─", m.menuWidth)+"┤") + "\n")

	// menu items with ncurses-style cursor
	for i, item := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = "▐█"
		}
		label := cursor + " " + item.label
		builder.WriteString(bgLine(m.normalItemStyle.Render(label)) + "\n")
	}

	// separator
	builder.WriteString(bgLine("├"+strings.Repeat("─", m.menuWidth)+"┤") + "\n")

	// footer
	footer := getText("console/notes")
	builder.WriteString(bgLine(m.footerStyle.Render(footer+repeatSafe(m.menuWidth-ansi.StringWidth(footer)))) + "\n")

	// bottom border
	builder.WriteString(bgLine("└"+strings.Repeat("─", m.menuWidth)+"┘") + "\n")

	return tea.NewView(builder.String())
}

func runConsole(escapeChar byte, writer io.WriteCloser, sshConn *sshConnection) {
	width := sshConn.session.GetTerminalWidth()
	model := initMenuModel(min(width, 60), width)

	var key, char string
	if escapeChar <= 26 {
		key = "ctrl+" + string([]byte{'a' - 1 + escapeChar})
		char = "^" + string([]byte{'A' - 1 + escapeChar})
	} else {
		key = string(escapeChar)
		char = string(escapeChar)
	}
	model.items = []*menuItem{
		{key, strings.ReplaceAll(getText("console/send_char"), "{0}", char), func() (tea.Model, tea.Cmd) {
			_, _ = writer.Write([]byte{escapeChar})
			model.quitting = true
			return model, tea.Quit
		}},
	}

	if runtime.GOOS != "windows" {
		var suspend bool
		defer func() {
			if suspend {
				go suspendProcess()
			}
		}()
		model.items = append(model.items, &menuItem{"ctrl+z", getText("console/suspend"), func() (tea.Model, tea.Cmd) {
			suspend = true
			model.quitting = true
			return model, tea.Quit
		}})
	}

	quitted := make(chan struct{})
	defer close(quitted)
	var exiting atomic.Bool
	model.items = append(model.items, &menuItem{".", getText("console/terminate"), func() (tea.Model, tea.Cmd) {
		exiting.Store(true)
		go func() {
			<-quitted
			wantExit.Store(true)
			sshConn.forceExit(kExitCodeConsoleKill, fmt.Sprintf("user action in the console or entering the escape sequences ( %s. )", char))
		}()
		model.quitting = true
		return model, tea.Quit
	}})

	if sshConn.param.args.Attach || strings.ToLower(getExOptionConfig(sshConn.param.args, "UdpSessionAttach")) == "yes" {
		model.items = append(model.items, &menuItem{"d", getText("console/detach"), func() (tea.Model, tea.Cmd) {
			exiting.Store(true)
			go func() {
				<-quitted
				sshConn.forceExit(kExitCodeUdpDetach, fmt.Sprintf("user action in the console or entering the escape sequence ( %sd )", char))
			}()
			model.quitting = true
			return model, tea.Quit
		}})
	}

	teaOpts, cancelReader := newTeaOptions(func(buf []byte) {
		if enableDebugLogging {
			if ch := stdinInputChan.Load(); ch != nil {
				*ch <- append([]byte(nil), buf...)
				return
			}
		}
		_, _ = writer.Write(buf)
	})
	defer cancelReader()

	p := tea.NewProgram(model, append(teaOpts, tea.WithOutput(os.Stderr))...)
	if _, err := p.Run(); err != nil {
		warning("run escape console failed: %v", err)
	}

	if !exiting.Load() {
		_ = sshConn.session.RedrawScreen(true)
	}
}
