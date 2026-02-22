package disasm

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func repoPath(parts ...string) string {
	_, file, _, _ := runtime.Caller(0)
	base := filepath.Dir(file)
	all := append([]string{base, ".."}, parts...)
	return filepath.Join(all...)
}

func mustDisassembler(t *testing.T) *Disassembler {
	t.Helper()
	d, err := NewFromFile(repoPath("assets", "opcode-table.json"))
	if err != nil {
		t.Fatalf("build disassembler: %v", err)
	}
	return d
}

func TestDecode_IndividualOpcodes(t *testing.T) {
	d := mustDisassembler(t)

	tests := []struct {
		name            string
		pc              int
		code            []byte
		expect          string
		expectLn        int
		unknown         bool
		commentContains string
	}{
		{name: "NOP", code: []byte{0x00}, expect: "NOP", expectLn: 1, commentContains: "No operation is performed."},
		{name: "LD BC,nn", code: []byte{0x01, 0x34, 0x12}, expect: "LD BC,$1234", expectLn: 3},
		{name: "LD B,C", code: []byte{0x41}, expect: "LD B,C", expectLn: 1},
		{name: "HALT", code: []byte{0x76}, expect: "HALT", expectLn: 1},
		{name: "EX AF,AF'", code: []byte{0x08}, expect: "EX AF,AF'", expectLn: 1, commentContains: "Exchanges the 16-bit contents of AF and AF'."},
		{name: "JR d target", pc: 0x0000, code: []byte{0x18, 0xFE}, expect: "JR $0000", expectLn: 2},
		{name: "JR C,d target", pc: 0x0000, code: []byte{0x38, 0xFE}, expect: "JR C,$0000", expectLn: 2},
		{name: "IX displacement", code: []byte{0xDD, 0x36, 0xFE, 0x42}, expect: "LD (IX-$02),$42", expectLn: 4},
		{name: "BIT 7,(HL)", code: []byte{0xCB, 0x7E}, expect: "BIT 7,(HL)", expectLn: 2},
		{name: "BIT 0,(IX+d)", code: []byte{0xDD, 0xCB, 0x05, 0x46}, expect: "BIT 0,(IX+$05)", expectLn: 4},
		{name: "RST", code: []byte{0xFF}, expect: "RST $38", expectLn: 1},
		{name: "Unknown fallback", code: []byte{0xDD}, expect: "DB $DD", expectLn: 1, unknown: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := d.DecodeAt(tt.code, tt.pc)
			if line.Mnemonic != tt.expect {
				t.Fatalf("mnemonic mismatch: got %q want %q", line.Mnemonic, tt.expect)
			}
			if len(line.Bytes) != tt.expectLn {
				t.Fatalf("length mismatch: got %d want %d", len(line.Bytes), tt.expectLn)
			}
			if line.Unknown != tt.unknown {
				t.Fatalf("unknown mismatch: got %v want %v", line.Unknown, tt.unknown)
			}
			if tt.commentContains != "" && !strings.Contains(line.Comment, tt.commentContains) {
				t.Fatalf("comment mismatch: got %q does not contain %q", line.Comment, tt.commentContains)
			}
			if tt.commentContains != "" {
				formatted := FormatLine(line)
				if !strings.Contains(formatted, " ; "+tt.commentContains) {
					t.Fatalf("formatted line missing comment: %q", formatted)
				}
			}
		})
	}
}

func TestFormatLine_CommentAligned(t *testing.T) {
	short := Line{
		Address:  0x0000,
		Bytes:    []byte{0x00},
		Mnemonic: "NOP",
		Comment:  "No operation is performed.",
	}
	long := Line{
		Address:  0x0001,
		Bytes:    []byte{0xDD, 0x36, 0xFE, 0x42},
		Mnemonic: "LD (IX-$02),$42",
		Comment:  "Loads $42 into memory pointed by IX-2.",
	}

	shortFmt := FormatLine(short)
	longFmt := FormatLine(long)

	shortIdx := strings.Index(shortFmt, ";")
	longIdx := strings.Index(longFmt, ";")
	if shortIdx == -1 || longIdx == -1 {
		t.Fatalf("missing comment delimiter: short=%q long=%q", shortFmt, longFmt)
	}
	if shortIdx != longIdx {
		t.Fatalf("comment alignment mismatch: short=%d long=%d", shortIdx, longIdx)
	}
}

