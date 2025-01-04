//go:generate go run ./cmd/generators/opcodes/main.go --input ./assets/opcode-table.json --output ./pkg/opcodes/opcodes_generated.go
package main

import (
	"fmt"
	"log"
	"os"
)

func loadRom() ([]byte, error) {
	data, err := os.ReadFile("assets/roms/48.rom")
	return data, err
}

type Instruction struct {
	Mnemonic string
	Length   int
	Format   string
}

var Table = map[byte]Instruction{
	0x00: {"NOP", 1, ""},
	0x01: {"LD BC,nn", 3, "BC,0x%04X"},
	0x02: {"LD (BC),A", 1, "(BC),A"},
	0x03: {"INC BC", 1, "BC"},
	0x04: {"INC B", 1, "B"},
	0x05: {"DEC B", 1, "B"},
	0x06: {"LD B,n", 2, "B,0x%02X"},
	0x07: {"RLCA", 1, ""},
	0x08: {"EX AF,AF'", 1, ""},
	0x09: {"ADD HL,BC", 1, "HL,BC"},
	0x0A: {"LD A,(BC)", 1, "A,(BC)"},
	0x0B: {"DEC BC", 1, "BC"},
	0x0C: {"INC C", 1, "C"},
	0x0D: {"DEC C", 1, "C"},
	0x0E: {"LD C,n", 2, "C,0x%02X"},
	0x0F: {"RRCA", 1, ""},
	//... (continue for all opcodes)
}

func disassemble(rom []byte) {
	pc := 0
	for pc < len(rom) {
		opcode := rom[pc]
		inst, exists := Table[opcode]
		if !exists {
			fmt.Printf("%04X: DB 0x%02X ; Unknown\n", pc, opcode)
			pc++
			continue
		}

		fmt.Printf("%04X: %s ", pc, inst.Mnemonic)

		switch inst.Length {
		case 1:
			fmt.Println()
		case 2:
			operand := rom[pc+1]
			fmt.Printf(inst.Format, operand)
			fmt.Println()
		case 3:
			low := rom[pc+1]
			high := rom[pc+2]
			operand := uint16(low) | uint16(high)<<8
			fmt.Printf(inst.Format, operand)
			fmt.Println()
		}

		pc += inst.Length
	}
}

func main() {
	rom, err := loadRom()
	if err != nil {
		log.Fatal(err)
	}
	disassemble(rom)
}
