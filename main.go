package main

import (
	"fmt"
	"log"
	"os"

	"go-zx/disasm"
)

func loadROM() ([]byte, error) {
	return os.ReadFile("assets/roms/48.rom")
}

func main() {
	d, err := disasm.NewFromFile("assets/opcode-table.json")
	if err != nil {
		log.Fatal(err)
	}

	rom, err := loadROM()
	if err != nil {
		log.Fatal(err)
	}

	for _, line := range d.Disassemble(rom) {
		fmt.Println(disasm.FormatLine(line))
	}
}
