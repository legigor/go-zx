package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"go-zx/disasm"
	"go-zx/emu"
)

const tickInterval = 20 * time.Millisecond

type tickMsg time.Time

type model struct {
	width  int
	height int

	running bool
	status  string

	machine *emu.Machine48K
	emu     *emu.Emulator
	dis     *disasm.Disassembler
	rom     []byte

	lastStep    emu.StepResult
	lastComment string
	lastErr     string
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
	rom := make([]byte, 0x4000)
	copy(rom, []byte{0xF3, 0xAF, 0x11, 0xFF, 0xFF, 0xC3, 0xCB, 0x11})
	rom[0x11CB] = 0x76 // HALT

	m, err := newModelFromROM(rom, nil)
	if err != nil {
		return model{status: "error", lastErr: err.Error()}
	}
	return m
}

func newModelFromROM(rom []byte, d *disasm.Disassembler) (model, error) {
	machine, err := emu.NewMachine48K(rom)
	if err != nil {
		return model{}, err
	}

	e := emu.New(machine)
	m := model{
		status:  "paused",
		machine: machine,
		emu:     e,
		dis:     d,
		rom:     append([]byte(nil), rom...),
	}
	m.seedLastStepFromPC()
	return m, nil
}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m *model) seedLastStepFromPC() {
	if m.emu == nil {
		return
	}
	pc := int(m.emu.Reg.PC)
	if m.dis != nil && pc >= 0 && pc < len(m.rom) {
		line := m.dis.DecodeAt(m.rom, pc)
		m.lastStep = emu.StepResult{
			PCBefore: uint16(line.Address),
			PCAfter:  uint16(line.Address + len(line.Bytes)),
			Bytes:    append([]byte(nil), line.Bytes...),
			Mnemonic: line.Mnemonic,
			TStates:  0,
		}
		m.lastComment = line.Comment
		return
	}

	opcode := m.machine.Read(m.emu.Reg.PC)
	m.lastStep = emu.StepResult{
		PCBefore: m.emu.Reg.PC,
		PCAfter:  m.emu.Reg.PC + 1,
		Bytes:    []byte{opcode},
		Mnemonic: fmt.Sprintf("DB $%02X", opcode),
		TStates:  0,
	}
	m.lastComment = ""
}

func (m *model) commentForPC(pc uint16) string {
	if m.dis == nil {
		return ""
	}
	if int(pc) >= len(m.rom) {
		return ""
	}
	return m.dis.DecodeAt(m.rom, int(pc)).Comment
}

func (m *model) stepOnce() {
	if m.emu == nil {
		return
	}
	step, err := m.emu.Step()
	m.lastStep = step
	m.lastComment = m.commentForPC(step.PCBefore)
	if err != nil {
		m.running = false
		m.status = "error"
		m.lastErr = err.Error()
		return
	}
	m.lastErr = ""
}

func (m *model) runOneFrame() {
	used := 0
	for used < emu.TStatesPerFrame {
		step, err := m.emu.Step()
		m.lastStep = step
		m.lastComment = m.commentForPC(step.PCBefore)
		if err != nil {
			m.running = false
			m.status = "error"
			m.lastErr = err.Error()
			return
		}
		used += step.TStates
		if m.emu.Halted {
			break
		}
	}
	m.lastErr = ""
}

func (m *model) resetRuntime() {
	reset, err := newModelFromROM(m.rom, m.dis)
	if err != nil {
		m.running = false
		m.status = "error"
		m.lastErr = err.Error()
		return
	}
	reset.width = m.width
	reset.height = m.height
	*m = reset
	m.status = "reset"
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
			if m.status == "error" {
				return m, nil
			}
			m.running = true
			m.status = "running"
			return m, tickCmd()
		case "p":
			m.running = false
			m.status = "paused"
			return m, nil
		case "r":
			m.resetRuntime()
			return m, nil
		}
	case tickMsg:
		if m.running {
			m.runOneFrame()
			if m.running {
				return m, tickCmd()
			}
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

	title := titleStyle.Render("go-zx • ZX Spectrum 48K • live emulator")
	status := statusStyle(m.status).Render(strings.ToUpper(m.status))
	headerLine := joinWithRight(title, "STATUS: "+status, contentWidth-4)
	statsLine := mutedStyle.Render(fmt.Sprintf(
		"STEP %08d   FRAME %05d   T-STATES %09d   FRAME BUDGET %05d/%05d",
		m.emu.StepCount,
		int(m.emu.TotalTStates)/emu.TStatesPerFrame,
		m.emu.TotalTStates,
		int(m.emu.TotalTStates)%emu.TStatesPerFrame,
		emu.TStatesPerFrame,
	))
	header := headerStyle.Width(contentWidth).Render(headerLine + "\n" + statsLine)

	topLeft := panelStyle.Width(leftWidth).Render(m.renderCPUPanel())
	topRight := panelStyle.Width(rightWidth).Render(m.renderInstructionPanel())
	bottomLeft := panelStyle.Width(leftWidth).Render(m.renderFlagsPanel())
	bottomRight := panelStyle.Width(rightWidth).Render(m.renderDisasmPanel())

	row1 := lipgloss.JoinHorizontal(lipgloss.Top, topLeft, strings.Repeat(" ", gap), topRight)
	row2 := lipgloss.JoinHorizontal(lipgloss.Top, bottomLeft, strings.Repeat(" ", gap), bottomRight)

	footerText := "[s] step   [c] continue(frame)   [p] pause   [r] reset   [q] quit"
	if m.lastErr != "" {
		footerText += "\nerror: " + m.lastErr
	}
	footer := helpStyle.Width(contentWidth).Render(footerText)

	return lipgloss.JoinVertical(lipgloss.Left, header, row1, row2, footer)
}

