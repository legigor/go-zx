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
		"Disassembly (window)",
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
	if !strings.Contains(view, "go-zx TUI prototype") {
		t.Fatalf("expected compact header, got:\n%s", view)
	}
}
