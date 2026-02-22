package emu

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func repoPath(parts ...string) string {
	_, file, _, _ := runtime.Caller(0)
	base := filepath.Dir(file)
	all := append([]string{base, ".."}, parts...)
	return filepath.Join(all...)
}

func blankROM() []byte {
	rom := make([]byte, romSize48K)
	for i := range rom {
		rom[i] = 0x00
	}
	return rom
}

func TestMachine48K_ROMReadOnly(t *testing.T) {
	rom := blankROM()
	rom[0x0001] = 0xAF

	m, err := NewMachine48K(rom)
	if err != nil {
		t.Fatalf("new machine: %v", err)
	}

	if got := m.Read(0x0001); got != 0xAF {
		t.Fatalf("rom read mismatch: got=%02X want=AF", got)
	}

	m.Write(0x0001, 0x12)
	if got := m.Read(0x0001); got != 0xAF {
		t.Fatalf("rom should remain read-only: got=%02X want=AF", got)
	}

	m.Write(0x4000, 0x77)
	if got := m.Read(0x4000); got != 0x77 {
		t.Fatalf("ram write mismatch: got=%02X want=77", got)
	}
}

func TestEmulator_BasicProgramExecution(t *testing.T) {
	rom := blankROM()
	copy(rom[:], []byte{
		0x3E, 0x99, // LD A,$99
		0x32, 0x00, 0x40, // LD ($4000),A
		0x3A, 0x00, 0x40, // LD A,($4000)
		0x76, // HALT
	})

	m, err := NewMachine48K(rom)
	if err != nil {
		t.Fatalf("new machine: %v", err)
	}
	e := New(m)

	step, err := e.Step()
	if err != nil {
		t.Fatalf("step 1: %v", err)
	}
	if step.Mnemonic != "LD A,$99" {
		t.Fatalf("step1 mnemonic: got=%q", step.Mnemonic)
	}
	if e.Reg.A != 0x99 {
		t.Fatalf("A mismatch: got=%02X want=99", e.Reg.A)
	}

	step, err = e.Step()
	if err != nil {
		t.Fatalf("step 2: %v", err)
	}
	if step.Mnemonic != "LD ($4000),A" {
		t.Fatalf("step2 mnemonic: got=%q", step.Mnemonic)
	}
	if got := m.Read(0x4000); got != 0x99 {
		t.Fatalf("RAM[4000] mismatch: got=%02X want=99", got)
	}

	step, err = e.Step()
	if err != nil {
		t.Fatalf("step 3: %v", err)
	}
	if step.Mnemonic != "LD A,($4000)" {
		t.Fatalf("step3 mnemonic: got=%q", step.Mnemonic)
	}

	step, err = e.Step()
	if err != nil {
		t.Fatalf("step 4: %v", err)
	}
	if step.Mnemonic != "HALT" {
		t.Fatalf("step4 mnemonic: got=%q", step.Mnemonic)
	}
	if !e.Halted {
		t.Fatal("expected halted state")
	}
}

func TestEmulator_RunForTStates(t *testing.T) {
	rom := blankROM() // NOP stream
	m, err := NewMachine48K(rom)
	if err != nil {
		t.Fatalf("new machine: %v", err)
	}
	e := New(m)

	run, err := e.RunForTStates(20, 0)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !run.Finished {
		t.Fatalf("expected finished run: %+v", run)
	}
	if run.Steps != 5 {
		t.Fatalf("steps mismatch: got=%d want=5", run.Steps)
	}
	if run.TStates != 20 {
		t.Fatalf("tstates mismatch: got=%d want=20", run.TStates)
	}
}

func TestEmulator_ROMBootPrefix(t *testing.T) {
	rom, err := os.ReadFile(repoPath("assets", "roms", "48.rom"))
	if err != nil {
		t.Fatalf("read ROM: %v", err)
	}

	m, err := NewMachine48K(rom)
	if err != nil {
		t.Fatalf("new machine: %v", err)
	}
	e := New(m)

	step, err := e.Step()
	if err != nil {
		t.Fatalf("step 1: %v", err)
	}
	if step.Mnemonic != "DI" {
		t.Fatalf("step1 mnemonic: got=%q want=DI", step.Mnemonic)
	}

	step, err = e.Step()
	if err != nil {
		t.Fatalf("step 2: %v", err)
	}
	if step.Mnemonic != "XOR A" {
		t.Fatalf("step2 mnemonic: got=%q want=XOR A", step.Mnemonic)
	}

	step, err = e.Step()
	if err != nil {
		t.Fatalf("step 3: %v", err)
	}
	if step.Mnemonic != "LD DE,$FFFF" {
		t.Fatalf("step3 mnemonic: got=%q want=LD DE,$FFFF", step.Mnemonic)
	}
	if e.Reg.DE() != 0xFFFF {
		t.Fatalf("DE mismatch: got=%04X want=FFFF", e.Reg.DE())
	}

	step, err = e.Step()
	if err != nil {
		t.Fatalf("step 4: %v", err)
	}
	if step.Mnemonic != "JP $11CB" {
		t.Fatalf("step4 mnemonic: got=%q want=JP $11CB", step.Mnemonic)
	}
	if e.Reg.PC != 0x11CB {
		t.Fatalf("PC mismatch after JP: got=%04X want=11CB", e.Reg.PC)
	}
}

func TestEmulator_ROMProgress_NoUnsupportedForWindow(t *testing.T) {
	rom, err := os.ReadFile(repoPath("assets", "roms", "48.rom"))
	if err != nil {
		t.Fatalf("read ROM: %v", err)
	}

	m, err := NewMachine48K(rom)
	if err != nil {
		t.Fatalf("new machine: %v", err)
	}
	e := New(m)

	steps, last, runErr := e.RunUntilUnsupported(50000)
	if runErr != nil {
		t.Fatalf("unsupported opcode after %d steps at PC=%04X mnemonic=%q bytes=% X: %v", steps, last.PCBefore, last.Mnemonic, last.Bytes, runErr)
	}
	if steps != 50000 {
		t.Fatalf("step budget mismatch: got=%d want=50000", steps)
	}
}
