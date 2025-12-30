# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

go-zx is a ZX Spectrum 48K emulator/disassembler learning project. The goal is to understand how the ZX Spectrum works by disassembling its ROM and eventually running it in an emulator.

## Build Commands

```bash
# Build and run
go run .

# Build binary
go build -o go-zx .
```

## Architecture

- `main.go` - Entry point with ROM loader and disassembler
- `assets/roms/48.rom` - ZX Spectrum 48K ROM binary (16KB)

## Z80 Resources

- [DECODING Z80 OPCODES](http://www.z80.info/decoding.htm)
- [Z80 Opcodes](http://www.breakintoprogram.co.uk/programming/assembly-language/z80/z80-opcodes)
- [Main Instructions](https://clrhome.org/table/)
- [opcode-table.json source](https://github.com/deeptoaster/opcode-table/blob/master/opcode-table.json)
