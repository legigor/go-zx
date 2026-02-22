package main

import (
	"log"

	"go-zx/tui"
)

func main() {
	if err := tui.RunLive("assets/roms/48.rom", "assets/opcode-table.json"); err != nil {
		log.Fatal(err)
	}
}
