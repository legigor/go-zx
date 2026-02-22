package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestViewContainsCorePanels(t *testing.T) {
	m := initialModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	view := updated.(model).View()

	mustContain := []string{
		"CPU / Registers",
		"Instruction",
		"Flags / Clock",
		"Disassembly (from PC)",
		"[s] step",
	}

	for _, want := range mustContain {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q\n%s", want, view)
		}
	}
}

func TestCompactViewWhenNarrow(t *testing.T) {
	m := initialModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 24})
	view := updated.(model).View()
	if !strings.Contains(view, "go-zx live") {
		t.Fatalf("expected compact header, got:\n%s", view)
	}
}

func TestStepKeyExecutesRealInstruction(t *testing.T) {
	m := initialModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	next := updated.(model)

	if next.lastErr != "" {
		t.Fatalf("unexpected error: %v", next.lastErr)
	}
	if next.emu.StepCount != 1 {
		t.Fatalf("step count mismatch: got=%d want=1", next.emu.StepCount)
	}
	if next.lastStep.Mnemonic != "DI" {
		t.Fatalf("mnemonic mismatch: got=%q want=DI", next.lastStep.Mnemonic)
	}
}
