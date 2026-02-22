package disasm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type TraceConfig struct {
	EntryPCs      []int
	MaxStates     int
	MaxStackDepth int
}

type TraceIssue struct {
	PC       int
	Mnemonic string
	Reason   string
}

type TraceResult struct {
	Visited        map[int]int
	OpcodeHits     map[byte]int
	Unknown        []TraceIssue
	Dynamic        []TraceIssue
	StatesExplored int
	LimitReached   bool
}

func (r TraceResult) VisitedCount() int {
	return len(r.Visited)
}

func (d *Disassembler) TraceControlFlow(mem []byte, cfg TraceConfig) TraceResult {
	if len(cfg.EntryPCs) == 0 {
		cfg.EntryPCs = []int{0x0000}
	}
	if cfg.MaxStates <= 0 {
		cfg.MaxStates = 200000
	}
	if cfg.MaxStackDepth <= 0 {
		cfg.MaxStackDepth = 64
	}

	result := TraceResult{
		Visited:    make(map[int]int),
		OpcodeHits: make(map[byte]int),
	}

	type traceState struct {
		pc    int
		stack []int
	}

	queue := make([]traceState, 0, len(cfg.EntryPCs))
	for _, entry := range cfg.EntryPCs {
		if entry >= 0 && entry < len(mem) {
			queue = append(queue, traceState{pc: entry})
		}
	}

	seen := map[string]bool{}
	for len(queue) > 0 {
		if result.StatesExplored >= cfg.MaxStates {
			result.LimitReached = true
			break
		}

		state := queue[0]
		queue = queue[1:]
		if state.pc < 0 || state.pc >= len(mem) {
			continue
		}

		key := traceStateKey(state.pc, state.stack)
		if seen[key] {
			continue
		}
		seen[key] = true
		result.StatesExplored++

		line := d.DecodeAt(mem, state.pc)
		if len(line.Bytes) == 0 {
			continue
		}

		result.Visited[state.pc]++
		result.OpcodeHits[line.Bytes[0]]++

		if line.Unknown {
			result.Unknown = append(result.Unknown, TraceIssue{
				PC:       state.pc,
				Mnemonic: line.Mnemonic,
				Reason:   "unknown opcode in executed path",
			})
			continue
		}

		nextPC := state.pc + len(line.Bytes)
		op := strings.TrimSpace(line.Mnemonic)

		enqueue := func(pc int, stack []int) {
			if pc < 0 || pc >= len(mem) {
				return
			}
			queue = append(queue, traceState{pc: pc, stack: append([]int(nil), stack...)})
		}

		switch {
		case op == "HALT":
			continue
		case op == "RET" || op == "RETI" || op == "RETN":
			if len(state.stack) == 0 {
				result.Dynamic = append(result.Dynamic, TraceIssue{
					PC:       state.pc,
					Mnemonic: op,
					Reason:   "return with empty abstract stack",
				})
				continue
			}
			ret := state.stack[len(state.stack)-1]
			enqueue(ret, state.stack[:len(state.stack)-1])
			continue
		case strings.HasPrefix(op, "RET "):
			enqueue(nextPC, state.stack)
			if len(state.stack) == 0 {
				result.Dynamic = append(result.Dynamic, TraceIssue{
					PC:       state.pc,
					Mnemonic: op,
					Reason:   "conditional return with empty abstract stack",
				})
				continue
			}
			ret := state.stack[len(state.stack)-1]
			enqueue(ret, state.stack[:len(state.stack)-1])
			continue
		case strings.HasPrefix(op, "JP "):
			tail := strings.TrimPrefix(op, "JP ")
			if strings.HasPrefix(tail, "(") {
				result.Dynamic = append(result.Dynamic, TraceIssue{
					PC:       state.pc,
					Mnemonic: op,
					Reason:   "indirect jump target is runtime-dependent",
				})
				continue
			}
			target, ok := traceTargetAddress(op)
			if !ok {
				result.Dynamic = append(result.Dynamic, TraceIssue{
					PC:       state.pc,
					Mnemonic: op,
					Reason:   "failed to parse jump target",
				})
				continue
			}
			if strings.Contains(tail, ",") {
				enqueue(nextPC, state.stack)
				enqueue(target, state.stack)
			} else {
				enqueue(target, state.stack)
			}
			continue
		case strings.HasPrefix(op, "JR "):
			tail := strings.TrimPrefix(op, "JR ")
			target, ok := traceTargetAddress(op)
			if !ok {
				result.Dynamic = append(result.Dynamic, TraceIssue{
					PC:       state.pc,
					Mnemonic: op,
					Reason:   "failed to parse relative jump target",
				})
				continue
			}
			if strings.Contains(tail, ",") {
				enqueue(nextPC, state.stack)
				enqueue(target, state.stack)
			} else {
				enqueue(target, state.stack)
			}
			continue
		case strings.HasPrefix(op, "DJNZ "):
			target, ok := traceTargetAddress(op)
			if !ok {
				result.Dynamic = append(result.Dynamic, TraceIssue{
					PC:       state.pc,
					Mnemonic: op,
					Reason:   "failed to parse DJNZ target",
				})
				continue
			}
			enqueue(nextPC, state.stack)
			enqueue(target, state.stack)
			continue
		case strings.HasPrefix(op, "CALL "):
			tail := strings.TrimPrefix(op, "CALL ")
			target, ok := traceTargetAddress(op)
			if !ok {
				result.Dynamic = append(result.Dynamic, TraceIssue{
					PC:       state.pc,
					Mnemonic: op,
					Reason:   "failed to parse call target",
				})
				continue
			}
			if len(state.stack) >= cfg.MaxStackDepth {
				result.Dynamic = append(result.Dynamic, TraceIssue{
					PC:       state.pc,
					Mnemonic: op,
					Reason:   fmt.Sprintf("abstract stack depth exceeded limit (%d)", cfg.MaxStackDepth),
				})
				continue
			}
			callStack := append(append([]int(nil), state.stack...), nextPC)
			if strings.Contains(tail, ",") {
				enqueue(nextPC, state.stack)
				enqueue(target, callStack)
			} else {
				enqueue(target, callStack)
			}
			continue
		case strings.HasPrefix(op, "RST "):
			target, ok := traceTargetAddress(op)
			if !ok {
				result.Dynamic = append(result.Dynamic, TraceIssue{
					PC:       state.pc,
					Mnemonic: op,
					Reason:   "failed to parse RST target",
				})
				continue
			}
			if len(state.stack) >= cfg.MaxStackDepth {
				result.Dynamic = append(result.Dynamic, TraceIssue{
					PC:       state.pc,
					Mnemonic: op,
					Reason:   fmt.Sprintf("abstract stack depth exceeded limit (%d)", cfg.MaxStackDepth),
				})
				continue
			}
			rstStack := append(append([]int(nil), state.stack...), nextPC)
			enqueue(target, rstStack)
			continue
		default:
			enqueue(nextPC, state.stack)
		}
	}

	return result
}

func traceStateKey(pc int, stack []int) string {
	if len(stack) == 0 {
		return strconv.Itoa(pc)
	}
	parts := make([]string, 0, len(stack)+1)
	parts = append(parts, strconv.Itoa(pc))
	for _, v := range stack {
		parts = append(parts, strconv.Itoa(v))
	}
	return strings.Join(parts, ":")
}

var traceTargetRegex = regexp.MustCompile(`\$([0-9A-F]{2,4})`)

func traceTargetAddress(mnemonic string) (int, bool) {
	match := traceTargetRegex.FindStringSubmatch(mnemonic)
	if len(match) != 2 {
		return 0, false
	}
	v, err := strconv.ParseInt(match[1], 16, 32)
	if err != nil {
		return 0, false
	}
	return int(v), true
}