func (m model) compactView(width int) string {
	header := fmt.Sprintf("go-zx live  |  STATUS: %s", strings.ToUpper(m.status))
	line1 := fmt.Sprintf("STEP %d  PC %04X  LAST %s", m.emu.StepCount, m.emu.Reg.PC, m.lastStep.Mnemonic)
	line2 := fmt.Sprintf("T %d  FRAME %d (%d/%d)", m.emu.TotalTStates, int(m.emu.TotalTStates)/emu.TStatesPerFrame, int(m.emu.TotalTStates)%emu.TStatesPerFrame, emu.TStatesPerFrame)
	line3 := "[s]step [c]cont [p]pause [r]reset [q]quit"

	rows := []string{header, line1, line2, line3}
	if m.lastErr != "" {
		rows = append(rows, "error: "+m.lastErr)
	}

	style := panelStyle.Copy().Width(max(40, width-2))
	return style.Render(strings.Join(rows, "\n"))
}

func (m model) renderCPUPanel() string {
	r := m.emu.Reg
	return strings.Join([]string{
		titleStyle.Render("CPU / Registers"),
		fmt.Sprintf("PC  %04X    SP  %04X", r.PC, r.SP),
		fmt.Sprintf("IX  %04X    IY  %04X", r.IX, r.IY),
		"",
		fmt.Sprintf("AF  %02X%02X    BC  %02X%02X", r.A, r.F, r.B, r.C),
		fmt.Sprintf("DE  %02X%02X    HL  %02X%02X", r.D, r.E, r.H, r.L),
		fmt.Sprintf("I   %02X      R   %02X", r.I, r.R),
		fmt.Sprintf("IFF1 %t  IFF2 %t  IM %d  HALT %t", m.emu.IFF1, m.emu.IFF2, m.emu.IM, m.emu.Halted),
	}, "\n")
}

func (m model) renderInstructionPanel() string {
	comment := m.lastComment
	if comment == "" {
		comment = "-"
	}
	return strings.Join([]string{
		titleStyle.Render("Instruction"),
		fmt.Sprintf("%04X: %-11s %s", m.lastStep.PCBefore, bytesToHex(m.lastStep.Bytes), fallbackText(m.lastStep.Mnemonic, "-")),
		fmt.Sprintf("Comment: %s", comment),
		fmt.Sprintf("Cycles: +%d", m.lastStep.TStates),
		"",
		"Bus this step: (instrumentation TBD)",
		"Last memory read:  [pending]",
		"Last memory write: [pending]",
	}, "\n")
}

func (m model) renderFlagsPanel() string {
	f := m.emu.Reg.F
	flags := fmt.Sprintf("[S Z H P/V N C] = [%d %d %d  %d  %d %d]",
		flag(f, 7), flag(f, 6), flag(f, 4), flag(f, 2), flag(f, 1), flag(f, 0),
	)

	return strings.Join([]string{
		titleStyle.Render("Flags / Clock"),
		flags,
		"",
		fmt.Sprintf("T-states total: %d", m.emu.TotalTStates),
		fmt.Sprintf("Frame budget:   %d / %d", int(m.emu.TotalTStates)%emu.TStatesPerFrame, emu.TStatesPerFrame),
		fmt.Sprintf("Frame number:   %d", int(m.emu.TotalTStates)/emu.TStatesPerFrame),
	}, "\n")
}

func (m model) renderDisasmPanel() string {
	lines := []string{titleStyle.Render("Disassembly (from PC)")}
	pc := int(m.emu.Reg.PC)

	if m.dis == nil || pc >= len(m.rom) {
		for i := 0; i < 8; i++ {
			addr := m.emu.Reg.PC + uint16(i)
			lines = append(lines, fmt.Sprintf("%s %04X: %02X", markerFor(i), addr, m.machine.Read(addr)))
		}
		return strings.Join(lines, "\n")
	}

	for i := 0; i < 8 && pc < len(m.rom); i++ {
		line := m.dis.DecodeAt(m.rom, pc)
		if len(line.Bytes) == 0 {
			break
		}
		lines = append(lines, fmt.Sprintf("%s %04X: %-11s %s", markerFor(i), line.Address, bytesToHex(line.Bytes), line.Mnemonic))
		pc += len(line.Bytes)
	}

	return strings.Join(lines, "\n")
}

func markerFor(i int) string {
	if i == 0 {
		return "▶"
	}
	return " "
}

func bytesToHex(bytes []byte) string {
	if len(bytes) == 0 {
		return ""
	}
	parts := make([]string, len(bytes))
	for i, b := range bytes {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, " ")
}

func fallbackText(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
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
	case "error":
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
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

func RunLive(romPath, opcodeTablePath string) error {
	rom, err := os.ReadFile(romPath)
	if err != nil {
		return err
	}

	d, err := disasm.NewFromFile(opcodeTablePath)
	if err != nil {
		return err
	}

	m, err := newModelFromROM(rom, d)
	if err != nil {
		return err
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// RunPrototype remains for quick smoke testing with an embedded mini ROM.
func RunPrototype() error {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
