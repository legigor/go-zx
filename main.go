//go:generate go run ./cmd/generators/opcodes/main.go --input ./assets/opcode-table.json --output ./pkg/opcodes/opcodes_generated.go
package main

import (
	"fmt"
	"go-zx/pkg/opcodes"
	"log"
	"os"
)

func loadRom() ([]byte, error) {
	data, err := os.ReadFile("assets/roms/48.rom")
	return data, err
}

func disassemble(rom []byte) {
	pc := 0
	for pc < len(rom) {
		opcode := rom[pc]
		inst, exists := opcodes.Table[opcode]
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
