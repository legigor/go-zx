package disasm

import (
	"os"
	"path/filepath"
	"runtime"
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
