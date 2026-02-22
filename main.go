package main

import (
	"log"

	"go-zx/tui"
)

func main() {
	if err := tui.RunPrototype(); err != nil {
		log.Fatal(err)
	}
}
