package disasm

import (
	"os"
	"sort"
	"strings"
	"testing"
)

func TestTraceControlFlow_SyntheticCallReturn(t *testing.T) {
	d := mustDisassembler(t)

	program := []byte{
		0xCD, 0x05, 0x00, // CALL $0005
		0x00,       // NOP
		0x76,       // HALT
		0x3E, 0x01, // LD A,$01
		0xC9, // RET
	}

	result := d.TraceControlFlow(program, TraceConfig{EntryPCs: []int{0x0000}, MaxStates: 1000})
	if result.LimitReached {
		t.Fatal("trace unexpectedly hit state limit")
	}
	if len(result.Unknown) != 0 {
		t.Fatalf("expected zero unknown instructions, got: %+v", result.Unknown)
	}
	if len(result.Dynamic) != 0 {
		t.Fatalf("expected zero dynamic issues, got: %+v", result.Dynamic)
	}

	expected := []int{0x0000, 0x0003, 0x0004, 0x0005, 0x0007}
	visited := make([]int, 0, len(result.Visited))
	for pc := range result.Visited {
		visited = append(visited, pc)
	}
	sort.Ints(visited)
	if len(visited) != len(expected) {
		t.Fatalf("visited mismatch: got=%v want=%v", visited, expected)
	}
	for i := range expected {
		if visited[i] != expected[i] {
			t.Fatalf("visited mismatch: got=%v want=%v", visited, expected)
		}
	}
}

func TestTraceControlFlow_ROM_NoUnknownOnTracedPaths(t *testing.T) {
	d := mustDisassembler(t)
	rom, err := os.ReadFile(repoPath("assets", "roms", "48.rom"))
	if err != nil {
		t.Fatalf("read rom: %v", err)
	}

	result := d.TraceControlFlow(rom, TraceConfig{
		EntryPCs:      []int{0x0000, 0x0008, 0x0010, 0x0018, 0x0020, 0x0028, 0x0030, 0x0038, 0x0066},
		MaxStates:     250000,
		MaxStackDepth: 64,
	})

	if result.LimitReached {
		t.Fatalf("trace hit state limit: explored=%d", result.StatesExplored)
	}
	if result.VisitedCount() < 600 {
		t.Fatalf("trace coverage too small: visited=%d", result.VisitedCount())
	}
	if len(result.Unknown) > 0 {
		lines := make([]string, 0, len(result.Unknown))
		for _, issue := range result.Unknown {
			lines = append(lines, issue.Mnemonic)
		}
		t.Fatalf("unknown opcode(s) on traced paths (%d): %s", len(result.Unknown), strings.Join(lines, ", "))
	}

	if len(result.Dynamic) == 0 {
		t.Fatal("expected some dynamic control-flow issues (e.g. JP (HL)/(IX)), got none")
	}
}
