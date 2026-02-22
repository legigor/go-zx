package emu

import "fmt"

const TStatesPerFrame = 69888

type StepResult struct {
	PCBefore uint16
	PCAfter  uint16
	Bytes    []byte
	Mnemonic string
	TStates  int
}

type RunResult struct {
	Steps    int
	TStates  int
	Finished bool
}

type Emulator struct {
	Reg Registers

	IFF1 bool
	IFF2 bool
	IM   byte

	Halted bool

	Bus Bus

	StepCount    uint64
	TotalTStates uint64

	LastStep StepResult
}

func New(bus Bus) *Emulator {
	e := &Emulator{Bus: bus}
	e.Reset()
	return e
}

func (e *Emulator) Reset() {
	e.Reg = Registers{}
	e.Reg.SP = 0xFFFF
	e.Reg.PC = 0x0000
	e.IFF1 = false
	e.IFF2 = false
	e.IM = 0
	e.Halted = false
	e.StepCount = 0
	e.TotalTStates = 0
	e.LastStep = StepResult{}
}

func (e *Emulator) Step() (StepResult, error) {
	if e.Bus == nil {
		return StepResult{}, fmt.Errorf("nil bus")
	}

	if e.Halted {
		result := StepResult{
			PCBefore: e.Reg.PC,
			PCAfter:  e.Reg.PC,
			Mnemonic: "HALT",
			TStates:  4,
		}
		e.commitStep(result)
		return result, nil
	}

	pcBefore := e.Reg.PC
	bytes := make([]byte, 0, 4)

	fetch8 := func() byte {
		b := e.Bus.Read(e.Reg.PC)
		bytes = append(bytes, b)
		e.Reg.PC++
		return b
	}
	fetch16 := func() uint16 {
		lo := uint16(fetch8())
		hi := uint16(fetch8())
		return lo | (hi << 8)
	}

	op := fetch8()
	e.Reg.R++

	result := StepResult{PCBefore: pcBefore, Bytes: bytes}

	// LD r,r' block (0x40-0x7F), except HALT (0x76)
	if op >= 0x40 && op <= 0x7F && op != 0x76 {
		dst := (op >> 3) & 0x07
		src := op & 0x07
		v := e.readRegByIndex(src)
		e.writeRegByIndex(dst, v)
		result.Mnemonic = fmt.Sprintf("LD %s,%s", regName(dst), regName(src))
		if dst == 6 || src == 6 {
			result.TStates = 7
		} else {
			result.TStates = 4
		}
		result.Bytes = append([]byte(nil), bytes...)
		result.PCAfter = e.Reg.PC
		e.commitStep(result)
		return result, nil
	}

	switch op {
	case 0x00:
		result.Mnemonic = "NOP"
		result.TStates = 4
	case 0xF3:
		e.IFF1 = false
		e.IFF2 = false
		result.Mnemonic = "DI"
		result.TStates = 4
	case 0xFB:
		e.IFF1 = true
		e.IFF2 = true
		result.Mnemonic = "EI"
		result.TStates = 4
	case 0x76:
		e.Halted = true
		result.Mnemonic = "HALT"
		result.TStates = 4
	case 0xAF:
		e.xorA(e.Reg.A)
		result.Mnemonic = "XOR A"
		result.TStates = 4
	case 0xA8, 0xA9, 0xAA, 0xAB, 0xAC, 0xAD, 0xAE:
		regIndex := op & 0x07
		v := e.readRegByIndex(regIndex)
		e.xorA(v)
		result.Mnemonic = fmt.Sprintf("XOR %s", regName(regIndex))
		if regIndex == 6 {
			result.TStates = 7
		} else {
			result.TStates = 4
		}
	case 0x3E:
		n := fetch8()
		e.Reg.A = n
		result.Mnemonic = fmt.Sprintf("LD A,$%02X", n)
		result.TStates = 7
	case 0x06, 0x0E, 0x16, 0x1E, 0x26, 0x2E:
		n := fetch8()
		regIndex := (op >> 3) & 0x07
		e.writeRegByIndex(regIndex, n)
		result.Mnemonic = fmt.Sprintf("LD %s,$%02X", regName(regIndex), n)
		result.TStates = 7
	case 0x36:
		n := fetch8()
		e.Bus.Write(e.Reg.HL(), n)
		result.Mnemonic = fmt.Sprintf("LD (HL),$%02X", n)
		result.TStates = 10
	case 0x01:
		nn := fetch16()
		e.Reg.SetBC(nn)
		result.Mnemonic = fmt.Sprintf("LD BC,$%04X", nn)
		result.TStates = 10
	case 0x11:
		nn := fetch16()
		e.Reg.SetDE(nn)
		result.Mnemonic = fmt.Sprintf("LD DE,$%04X", nn)
		result.TStates = 10
	case 0x21:
		nn := fetch16()
		e.Reg.SetHL(nn)
		result.Mnemonic = fmt.Sprintf("LD HL,$%04X", nn)
		result.TStates = 10
	case 0x31:
		nn := fetch16()
		e.Reg.SP = nn
		result.Mnemonic = fmt.Sprintf("LD SP,$%04X", nn)
		result.TStates = 10
	case 0x32:
		nn := fetch16()
		e.Bus.Write(nn, e.Reg.A)
		result.Mnemonic = fmt.Sprintf("LD ($%04X),A", nn)
		result.TStates = 13
	case 0x3A:
		nn := fetch16()
		e.Reg.A = e.Bus.Read(nn)
		result.Mnemonic = fmt.Sprintf("LD A,($%04X)", nn)
		result.TStates = 13
	case 0x77:
		e.Bus.Write(e.Reg.HL(), e.Reg.A)
		result.Mnemonic = "LD (HL),A"
		result.TStates = 7
	case 0x7E:
		e.Reg.A = e.Bus.Read(e.Reg.HL())
		result.Mnemonic = "LD A,(HL)"
		result.TStates = 7
	case 0x23:
		e.Reg.SetHL(e.Reg.HL() + 1)
		result.Mnemonic = "INC HL"
		result.TStates = 6
	case 0x18:
		d := int8(fetch8())
		target := uint16(int(e.Reg.PC) + int(d))
		e.Reg.PC = target
		result.Mnemonic = fmt.Sprintf("JR $%04X", target)
		result.TStates = 12
	case 0x20, 0x28, 0x30, 0x38:
		d := int8(fetch8())
		target := uint16(int(e.Reg.PC) + int(d))
		cond, cc := e.jrCondition(op)
		if cond {
			e.Reg.PC = target
			result.Mnemonic = fmt.Sprintf("JR %s,$%04X", cc, target)
			result.TStates = 12
		} else {
			result.Mnemonic = fmt.Sprintf("JR %s,$%04X", cc, target)
			result.TStates = 7
		}
	case 0xC3:
		nn := fetch16()
		e.Reg.PC = nn
		result.Mnemonic = fmt.Sprintf("JP $%04X", nn)
		result.TStates = 10
	case 0xCD:
		nn := fetch16()
		e.push16(e.Reg.PC)
		e.Reg.PC = nn
		result.Mnemonic = fmt.Sprintf("CALL $%04X", nn)
		result.TStates = 17
	case 0xC9:
		ret := e.pop16()
		e.Reg.PC = ret
		result.Mnemonic = "RET"
		result.TStates = 10
	default:
		// Keep PC stable on trap to unsupported opcode so debugger/UI can inspect the faulting address.
		e.Reg.PC = pcBefore
		result.Bytes = append([]byte(nil), bytes...)
		result.PCAfter = e.Reg.PC
		return result, fmt.Errorf("unsupported opcode $%02X at $%04X", op, pcBefore)
	}

	result.Bytes = append([]byte(nil), bytes...)
	result.PCAfter = e.Reg.PC
	e.commitStep(result)
	return result, nil
}

