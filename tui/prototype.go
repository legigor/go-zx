package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const tstatesPerFrame = 69888

type tickMsg time.Time

type disasmLine struct {
	addr     uint16
	bytes    string
	mnemonic string
	comment  string
	cycles   int
}

type model struct {
	width  int
	height int

	running bool
	status  string

	step        int
	frame       int
	tstates     int
	frameBudget int

	pc uint16
	sp uint16
	ix uint16
	iy uint16

	a byte
	f byte
	b byte
	c byte
	d byte
	e byte
	h byte
	l byte

	last disasmLine

	program []disasmLine
	cursor  int
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	mutedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	panelStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1).
			Foreground(lipgloss.Color("252"))
)

func initialModel() model {
	program := []disasmLine{
		{addr: 0x0000, bytes: "F3", mnemonic: "DI", comment: "Disable maskable interrupts.", cycles: 4},
		{addr: 0x0001, bytes: "AF", mnemonic: "XOR A", comment: "Clear A and reset carry.", cycles: 4},
		{addr: 0x0002, bytes: "11 FF FF", mnemonic: "LD DE,$FFFF", comment: "Load immediate into DE.", cycles: 10},
		{addr: 0x0005, bytes: "C3 CB 11", mnemonic: "JP $11CB", comment: "Jump to ROM routine.", cycles: 10},
		{addr: 0x11CB, bytes: "3E 01", mnemonic: "LD A,$01", comment: "Load immediate into A.", cycles: 7},
		{addr: 0x11CD, bytes: "32 00 40", mnemonic: "LD ($4000),A", comment: "Write A to screen memory.", cycles: 13},
		{addr: 0x11D0, bytes: "21 00 40", mnemonic: "LD HL,$4000", comment: "Point HL to VRAM.", cycles: 10},
		{addr: 0x11D3, bytes: "77", mnemonic: "LD (HL),A", comment: "Store A at (HL).", cycles: 7},
		{addr: 0x11D4, bytes: "23", mnemonic: "INC HL", comment: "Increment HL.", cycles: 6},
		{addr: 0x11D5, bytes: "18 FC", mnemonic: "JR $11D3", comment: "Loop back.", cycles: 12},
	}

	return model{
		status:  "paused",
		pc:      program[0].addr,
		sp:      0x5C3A,
		ix:      0x0000,
		iy:      0x5C3A,
		program: program,
		last:    program[0],
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m *model) stepOnce() {
	if len(m.program) == 0 {
		return
	}

	line := m.program[m.cursor]
	m.last = line
	m.step++
	m.pc = line.addr

	m.tstates += line.cycles
	m.frameBudget += line.cycles
	for m.frameBudget >= tstatesPerFrame {
		m.frameBudget -= tstatesPerFrame
		m.frame++
	}

	m.a += 1
	m.b -= 1
	m.c += 2
	m.d ^= 0x01
	m.e += 1
	m.h = byte((int(m.h) + 0x10) & 0xFF)
	m.l = byte((int(m.l) + 0x01) & 0xFF)
	m.f ^= 0x44

	m.cursor = (m.cursor + 1) % len(m.program)
	m.pc = m.program[m.cursor].addr
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "s":
			m.running = false
			m.status = "single-step"
			m.stepOnce()
			return m, nil
		case "c":
			m.running = true
			m.status = "running"
			return m, tickCmd()
		case "p":
			m.running = false
			m.status = "paused"
			return m, nil
		case "r":
			reset := initialModel()
			reset.width = m.width
			reset.height = m.height
			reset.status = "reset"
			return reset, nil
		}
	case tickMsg:
		if m.running {
			m.stepOnce()
			return m, tickCmd()
		}
	}

	return m, nil
}

