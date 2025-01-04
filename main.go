//go:generate go run ./cmd/generators/opcodes/main.go --input ./assets/opcode-table.json --output ./pkg/opcodes/opcodes_generated.go
package main

import (
	"fmt"
	"log"
	"os"
)

func loadRom() ([]byte, error) {
	data, err := os.ReadFile("assets/roms/48.rom")
	// increase up to 48KB
	return data, err
}

func disassemble(rom []byte) {
	pc := 0
	for pc < len(rom) {
		opcode := rom[pc]
		fmt.Printf("%04X: DB 0x%02X ; Unknown\n", pc, opcode)
		pc += 1 // inst.Length
	}
}

func main() {
	rom, err := loadRom()
	if err != nil {
		log.Fatal(err)
	}
	disassemble(rom)
}