func (e *Emulator) RunForTStates(budget int, maxSteps int) (RunResult, error) {
	if budget <= 0 {
		return RunResult{Finished: true}, nil
	}

	result := RunResult{}
	for result.TStates < budget {
		if maxSteps > 0 && result.Steps >= maxSteps {
			break
		}
		step, err := e.Step()
		if err != nil {
			return result, err
		}
		result.Steps++
		result.TStates += step.TStates
	}
	result.Finished = result.TStates >= budget
	return result, nil
}

func (e *Emulator) RunFrame(maxSteps int) (RunResult, error) {
	return e.RunForTStates(TStatesPerFrame, maxSteps)
}

func (e *Emulator) commitStep(step StepResult) {
	e.StepCount++
	e.TotalTStates += uint64(step.TStates)
	e.LastStep = step
}

func (e *Emulator) jrCondition(op byte) (bool, string) {
	switch op {
	case 0x20:
		return !e.Reg.Flag(FlagZ), "NZ"
	case 0x28:
		return e.Reg.Flag(FlagZ), "Z"
	case 0x30:
		return !e.Reg.Flag(FlagC), "NC"
	case 0x38:
		return e.Reg.Flag(FlagC), "C"
	default:
		return false, "?"
	}
}

func (e *Emulator) xorA(v byte) {
	res := e.Reg.A ^ v
	e.Reg.A = res
	e.Reg.F = 0
	e.Reg.SetFlag(FlagS, (res&0x80) != 0)
	e.Reg.SetFlag(FlagZ, res == 0)
	e.Reg.SetFlag(FlagPV, parityEven(res))
	e.Reg.SetFlag(FlagX, (res&0x08) != 0)
	e.Reg.SetFlag(FlagY, (res&0x20) != 0)
}

