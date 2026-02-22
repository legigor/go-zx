# AGENTS.md

This file provides guidance to coding agents working in this repository.

## Project Overview

go-zx is a ZX Spectrum 48K emulator/disassembler learning project.
Current focus: ROM disassembly quality and control-flow-aware validation.

## Current Status

- Table-driven Z80 disassembler is implemented using `assets/opcode-table.json`.
- ROM disassembly output includes inline instruction comments derived from opcode descriptions.
- Comments are aligned to a fixed output column for readability.
- Unknown opcodes are reported as `DB $XX` with `Unknown opcode/data byte` comment.
- Reachable/executed-path validation exists via static control-flow checks and tracer-based checks.

## Build & Test Commands

```bash
# Run disassembly of 48K ROM
go run .

# Build binary
go build -o go-zx .

# Run all tests
go test ./...
```

## Architecture

- `main.go`
  - Loads opcode table and ROM
  - Runs full disassembly and prints formatted lines

- `assets/roms/48.rom`
  - ZX Spectrum 48K ROM binary (16KB)

- `assets/opcode-table.json`
  - Opcode templates and descriptions used by the disassembler

- `disasm/table.go`
  - Opcode table models and loaders
  - `OpcodeSpec` includes `description`

- `disasm/disassembler.go`
  - Opcode compilation and decode logic
  - `Line` model includes `Comment` and `Unknown`
  - `FormatLine` renders aligned `; comment`

- `disasm/tracer.go`
  - Abstract control-flow tracer (`TraceControlFlow`)
  - Tracks visited PCs, opcode hits, unknowns on traced paths, and dynamic branch issues

- `disasm/*_test.go`
  - Decoder, formatting, ROM E2E, reachable-path validation, and tracer validation tests

## Validation Expectations

- Linear ROM unknown count is only a guardrail (data bytes may appear in linear sweep).
- Stronger requirement: unknown opcodes should be zero on reachable/traced executable paths.
- Dynamic/indirect branches (e.g. `JP (HL)`) are tracked as dynamic issues, not immediate hard failures.

## Commit Style

Semantic commit messages are **mandatory**.

Format:

```
<type>(<scope>): <subject>
```

Types:
- `feat` — new feature
- `fix` — bug fix
- `docs` — documentation changes
- `style` — formatting, no code change
- `refactor` — code restructuring, no behavior change
- `test` — adding or updating tests
- `chore` — build, tooling, or maintenance tasks

Examples:

```
feat(cpu): implement Z80 register set
fix(disasm): correct CB-prefixed opcode decoding
docs(readme): add build instructions
refactor(memory): extract memory bus into separate module
chore(build): add Makefile
```

## Z80 Resources

- [DECODING Z80 OPCODES](http://www.z80.info/decoding.htm)
- [Z80 Opcodes](http://www.breakintoprogram.co.uk/programming/assembly-language/z80/z80-opcodes)
- [Main Instructions](https://clrhome.org/table/)
- [opcode-table.json source](https://github.com/deeptoaster/opcode-table/blob/master/opcode-table.json)