func TestReachableCodeHasNoUnknownOpcodes(t *testing.T) {
	d := mustDisassembler(t)
	rom, err := os.ReadFile(repoPath("assets", "roms", "48.rom"))
	if err != nil {
		t.Fatalf("read rom: %v", err)
	}

	entries := []int{0x0000, 0x0008, 0x0010, 0x0018, 0x0020, 0x0028, 0x0030, 0x0038, 0x0066}
	visited := map[int]bool{}
	queue := append([]int(nil), entries...)
	unknown := map[int]Line{}
	dynamicBranches := map[int]string{}

	for len(queue) > 0 {
		pc := queue[0]
		queue = queue[1:]
		if pc < 0 || pc >= len(rom) || visited[pc] {
			continue
		}
		visited[pc] = true

		line := d.DecodeAt(rom, pc)
		if len(line.Bytes) == 0 {
			continue
		}
		if line.Unknown {
			unknown[pc] = line
			continue
		}

		succ, dynamic := controlFlowSuccessors(pc, line)
		if dynamic {
			dynamicBranches[pc] = line.Mnemonic
		}
		for _, next := range succ {
			if next >= 0 && next < len(rom) && !visited[next] {
				queue = append(queue, next)
			}
		}
	}

	if len(visited) < 1024 {
		t.Fatalf("reachable graph unexpectedly small: visited=%d", len(visited))
	}

	if len(unknown) > 0 {
		addrs := sortedKeys(unknown)
		details := make([]string, 0, len(addrs))
		for _, addr := range addrs {
			details = append(details, FormatLine(unknown[addr]))
		}
		t.Fatalf("unknown opcode(s) in reachable code path (%d):\n%s", len(unknown), strings.Join(details, "\n"))
	}

	if len(dynamicBranches) > 0 {
		addrs := sortedMnemonicKeys(dynamicBranches)
		t.Logf("dynamic branches observed (need runtime execution to fully validate): %v", addrs)
	}
}

func TestDisassembleROM_E2E(t *testing.T) {
	d := mustDisassembler(t)
	rom, err := os.ReadFile(repoPath("assets", "roms", "48.rom"))
	if err != nil {
		t.Fatalf("read rom: %v", err)
	}

	lines := d.Disassemble(rom)
	if len(lines) == 0 {
		t.Fatal("expected disassembly lines, got none")
	}

	consumed := 0
	unknown := 0
	for _, line := range lines {
		consumed += len(line.Bytes)
		if line.Unknown {
			unknown++
		}
	}

	if consumed != len(rom) {
		t.Fatalf("rom not fully consumed: got %d want %d", consumed, len(rom))
	}
	if unknown > 8 {
		t.Fatalf("too many unknown opcodes for ROM: got %d", unknown)
	}

	if lines[0].Mnemonic != "DI" {
		t.Fatalf("line 0 mismatch: got %q", lines[0].Mnemonic)
	}
	if lines[1].Mnemonic != "XOR A" {
		t.Fatalf("line 1 mismatch: got %q", lines[1].Mnemonic)
	}
	if lines[2].Mnemonic != "LD DE,$FFFF" {
		t.Fatalf("line 2 mismatch: got %q", lines[2].Mnemonic)
	}
	if lines[3].Mnemonic != "JP $11CB" {
		t.Fatalf("line 3 mismatch: got %q", lines[3].Mnemonic)
	}
}

var targetRegex = regexp.MustCompile(`\$([0-9A-F]{2,4})`)

func controlFlowSuccessors(pc int, line Line) ([]int, bool) {
	m := line.Mnemonic
	next := pc + len(line.Bytes)

	switch {
	case m == "HALT":
		return nil, false
	case m == "RET" || m == "RETI" || m == "RETN":
		return nil, false
	case strings.HasPrefix(m, "RET "):
		return []int{next}, false
	case strings.HasPrefix(m, "JP "):
		if strings.HasPrefix(m, "JP (") {
			return nil, true
		}
		target, ok := extractTargetAddress(m)
		if !ok {
			return []int{next}, false
		}
		if strings.Contains(strings.TrimPrefix(m, "JP "), ",") {
			return uniqueInts(next, target), false
		}
		return []int{target}, false
	case strings.HasPrefix(m, "JR "):
		target, ok := extractTargetAddress(m)
		if !ok {
			return []int{next}, false
		}
		if strings.Contains(strings.TrimPrefix(m, "JR "), ",") {
			return uniqueInts(next, target), false
		}
		return []int{target}, false
	case strings.HasPrefix(m, "DJNZ "):
		target, ok := extractTargetAddress(m)
		if !ok {
			return []int{next}, false
		}
		return uniqueInts(next, target), false
	case strings.HasPrefix(m, "CALL "):
		target, ok := extractTargetAddress(m)
		if !ok {
			return []int{next}, false
		}
		return uniqueInts(next, target), false
	case strings.HasPrefix(m, "RST "):
		target, ok := extractTargetAddress(m)
		if !ok {
			return []int{next}, false
		}
		return uniqueInts(next, target), false
	default:
		return []int{next}, false
	}
}

func extractTargetAddress(mnemonic string) (int, bool) {
	match := targetRegex.FindStringSubmatch(mnemonic)
	if len(match) != 2 {
		return 0, false
	}
	value, err := strconv.ParseInt(match[1], 16, 32)
	if err != nil {
		return 0, false
	}
	return int(value), true
}

func uniqueInts(values ...int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func sortedKeys(m map[int]Line) []int {
	keys := make([]int, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	return keys
}

func sortedMnemonicKeys(m map[int]string) []string {
	keys := make([]int, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, strconv.FormatInt(int64(key), 16)+":"+m[key])
	}
	return out
}