func parityEven(v byte) bool {
	parity := 0
	for i := 0; i < 8; i++ {
		if (v & (1 << i)) != 0 {
			parity++
		}
	}
	return parity%2 == 0
}

func (e *Emulator) readRegByIndex(index byte) byte {
	switch index {
	case 0:
		return e.Reg.B
	case 1:
		return e.Reg.C
	case 2:
		return e.Reg.D
	case 3:
		return e.Reg.E
	case 4:
		return e.Reg.H
	case 5:
		return e.Reg.L
	case 6:
		return e.Bus.Read(e.Reg.HL())
	case 7:
		return e.Reg.A
	default:
		return 0
	}
}

func (e *Emulator) writeRegByIndex(index byte, value byte) {
	switch index {
	case 0:
		e.Reg.B = value
	case 1:
		e.Reg.C = value
	case 2:
		e.Reg.D = value
	case 3:
		e.Reg.E = value
	case 4:
		e.Reg.H = value
	case 5:
		e.Reg.L = value
	case 6:
		e.Bus.Write(e.Reg.HL(), value)
	case 7:
		e.Reg.A = value
	}
}

func regName(index byte) string {
	switch index {
	case 0:
		return "B"
	case 1:
		return "C"
	case 2:
		return "D"
	case 3:
		return "E"
	case 4:
		return "H"
	case 5:
		return "L"
	case 6:
		return "(HL)"
	case 7:
		return "A"
	default:
		return "?"
	}
}

func (e *Emulator) push16(value uint16) {
	hi := byte(value >> 8)
	lo := byte(value)
	e.Reg.SP--
	e.Bus.Write(e.Reg.SP, hi)
	e.Reg.SP--
	e.Bus.Write(e.Reg.SP, lo)
}

func (e *Emulator) pop16() uint16 {
	lo := uint16(e.Bus.Read(e.Reg.SP))
	e.Reg.SP++
	hi := uint16(e.Bus.Read(e.Reg.SP))
	e.Reg.SP++
	return lo | (hi << 8)
}