func (m model) View() string {
	width := m.width
	if width == 0 {
		width = 120
	}
	if width < 90 {
		return m.compactView(width)
	}

	contentWidth := width - 2
	gap := 1
	leftWidth := (contentWidth - gap) / 2
	rightWidth := contentWidth - gap - leftWidth

	title := titleStyle.Render("go-zx • ZX Spectrum 48K • TUI prototype")
	status := statusStyle(m.status).Render(strings.ToUpper(m.status))
	headerLine := joinWithRight(title, "STATUS: "+status, contentWidth-4)
	statsLine := mutedStyle.Render(fmt.Sprintf("STEP %08d   FRAME %05d   T-STATES %09d   FRAME BUDGET %05d/%05d", m.step, m.frame, m.tstates, m.frameBudget, tstatesPerFrame))
	header := headerStyle.Width(contentWidth).Render(headerLine + "\n" + statsLine)

	topLeft := panelStyle.Width(leftWidth).Render(m.renderCPUPanel())
	topRight := panelStyle.Width(rightWidth).Render(m.renderInstructionPanel())
	bottomLeft := panelStyle.Width(leftWidth).Render(m.renderFlagsPanel())
	bottomRight := panelStyle.Width(rightWidth).Render(m.renderDisasmPanel())

	row1 := lipgloss.JoinHorizontal(lipgloss.Top, topLeft, strings.Repeat(" ", gap), topRight)
	row2 := lipgloss.JoinHorizontal(lipgloss.Top, bottomLeft, strings.Repeat(" ", gap), bottomRight)

	footer := helpStyle.Width(contentWidth).Render("[s] step   [c] continue   [p] pause   [r] reset   [q] quit")

	return lipgloss.JoinVertical(lipgloss.Left, header, row1, row2, footer)
}

func (m model) compactView(width int) string {
	header := fmt.Sprintf("go-zx TUI prototype  |  STATUS: %s", strings.ToUpper(m.status))
	line1 := fmt.Sprintf("STEP %d  PC %04X  LAST %s", m.step, m.last.addr, m.last.mnemonic)
	line2 := fmt.Sprintf("T %d  FRAME %d (%d/%d)", m.tstates, m.frame, m.frameBudget, tstatesPerFrame)
	line3 := "[s]step [c]cont [p]pause [r]reset [q]quit"

	style := panelStyle.Copy().Width(max(40, width-2))
	return style.Render(strings.Join([]string{header, line1, line2, line3}, "\n"))
}

func (m model) renderCPUPanel() string {
	return strings.Join([]string{
		titleStyle.Render("CPU / Registers"),
		fmt.Sprintf("PC  %04X    SP  %04X", m.pc, m.sp),
		fmt.Sprintf("IX  %04X    IY  %04X", m.ix, m.iy),
		"",
		fmt.Sprintf("AF  %02X%02X    BC  %02X%02X", m.a, m.f, m.b, m.c),
		fmt.Sprintf("DE  %02X%02X    HL  %02X%02X", m.d, m.e, m.h, m.l),
		"",
		"Mode: mock data (not real CPU execution yet)",
	}, "\n")
}

func (m model) renderInstructionPanel() string {
	return strings.Join([]string{
		titleStyle.Render("Instruction"),
		fmt.Sprintf("%04X: %-11s %s", m.last.addr, m.last.bytes, m.last.mnemonic),
		fmt.Sprintf("Comment: %s", m.last.comment),
		fmt.Sprintf("Cycles: +%d", m.last.cycles),
		"",
		"Bus this step: R=2 W=0 IN=0 OUT=0",
		"Last memory read:  [mock]",
		"Last memory write: [mock]",
	}, "\n")
}

func (m model) renderFlagsPanel() string {
	flags := fmt.Sprintf("[S Z H P/V N C] = [%d %d %d  %d  %d %d]",
		flag(m.f, 7), flag(m.f, 6), flag(m.f, 4), flag(m.f, 2), flag(m.f, 1), flag(m.f, 0),
	)

	return strings.Join([]string{
		titleStyle.Render("Flags / Clock"),
		flags,
		"",
		fmt.Sprintf("T-states total: %d", m.tstates),
		fmt.Sprintf("Frame budget:   %d / %d", m.frameBudget, tstatesPerFrame),
		fmt.Sprintf("Frame number:   %d", m.frame),
	}, "\n")
}

func (m model) renderDisasmPanel() string {
	lines := []string{titleStyle.Render("Disassembly (window)")}

	for i := -3; i <= 4; i++ {
		idx := (m.cursor + i + len(m.program)) % len(m.program)
		line := m.program[idx]
		marker := " "
		if i == 0 {
			marker = "▶"
		}
		lines = append(lines, fmt.Sprintf("%s %04X: %-11s %s", marker, line.addr, line.bytes, line.mnemonic))
	}

	return strings.Join(lines, "\n")
}

func statusStyle(status string) lipgloss.Style {
	s := strings.ToLower(status)
	switch s {
	case "running":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	case "paused":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
	case "single-step":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	default:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	}
}

func joinWithRight(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func flag(f byte, bit uint) int {
	if (f & (1 << bit)) != 0 {
		return 1
	}
	return 0
}

func RunPrototype() error {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
