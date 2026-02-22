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
	result := StepResult{PCBefore: pcBefore}

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
		return e.finishStep(result, bytes)
	}

	// ALU A,r block (0x80-0xBF)
	if op >= 0x80 && op <= 0xBF {
		src := op & 0x07
		v := e.readRegByIndex(src)
		group := (op >> 3) & 0x07
		suffix := regName(src)
		switch group {
		case 0:
			e.addA(v, false)
			result.Mnemonic = fmt.Sprintf("ADD A,%s", suffix)
		case 1:
			e.addA(v, true)
			result.Mnemonic = fmt.Sprintf("ADC A,%s", suffix)
		case 2:
			e.subA(v, false)
			result.Mnemonic = fmt.Sprintf("SUB %s", suffix)
		case 3:
			e.subA(v, true)
			result.Mnemonic = fmt.Sprintf("SBC A,%s", suffix)
		case 4:
			e.andA(v)
			result.Mnemonic = fmt.Sprintf("AND %s", suffix)
		case 5:
			e.xorA(v)
			result.Mnemonic = fmt.Sprintf("XOR %s", suffix)
		case 6:
			e.orA(v)
			result.Mnemonic = fmt.Sprintf("OR %s", suffix)
		case 7:
			e.cpA(v)
			result.Mnemonic = fmt.Sprintf("CP %s", suffix)
		}
		if src == 6 {
			result.TStates = 7
		} else {
			result.TStates = 4
		}
		return e.finishStep(result, bytes)
	}

	// INC r / DEC r blocks
	if op&0xC7 == 0x04 {
		idx := (op >> 3) & 0x07
		v := e.readRegByIndex(idx)
		v2 := e.inc8(v)
		e.writeRegByIndex(idx, v2)
		result.Mnemonic = fmt.Sprintf("INC %s", regName(idx))
		if idx == 6 {
			result.TStates = 11
		} else {
			result.TStates = 4
		}
		return e.finishStep(result, bytes)
	}
	if op&0xC7 == 0x05 {
		idx := (op >> 3) & 0x07
		v := e.readRegByIndex(idx)
		v2 := e.dec8(v)
		e.writeRegByIndex(idx, v2)
		result.Mnemonic = fmt.Sprintf("DEC %s", regName(idx))
		if idx == 6 {
			result.TStates = 11
		} else {
			result.TStates = 4
		}
		return e.finishStep(result, bytes)
	}

	switch op {
	case 0x00:
		result.Mnemonic = "NOP"
		result.TStates = 4
	case 0x01, 0x11, 0x21, 0x31:
		nn := fetch16()
		pair := (op >> 4) & 0x03
		e.writePairByIndex(pair, nn)
		result.Mnemonic = fmt.Sprintf("LD %s,$%04X", pairName(pair), nn)
		result.TStates = 10
	case 0x02:
		e.Bus.Write(e.Reg.BC(), e.Reg.A)
		result.Mnemonic = "LD (BC),A"
		result.TStates = 7
	case 0x0A:
		e.Reg.A = e.Bus.Read(e.Reg.BC())
		result.Mnemonic = "LD A,(BC)"
		result.TStates = 7
	case 0x12:
		e.Bus.Write(e.Reg.DE(), e.Reg.A)
		result.Mnemonic = "LD (DE),A"
		result.TStates = 7
	case 0x1A:
		e.Reg.A = e.Bus.Read(e.Reg.DE())
		result.Mnemonic = "LD A,(DE)"
		result.TStates = 7
	case 0x03, 0x13, 0x23, 0x33:
		pair := (op >> 4) & 0x03
		v := e.readPairByIndex(pair) + 1
		e.writePairByIndex(pair, v)
		result.Mnemonic = fmt.Sprintf("INC %s", pairName(pair))
		result.TStates = 6
	case 0x0B, 0x1B, 0x2B, 0x3B:
		pair := (op >> 4) & 0x03
		v := e.readPairByIndex(pair) - 1
		e.writePairByIndex(pair, v)
		result.Mnemonic = fmt.Sprintf("DEC %s", pairName(pair))
		result.TStates = 6
	case 0x06, 0x0E, 0x16, 0x1E, 0x26, 0x2E, 0x36, 0x3E:
		n := fetch8()
		idx := (op >> 3) & 0x07
		e.writeRegByIndex(idx, n)
		result.Mnemonic = fmt.Sprintf("LD %s,$%02X", regName(idx), n)
		if idx == 6 {
			result.TStates = 10
		} else {
			result.TStates = 7
		}
	case 0x07:
		e.rlca()
		result.Mnemonic = "RLCA"
		result.TStates = 4
	case 0x08:
		e.exAF()
		result.Mnemonic = "EX AF,AF'"
		result.TStates = 4
	case 0x09, 0x19, 0x29, 0x39:
		pair := (op >> 4) & 0x03
		v := e.readPairByIndex(pair)
		e.addHL(v)
		result.Mnemonic = fmt.Sprintf("ADD HL,%s", pairName(pair))
		result.TStates = 11
	case 0x0F:
		e.rrca()
		result.Mnemonic = "RRCA"
		result.TStates = 4
	case 0x10:
		d := int8(fetch8())
		target := uint16(int(e.Reg.PC) + int(d))
		e.Reg.B--
		if e.Reg.B != 0 {
			e.Reg.PC = target
			result.TStates = 13
		} else {
			result.TStates = 8
		}
		result.Mnemonic = fmt.Sprintf("DJNZ $%04X", target)
	case 0x17:
		e.rla()
		result.Mnemonic = "RLA"
		result.TStates = 4
	case 0x18:
		d := int8(fetch8())
		target := uint16(int(e.Reg.PC) + int(d))
		e.Reg.PC = target
		result.Mnemonic = fmt.Sprintf("JR $%04X", target)
		result.TStates = 12
	case 0x1F:
		e.rra()
		result.Mnemonic = "RRA"
		result.TStates = 4
	case 0x20, 0x28, 0x30, 0x38:
		d := int8(fetch8())
		target := uint16(int(e.Reg.PC) + int(d))
		cond, cc := e.jrCondition(op)
		if cond {
			e.Reg.PC = target
			result.TStates = 12
		} else {
			result.TStates = 7
		}
		result.Mnemonic = fmt.Sprintf("JR %s,$%04X", cc, target)
	case 0x22:
		nn := fetch16()
		hl := e.Reg.HL()
		e.Bus.Write(nn, byte(hl))
		e.Bus.Write(nn+1, byte(hl>>8))
		result.Mnemonic = fmt.Sprintf("LD ($%04X),HL", nn)
		result.TStates = 16
	case 0x2A:
		nn := fetch16()
		lo := uint16(e.Bus.Read(nn))
		hi := uint16(e.Bus.Read(nn + 1))
		e.Reg.SetHL(lo | (hi << 8))
		result.Mnemonic = fmt.Sprintf("LD HL,($%04X)", nn)
		result.TStates = 16
	case 0x2F:
		e.cpl()
		result.Mnemonic = "CPL"
		result.TStates = 4
	case 0x32:
		nn := fetch16()
		e.Bus.Write(nn, e.Reg.A)
		result.Mnemonic = fmt.Sprintf("LD ($%04X),A", nn)
		result.TStates = 13
	case 0x37:
		e.scf()
		result.Mnemonic = "SCF"
		result.TStates = 4
	case 0x3A:
		nn := fetch16()
		e.Reg.A = e.Bus.Read(nn)
		result.Mnemonic = fmt.Sprintf("LD A,($%04X)", nn)
		result.TStates = 13
	case 0x3F:
		e.ccf()
		result.Mnemonic = "CCF"
		result.TStates = 4
	case 0x76:
		e.Halted = true
		result.Mnemonic = "HALT"
		result.TStates = 4
	case 0xC3:
		nn := fetch16()
		e.Reg.PC = nn
		result.Mnemonic = fmt.Sprintf("JP $%04X", nn)
		result.TStates = 10
	case 0xC2, 0xCA, 0xD2, 0xDA, 0xE2, 0xEA, 0xF2, 0xFA:
		nn := fetch16()
		condIndex := (op >> 3) & 0x07
		cond, cc := e.condByIndex(condIndex)
		if cond {
			e.Reg.PC = nn
		}
		result.Mnemonic = fmt.Sprintf("JP %s,$%04X", cc, nn)
		result.TStates = 10
	case 0xC9:
		ret := e.pop16()
		e.Reg.PC = ret
		result.Mnemonic = "RET"
		result.TStates = 10
	case 0xC0, 0xC8, 0xD0, 0xD8, 0xE0, 0xE8, 0xF0, 0xF8:
		condIndex := (op >> 3) & 0x07
		cond, cc := e.condByIndex(condIndex)
		if cond {
			ret := e.pop16()
			e.Reg.PC = ret
			result.TStates = 11
		} else {
			result.TStates = 5
		}
		result.Mnemonic = fmt.Sprintf("RET %s", cc)
	case 0xCD:
		nn := fetch16()
		e.push16(e.Reg.PC)
		e.Reg.PC = nn
		result.Mnemonic = fmt.Sprintf("CALL $%04X", nn)
		result.TStates = 17
	case 0xC4, 0xCC, 0xD4, 0xDC, 0xE4, 0xEC, 0xF4, 0xFC:
		nn := fetch16()
		condIndex := (op >> 3) & 0x07
		cond, cc := e.condByIndex(condIndex)
		if cond {
			e.push16(e.Reg.PC)
			e.Reg.PC = nn
			result.TStates = 17
		} else {
			result.TStates = 10
		}
		result.Mnemonic = fmt.Sprintf("CALL %s,$%04X", cc, nn)
	case 0xC1, 0xD1, 0xE1, 0xF1:
		pair := (op >> 4) & 0x03
		value := e.pop16()
		e.writePair2ByIndex(pair, value)
		result.Mnemonic = fmt.Sprintf("POP %s", pair2Name(pair))
		result.TStates = 10
	case 0xC5, 0xD5, 0xE5, 0xF5:
		pair := (op >> 4) & 0x03
		value := e.readPair2ByIndex(pair)
		e.push16(value)
		result.Mnemonic = fmt.Sprintf("PUSH %s", pair2Name(pair))
		result.TStates = 11
	case 0xC6, 0xCE, 0xD6, 0xDE, 0xE6, 0xEE, 0xF6, 0xFE:
		n := fetch8()
		group := (op >> 3) & 0x07
		switch group {
		case 0:
			e.addA(n, false)
			result.Mnemonic = fmt.Sprintf("ADD A,$%02X", n)
		case 1:
			e.addA(n, true)
			result.Mnemonic = fmt.Sprintf("ADC A,$%02X", n)
		case 2:
			e.subA(n, false)
			result.Mnemonic = fmt.Sprintf("SUB $%02X", n)
		case 3:
			e.subA(n, true)
			result.Mnemonic = fmt.Sprintf("SBC A,$%02X", n)
		case 4:
			e.andA(n)
			result.Mnemonic = fmt.Sprintf("AND $%02X", n)
		case 5:
			e.xorA(n)
			result.Mnemonic = fmt.Sprintf("XOR $%02X", n)
		case 6:
			e.orA(n)
			result.Mnemonic = fmt.Sprintf("OR $%02X", n)
		case 7:
			e.cpA(n)
			result.Mnemonic = fmt.Sprintf("CP $%02X", n)
		}
		result.TStates = 7
	case 0xC7, 0xCF, 0xD7, 0xDF, 0xE7, 0xEF, 0xF7, 0xFF:
		vec := uint16(op & 0x38)
		e.push16(e.Reg.PC)
		e.Reg.PC = vec
		result.Mnemonic = fmt.Sprintf("RST $%02X", vec)
		result.TStates = 11
	case 0xD3:
		n := fetch8()
		port := uint16(n) | (uint16(e.Reg.A) << 8)
		e.Bus.Out(port, e.Reg.A)
		result.Mnemonic = fmt.Sprintf("OUT ($%02X),A", n)
		result.TStates = 11
	case 0xD9:
		e.exx()
		result.Mnemonic = "EXX"
		result.TStates = 4
	case 0xDB:
		n := fetch8()
		port := uint16(n) | (uint16(e.Reg.A) << 8)
		e.Reg.A = e.Bus.In(port)
		result.Mnemonic = fmt.Sprintf("IN A,($%02X)", n)
		result.TStates = 11
	case 0xE3:
		e.exSPHL()
		result.Mnemonic = "EX (SP),HL"
		result.TStates = 19
	case 0xE9:
		e.Reg.PC = e.Reg.HL()
		result.Mnemonic = "JP (HL)"
		result.TStates = 4
	case 0xEB:
		e.exDEHL()
		result.Mnemonic = "EX DE,HL"
		result.TStates = 4
	case 0xF3:
		e.IFF1 = false
		e.IFF2 = false
		result.Mnemonic = "DI"
		result.TStates = 4
	case 0xF9:
		e.Reg.SP = e.Reg.HL()
		result.Mnemonic = "LD SP,HL"
		result.TStates = 6
	case 0xFB:
		e.IFF1 = true
		e.IFF2 = true
		result.Mnemonic = "EI"
		result.TStates = 4
	case 0xED:
		ed := fetch8()
		if err := e.execED(ed, &result, fetch8, fetch16); err != nil {
			e.Reg.PC = pcBefore
			result.Bytes = append([]byte(nil), bytes...)
			result.PCAfter = e.Reg.PC
			return result, err
		}
	case 0xDD, 0xFD:
		if err := e.execIndexed(op, &result, fetch8, fetch16); err != nil {
			e.Reg.PC = pcBefore
			result.Bytes = append([]byte(nil), bytes...)
			result.PCAfter = e.Reg.PC
			return result, err
		}
	case 0xCB:
		cb := fetch8()
		if err := e.execCB(cb, &result, fetch8); err != nil {
			e.Reg.PC = pcBefore
			result.Bytes = append([]byte(nil), bytes...)
			result.PCAfter = e.Reg.PC
			return result, err
		}
	default:
		// Undocumented/reserved unprefixed opcodes are treated as recognized no-ops for now.
		result.Mnemonic = fmt.Sprintf("NOP* $%02X", op)
		result.TStates = 4
	}

	return e.finishStep(result, bytes)
}

func (e *Emulator) execCB(cb byte, result *StepResult, _ func() byte) error {
	x := cb >> 6
	y := (cb >> 3) & 0x07
	z := cb & 0x07

	if x == 0 {
		v := e.readRegByIndex(z)
		res := e.cbRotateShift(y, v)
		e.writeRegByIndex(z, res)
		result.Mnemonic = fmt.Sprintf("%s %s", cbOpName(y), regName(z))
		if z == 6 {
			result.TStates = 15
		} else {
			result.TStates = 8
		}
		return nil
	}

	if x == 1 {
		v := e.readRegByIndex(z)
		e.cbBit(y, v)
		result.Mnemonic = fmt.Sprintf("BIT %d,%s", y, regName(z))
		if z == 6 {
			result.TStates = 12
		} else {
			result.TStates = 8
		}
		return nil
	}

	if x == 2 {
		v := e.readRegByIndex(z)
		res := v &^ (1 << y)
		e.writeRegByIndex(z, res)
		result.Mnemonic = fmt.Sprintf("RES %d,%s", y, regName(z))
		if z == 6 {
			result.TStates = 15
		} else {
			result.TStates = 8
		}
		return nil
	}

	v := e.readRegByIndex(z)
	res := v | (1 << y)
	e.writeRegByIndex(z, res)
	result.Mnemonic = fmt.Sprintf("SET %d,%s", y, regName(z))
	if z == 6 {
		result.TStates = 15
	} else {
		result.TStates = 8
	}
	return nil
}

func (e *Emulator) execIndexed(prefix byte, result *StepResult, fetch8 func() byte, fetch16 func() uint16) error {
	op := fetch8()
	name := indexName(prefix)

	if op == 0xCB {
		d := int8(fetch8())
		cb := fetch8()
		return e.execIndexedCB(prefix, d, cb, result)
	}

	switch op {
	case 0x21:
		nn := fetch16()
		e.writeIndex(prefix, nn)
		result.Mnemonic = fmt.Sprintf("LD %s,$%04X", name, nn)
		result.TStates = 14
	case 0x22:
		nn := fetch16()
		v := e.readIndex(prefix)
		e.Bus.Write(nn, byte(v))
		e.Bus.Write(nn+1, byte(v>>8))
		result.Mnemonic = fmt.Sprintf("LD ($%04X),%s", nn, name)
		result.TStates = 20
	case 0x2A:
		nn := fetch16()
		lo := uint16(e.Bus.Read(nn))
		hi := uint16(e.Bus.Read(nn + 1))
		e.writeIndex(prefix, lo|(hi<<8))
		result.Mnemonic = fmt.Sprintf("LD %s,($%04X)", name, nn)
		result.TStates = 20
	case 0x23:
		e.writeIndex(prefix, e.readIndex(prefix)+1)
		result.Mnemonic = fmt.Sprintf("INC %s", name)
		result.TStates = 10
	case 0x2B:
		e.writeIndex(prefix, e.readIndex(prefix)-1)
		result.Mnemonic = fmt.Sprintf("DEC %s", name)
		result.TStates = 10
	case 0x09, 0x19, 0x29, 0x39:
		pair := (op >> 4) & 0x03
		left := e.readIndex(prefix)
		right := e.readPairByIndex(pair)
		e.writeIndex(prefix, e.add16WithCarryFlags(left, right))
		result.Mnemonic = fmt.Sprintf("ADD %s,%s", name, pairName(pair))
		result.TStates = 15
	case 0xE1:
		e.writeIndex(prefix, e.pop16())
		result.Mnemonic = fmt.Sprintf("POP %s", name)
		result.TStates = 14
	case 0xE5:
		e.push16(e.readIndex(prefix))
		result.Mnemonic = fmt.Sprintf("PUSH %s", name)
		result.TStates = 15
	case 0xE9:
		e.Reg.PC = e.readIndex(prefix)
		result.Mnemonic = fmt.Sprintf("JP (%s)", name)
		result.TStates = 8
	case 0xF9:
		e.Reg.SP = e.readIndex(prefix)
		result.Mnemonic = fmt.Sprintf("LD SP,%s", name)
		result.TStates = 10
	case 0x36:
		d := int8(fetch8())
		n := fetch8()
		e.Bus.Write(e.indexedAddr(prefix, d), n)
		result.Mnemonic = fmt.Sprintf("LD (%s%+d),$%02X", name, d, n)
		result.TStates = 19
	case 0x34:
		d := int8(fetch8())
		addr := e.indexedAddr(prefix, d)
		v := e.Bus.Read(addr)
		e.Bus.Write(addr, e.inc8(v))
		result.Mnemonic = fmt.Sprintf("INC (%s%+d)", name, d)
		result.TStates = 23
	case 0x35:
		d := int8(fetch8())
		addr := e.indexedAddr(prefix, d)
		v := e.Bus.Read(addr)
		e.Bus.Write(addr, e.dec8(v))
		result.Mnemonic = fmt.Sprintf("DEC (%s%+d)", name, d)
		result.TStates = 23
	default:
		if op >= 0x70 && op <= 0x77 && op != 0x76 {
			d := int8(fetch8())
			src := op & 0x07
			e.Bus.Write(e.indexedAddr(prefix, d), e.readRegByIndex(src))
			result.Mnemonic = fmt.Sprintf("LD (%s%+d),%s", name, d, regName(src))
			result.TStates = 19
			return nil
		}
		if op >= 0x46 && op <= 0x7E && ((op & 0x07) == 0x06) {
			d := int8(fetch8())
			dst := (op >> 3) & 0x07
			v := e.Bus.Read(e.indexedAddr(prefix, d))
			e.writeRegByIndex(dst, v)
			result.Mnemonic = fmt.Sprintf("LD %s,(%s%+d)", regName(dst), name, d)
			result.TStates = 19
			return nil
		}
		if op >= 0x86 && op <= 0xBE && (op&0x07) == 0x06 {
			d := int8(fetch8())
			v := e.Bus.Read(e.indexedAddr(prefix, d))
			group := (op >> 3) & 0x07
			suffix := fmt.Sprintf("(%s%+d)", name, d)
			switch group {
			case 0:
				e.addA(v, false)
				result.Mnemonic = fmt.Sprintf("ADD A,%s", suffix)
			case 1:
				e.addA(v, true)
				result.Mnemonic = fmt.Sprintf("ADC A,%s", suffix)
			case 2:
				e.subA(v, false)
				result.Mnemonic = fmt.Sprintf("SUB %s", suffix)
			case 3:
				e.subA(v, true)
				result.Mnemonic = fmt.Sprintf("SBC A,%s", suffix)
			case 4:
				e.andA(v)
				result.Mnemonic = fmt.Sprintf("AND %s", suffix)
			case 5:
				e.xorA(v)
				result.Mnemonic = fmt.Sprintf("XOR %s", suffix)
			case 6:
				e.orA(v)
				result.Mnemonic = fmt.Sprintf("OR %s", suffix)
			case 7:
				e.cpA(v)
				result.Mnemonic = fmt.Sprintf("CP %s", suffix)
			}
			result.TStates = 19
			return nil
		}
		// Many DD/FD-prefixed forms are undocumented; keep them recognized for now.
		result.Mnemonic = fmt.Sprintf("%s-NOP $%02X", name, op)
		result.TStates = 8
		return nil
	}

	return nil
}

func (e *Emulator) execIndexedCB(prefix byte, d int8, cb byte, result *StepResult) error {
	x := cb >> 6
	y := (cb >> 3) & 0x07
	z := cb & 0x07
	name := indexName(prefix)
	addr := e.indexedAddr(prefix, d)
	v := e.Bus.Read(addr)

	if x == 0 {
		res := e.cbRotateShift(y, v)
		e.Bus.Write(addr, res)
		if z != 6 {
			e.writeRegByIndex(z, res)
		}
		result.Mnemonic = fmt.Sprintf("%s (%s%+d)", cbOpName(y), name, d)
		result.TStates = 23
		return nil
	}

	if x == 1 {
		e.cbBit(y, v)
		result.Mnemonic = fmt.Sprintf("BIT %d,(%s%+d)", y, name, d)
		result.TStates = 20
		return nil
	}

	if x == 2 {
		res := v &^ (1 << y)
		e.Bus.Write(addr, res)
		if z != 6 {
			e.writeRegByIndex(z, res)
		}
		result.Mnemonic = fmt.Sprintf("RES %d,(%s%+d)", y, name, d)
		result.TStates = 23
		return nil
	}

	res := v | (1 << y)
	e.Bus.Write(addr, res)
	if z != 6 {
		e.writeRegByIndex(z, res)
	}
	result.Mnemonic = fmt.Sprintf("SET %d,(%s%+d)", y, name, d)
	result.TStates = 23
	return nil
}

func (e *Emulator) execED(ed byte, result *StepResult, _ func() byte, fetch16 func() uint16) error {
	switch ed {
	case 0x42, 0x52, 0x62, 0x72:
		pair := (ed >> 4) & 0x03
		ss := e.readPairByIndex(pair)
		e.sbcHL(ss)
		result.Mnemonic = fmt.Sprintf("SBC HL,%s", pairName(pair))
		result.TStates = 15
	case 0x4A, 0x5A, 0x6A, 0x7A:
		pair := (ed >> 4) & 0x03
		ss := e.readPairByIndex(pair)
		e.adcHL(ss)
		result.Mnemonic = fmt.Sprintf("ADC HL,%s", pairName(pair))
		result.TStates = 15
	case 0x43, 0x53, 0x63, 0x73:
		pair := (ed >> 4) & 0x03
		nn := fetch16()
		value := e.readPairByIndex(pair)
		e.Bus.Write(nn, byte(value))
		e.Bus.Write(nn+1, byte(value>>8))
		result.Mnemonic = fmt.Sprintf("LD ($%04X),%s", nn, pairName(pair))
		result.TStates = 20
	case 0x4B, 0x5B, 0x6B, 0x7B:
		pair := (ed >> 4) & 0x03
		nn := fetch16()
		lo := uint16(e.Bus.Read(nn))
		hi := uint16(e.Bus.Read(nn + 1))
		e.writePairByIndex(pair, lo|(hi<<8))
		result.Mnemonic = fmt.Sprintf("LD %s,($%04X)", pairName(pair), nn)
		result.TStates = 20
	case 0x44, 0x4C, 0x54, 0x5C, 0x64, 0x6C, 0x74, 0x7C:
		e.neg()
		result.Mnemonic = "NEG"
		result.TStates = 8
	case 0x45:
		e.IFF1 = e.IFF2
		e.Reg.PC = e.pop16()
		result.Mnemonic = "RETN"
		result.TStates = 14
	case 0x4D:
		e.Reg.PC = e.pop16()
		result.Mnemonic = "RETI"
		result.TStates = 14
	case 0x46, 0x4E, 0x66, 0x6E:
		e.IM = 0
		result.Mnemonic = "IM 0"
		result.TStates = 8
	case 0x56, 0x76:
		e.IM = 1
		result.Mnemonic = "IM 1"
		result.TStates = 8
	case 0x5E, 0x7E:
		e.IM = 2
		result.Mnemonic = "IM 2"
		result.TStates = 8
	case 0x47:
		e.Reg.I = e.Reg.A
		result.Mnemonic = "LD I,A"
		result.TStates = 9
	case 0x4F:
		e.Reg.R = e.Reg.A
		result.Mnemonic = "LD R,A"
		result.TStates = 9
	case 0x57:
		e.Reg.A = e.Reg.I
		e.Reg.SetFlag(FlagS, (e.Reg.A&0x80) != 0)
		e.Reg.SetFlag(FlagZ, e.Reg.A == 0)
		e.Reg.SetFlag(FlagH, false)
		e.Reg.SetFlag(FlagPV, e.IFF2)
		e.Reg.SetFlag(FlagN, false)
		e.Reg.SetFlag(FlagX, (e.Reg.A&0x08) != 0)
		e.Reg.SetFlag(FlagY, (e.Reg.A&0x20) != 0)
		result.Mnemonic = "LD A,I"
		result.TStates = 9
	case 0x5F:
		e.Reg.A = e.Reg.R
		e.Reg.SetFlag(FlagS, (e.Reg.A&0x80) != 0)
		e.Reg.SetFlag(FlagZ, e.Reg.A == 0)
		e.Reg.SetFlag(FlagH, false)
		e.Reg.SetFlag(FlagPV, e.IFF2)
		e.Reg.SetFlag(FlagN, false)
		e.Reg.SetFlag(FlagX, (e.Reg.A&0x08) != 0)
		e.Reg.SetFlag(FlagY, (e.Reg.A&0x20) != 0)
		result.Mnemonic = "LD A,R"
		result.TStates = 9
	case 0xA0:
		e.ldi()
		result.Mnemonic = "LDI"
		result.TStates = 16
	case 0xA8:
		e.ldd()
		result.Mnemonic = "LDD"
		result.TStates = 16
	case 0xB0:
		e.ldi()
		result.Mnemonic = "LDIR"
		if e.Reg.BC() != 0 {
			e.Reg.PC -= 2
			result.TStates = 21
		} else {
			result.TStates = 16
		}
	case 0xB8:
		e.ldd()
		result.Mnemonic = "LDDR"
		if e.Reg.BC() != 0 {
			e.Reg.PC -= 2
			result.TStates = 21
		} else {
			result.TStates = 16
		}
	case 0xA1:
		e.cpi(false)
		result.Mnemonic = "CPI"
		result.TStates = 16
	case 0xA9:
		e.cpi(true)
		result.Mnemonic = "CPD"
		result.TStates = 16
	case 0xB1:
		e.cpi(false)
		result.Mnemonic = "CPIR"
		if e.Reg.BC() != 0 && !e.Reg.Flag(FlagZ) {
			e.Reg.PC -= 2
			result.TStates = 21
		} else {
			result.TStates = 16
		}
	case 0xB9:
		e.cpi(true)
		result.Mnemonic = "CPDR"
		if e.Reg.BC() != 0 && !e.Reg.Flag(FlagZ) {
			e.Reg.PC -= 2
			result.TStates = 21
		} else {
			result.TStates = 16
		}
	case 0xA2:
		e.ini(false)
		result.Mnemonic = "INI"
		result.TStates = 16
	case 0xAA:
		e.ini(true)
		result.Mnemonic = "IND"
		result.TStates = 16
	case 0xB2:
		e.ini(false)
		result.Mnemonic = "INIR"
		if e.Reg.B != 0 {
			e.Reg.PC -= 2
			result.TStates = 21
		} else {
			result.TStates = 16
		}
	case 0xBA:
		e.ini(true)
		result.Mnemonic = "INDR"
		if e.Reg.B != 0 {
			e.Reg.PC -= 2
			result.TStates = 21
		} else {
			result.TStates = 16
		}
	case 0xA3:
		e.outi(false)
		result.Mnemonic = "OUTI"
		result.TStates = 16
	case 0xAB:
		e.outi(true)
		result.Mnemonic = "OUTD"
		result.TStates = 16
	case 0xB3:
		e.outi(false)
		result.Mnemonic = "OTIR"
		if e.Reg.B != 0 {
			e.Reg.PC -= 2
			result.TStates = 21
		} else {
			result.TStates = 16
		}
	case 0xBB:
		e.outi(true)
		result.Mnemonic = "OTDR"
		if e.Reg.B != 0 {
			e.Reg.PC -= 2
			result.TStates = 21
		} else {
			result.TStates = 16
		}
	default:
		// Undocumented/reserved ED opcodes are treated as recognized no-ops for now.
		result.Mnemonic = fmt.Sprintf("ED-NOP $%02X", ed)
		result.TStates = 8
	}
	return nil
}

func (e *Emulator) finishStep(result StepResult, bytes []byte) (StepResult, error) {
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

func (e *Emulator) RunUntilUnsupported(maxSteps int) (steps int, last StepResult, err error) {
	for steps < maxSteps {
		step, stepErr := e.Step()
		if stepErr != nil {
			return steps, step, stepErr
		}
		steps++
		last = step
	}
	return steps, last, nil
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

func (e *Emulator) condByIndex(index byte) (bool, string) {
	switch index {
	case 0:
		return !e.Reg.Flag(FlagZ), "NZ"
	case 1:
		return e.Reg.Flag(FlagZ), "Z"
	case 2:
		return !e.Reg.Flag(FlagC), "NC"
	case 3:
		return e.Reg.Flag(FlagC), "C"
	case 4:
		return !e.Reg.Flag(FlagPV), "PO"
	case 5:
		return e.Reg.Flag(FlagPV), "PE"
	case 6:
		return !e.Reg.Flag(FlagS), "P"
	case 7:
		return e.Reg.Flag(FlagS), "M"
	default:
		return false, "?"
	}
}

func (e *Emulator) readPairByIndex(index byte) uint16 {
	switch index {
	case 0:
		return e.Reg.BC()
	case 1:
		return e.Reg.DE()
	case 2:
		return e.Reg.HL()
	case 3:
		return e.Reg.SP
	default:
		return 0
	}
}

func (e *Emulator) writePairByIndex(index byte, value uint16) {
	switch index {
	case 0:
		e.Reg.SetBC(value)
	case 1:
		e.Reg.SetDE(value)
	case 2:
		e.Reg.SetHL(value)
	case 3:
		e.Reg.SP = value
	}
}

func (e *Emulator) readPair2ByIndex(index byte) uint16 {
	switch index {
	case 0:
		return e.Reg.BC()
	case 1:
		return e.Reg.DE()
	case 2:
		return e.Reg.HL()
	case 3:
		return e.Reg.AF()
	default:
		return 0
	}
}

func (e *Emulator) writePair2ByIndex(index byte, value uint16) {
	switch index {
	case 0:
		e.Reg.SetBC(value)
	case 1:
		e.Reg.SetDE(value)
	case 2:
		e.Reg.SetHL(value)
	case 3:
		e.Reg.SetAF(value)
	}
}

func pairName(index byte) string {
	switch index {
	case 0:
		return "BC"
	case 1:
		return "DE"
	case 2:
		return "HL"
	case 3:
		return "SP"
	default:
		return "?"
	}
}

func pair2Name(index byte) string {
	switch index {
	case 0:
		return "BC"
	case 1:
		return "DE"
	case 2:
		return "HL"
	case 3:
		return "AF"
	default:
		return "?"
	}
}

func indexName(prefix byte) string {
	if prefix == 0xDD {
		return "IX"
	}
	return "IY"
}

func (e *Emulator) readIndex(prefix byte) uint16 {
	if prefix == 0xDD {
		return e.Reg.IX
	}
	return e.Reg.IY
}

func (e *Emulator) writeIndex(prefix byte, value uint16) {
	if prefix == 0xDD {
		e.Reg.IX = value
		return
	}
	e.Reg.IY = value
}

func (e *Emulator) indexedAddr(prefix byte, d int8) uint16 {
	base := e.readIndex(prefix)
	return uint16(int(base) + int(d))
}

func cbOpName(y byte) string {
	switch y {
	case 0:
		return "RLC"
	case 1:
		return "RRC"
	case 2:
		return "RL"
	case 3:
		return "RR"
	case 4:
		return "SLA"
	case 5:
		return "SRA"
	case 6:
		return "SLL"
	case 7:
		return "SRL"
	default:
		return "?"
	}
}

func (e *Emulator) cbRotateShift(y byte, v byte) byte {
	var res byte
	carryOut := byte(0)
	carryIn := byte(0)
	if e.Reg.Flag(FlagC) {
		carryIn = 1
	}

	switch y {
	case 0: // RLC
		carryOut = (v >> 7) & 1
		res = (v << 1) | carryOut
	case 1: // RRC
		carryOut = v & 1
		res = (v >> 1) | (carryOut << 7)
	case 2: // RL
		carryOut = (v >> 7) & 1
		res = (v << 1) | carryIn
	case 3: // RR
		carryOut = v & 1
		res = (v >> 1) | (carryIn << 7)
	case 4: // SLA
		carryOut = (v >> 7) & 1
		res = v << 1
	case 5: // SRA
		carryOut = v & 1
		res = (v >> 1) | (v & 0x80)
	case 6: // SLL (undocumented)
		carryOut = (v >> 7) & 1
		res = (v << 1) | 0x01
	case 7: // SRL
		carryOut = v & 1
		res = v >> 1
	}

	f := byte(0)
	if (res & 0x80) != 0 {
		f |= FlagS
	}
	if res == 0 {
		f |= FlagZ
	}
	if parityEven(res) {
		f |= FlagPV
	}
	if carryOut != 0 {
		f |= FlagC
	}
	f |= res & (FlagX | FlagY)
	e.Reg.F = f
	return res
}

func (e *Emulator) cbBit(bit byte, v byte) {
	mask := byte(1 << bit)
	set := (v & mask) != 0
	f := e.Reg.F & FlagC
	f |= FlagH
	if !set {
		f |= FlagZ | FlagPV
	}
	if bit == 7 && set {
		f |= FlagS
	}
	f |= v & (FlagX | FlagY)
	e.Reg.F = f
}

func (e *Emulator) add16WithCarryFlags(left, right uint16) uint16 {
	sum := uint32(left) + uint32(right)
	res := uint16(sum)
	f := e.Reg.F & (FlagS | FlagZ | FlagPV)
	if ((left & 0x0FFF) + (right & 0x0FFF)) > 0x0FFF {
		f |= FlagH
	}
	if sum > 0xFFFF {
		f |= FlagC
	}
	f |= byte((res >> 8) & 0x28)
	e.Reg.F = f
	return res
}

func (e *Emulator) addHL(value uint16) {
	hl := e.Reg.HL()
	sum := uint32(hl) + uint32(value)
	res := uint16(sum)

	f := e.Reg.F & (FlagS | FlagZ | FlagPV)
	if ((hl & 0x0FFF) + (value & 0x0FFF)) > 0x0FFF {
		f |= FlagH
	}
	if sum > 0xFFFF {
		f |= FlagC
	}
	f |= byte((res >> 8) & 0x28)
	e.Reg.F = f
	e.Reg.SetHL(res)
}

func (e *Emulator) adcHL(value uint16) {
	hl := e.Reg.HL()
	carry := uint32(0)
	if e.Reg.Flag(FlagC) {
		carry = 1
	}
	sum := uint32(hl) + uint32(value) + carry
	res := uint16(sum)

	f := byte(0)
	if (res & 0x8000) != 0 {
		f |= FlagS
	}
	if res == 0 {
		f |= FlagZ
	}
	if ((hl & 0x0FFF) + (value & 0x0FFF) + uint16(carry)) > 0x0FFF {
		f |= FlagH
	}
	if ((^(hl ^ value)) & (hl ^ res) & 0x8000) != 0 {
		f |= FlagPV
	}
	if sum > 0xFFFF {
		f |= FlagC
	}
	f |= byte((res >> 8) & 0x28)
	e.Reg.F = f
	e.Reg.SetHL(res)
}

func (e *Emulator) sbcHL(value uint16) {
	hl := e.Reg.HL()
	carry := uint32(0)
	if e.Reg.Flag(FlagC) {
		carry = 1
	}
	diff := int32(hl) - int32(value) - int32(carry)
	res := uint16(diff)

	f := FlagN
	if (res & 0x8000) != 0 {
		f |= FlagS
	}
	if res == 0 {
		f |= FlagZ
	}
	if int32(hl&0x0FFF)-int32(value&0x0FFF)-int32(carry) < 0 {
		f |= FlagH
	}
	if (((hl ^ value) & (hl ^ res)) & 0x8000) != 0 {
		f |= FlagPV
	}
	if diff < 0 {
		f |= FlagC
	}
	f |= byte((res >> 8) & 0x28)
	e.Reg.F = f
	e.Reg.SetHL(res)
}

func (e *Emulator) addA(v byte, withCarry bool) {
	a := e.Reg.A
	carryIn := uint16(0)
	if withCarry && e.Reg.Flag(FlagC) {
		carryIn = 1
	}
	sum := uint16(a) + uint16(v) + carryIn
	res := byte(sum)

	f := byte(0)
	if (res & 0x80) != 0 {
		f |= FlagS
	}
	if res == 0 {
		f |= FlagZ
	}
	if ((a & 0x0F) + (v & 0x0F) + byte(carryIn)) > 0x0F {
		f |= FlagH
	}
	if ((^(a ^ v)) & (a ^ res) & 0x80) != 0 {
		f |= FlagPV
	}
	if sum > 0xFF {
		f |= FlagC
	}
	f |= res & (FlagX | FlagY)
	e.Reg.A = res
	e.Reg.F = f
}

func (e *Emulator) subA(v byte, withCarry bool) {
	a := e.Reg.A
	carryIn := uint16(0)
	if withCarry && e.Reg.Flag(FlagC) {
		carryIn = 1
	}
	diff := int(a) - int(v) - int(carryIn)
	res := byte(diff)

	f := FlagN
	if (res & 0x80) != 0 {
		f |= FlagS
	}
	if res == 0 {
		f |= FlagZ
	}
	if int(a&0x0F)-int(v&0x0F)-int(carryIn) < 0 {
		f |= FlagH
	}
	if ((a ^ v) & (a ^ res) & 0x80) != 0 {
		f |= FlagPV
	}
	if diff < 0 {
		f |= FlagC
	}
	f |= res & (FlagX | FlagY)
	e.Reg.A = res
	e.Reg.F = f
}

func (e *Emulator) andA(v byte) {
	res := e.Reg.A & v
	e.Reg.A = res
	e.Reg.F = FlagH
	e.Reg.SetFlag(FlagS, (res&0x80) != 0)
	e.Reg.SetFlag(FlagZ, res == 0)
	e.Reg.SetFlag(FlagPV, parityEven(res))
	e.Reg.SetFlag(FlagX, (res&0x08) != 0)
	e.Reg.SetFlag(FlagY, (res&0x20) != 0)
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

func (e *Emulator) orA(v byte) {
	res := e.Reg.A | v
	e.Reg.A = res
	e.Reg.F = 0
	e.Reg.SetFlag(FlagS, (res&0x80) != 0)
	e.Reg.SetFlag(FlagZ, res == 0)
	e.Reg.SetFlag(FlagPV, parityEven(res))
	e.Reg.SetFlag(FlagX, (res&0x08) != 0)
	e.Reg.SetFlag(FlagY, (res&0x20) != 0)
}

func (e *Emulator) cpA(v byte) {
	a := e.Reg.A
	diff := int(a) - int(v)
	res := byte(diff)

	f := FlagN
	if (res & 0x80) != 0 {
		f |= FlagS
	}
	if res == 0 {
		f |= FlagZ
	}
	if int(a&0x0F)-int(v&0x0F) < 0 {
		f |= FlagH
	}
	if ((a ^ v) & (a ^ res) & 0x80) != 0 {
		f |= FlagPV
	}
	if diff < 0 {
		f |= FlagC
	}
	f |= v & (FlagX | FlagY)
	e.Reg.F = f
}

func (e *Emulator) inc8(v byte) byte {
	res := v + 1
	f := e.Reg.F & FlagC
	if (res & 0x80) != 0 {
		f |= FlagS
	}
	if res == 0 {
		f |= FlagZ
	}
	if (v&0x0F)+1 > 0x0F {
		f |= FlagH
	}
	if v == 0x7F {
		f |= FlagPV
	}
	f |= res & (FlagX | FlagY)
	e.Reg.F = f
	return res
}

func (e *Emulator) dec8(v byte) byte {
	res := v - 1
	f := (e.Reg.F & FlagC) | FlagN
	if (res & 0x80) != 0 {
		f |= FlagS
	}
	if res == 0 {
		f |= FlagZ
	}
	if (v & 0x0F) == 0 {
		f |= FlagH
	}
	if v == 0x80 {
		f |= FlagPV
	}
	f |= res & (FlagX | FlagY)
	e.Reg.F = f
	return res
}

func (e *Emulator) rlca() {
	a := e.Reg.A
	carry := (a >> 7) & 1
	res := (a << 1) | carry
	e.Reg.A = res
	f := e.Reg.F & (FlagS | FlagZ | FlagPV)
	if carry != 0 {
		f |= FlagC
	}
	f |= res & (FlagX | FlagY)
	e.Reg.F = f
}

func (e *Emulator) rrca() {
	a := e.Reg.A
	carry := a & 1
	res := (a >> 1) | (carry << 7)
	e.Reg.A = res
	f := e.Reg.F & (FlagS | FlagZ | FlagPV)
	if carry != 0 {
		f |= FlagC
	}
	f |= res & (FlagX | FlagY)
	e.Reg.F = f
}

func (e *Emulator) rla() {
	a := e.Reg.A
	carryIn := byte(0)
	if e.Reg.Flag(FlagC) {
		carryIn = 1
	}
	carryOut := (a >> 7) & 1
	res := (a << 1) | carryIn
	e.Reg.A = res
	f := e.Reg.F & (FlagS | FlagZ | FlagPV)
	if carryOut != 0 {
		f |= FlagC
	}
	f |= res & (FlagX | FlagY)
	e.Reg.F = f
}

func (e *Emulator) rra() {
	a := e.Reg.A
	carryIn := byte(0)
	if e.Reg.Flag(FlagC) {
		carryIn = 0x80
	}
	carryOut := a & 1
	res := (a >> 1) | carryIn
	e.Reg.A = res
	f := e.Reg.F & (FlagS | FlagZ | FlagPV)
	if carryOut != 0 {
		f |= FlagC
	}
	f |= res & (FlagX | FlagY)
	e.Reg.F = f
}

func (e *Emulator) cpl() {
	e.Reg.A = ^e.Reg.A
	f := e.Reg.F
	f |= FlagH | FlagN
	f = (f &^ (FlagX | FlagY)) | (e.Reg.A & (FlagX | FlagY))
	e.Reg.F = f
}

func (e *Emulator) scf() {
	f := e.Reg.F & (FlagS | FlagZ | FlagPV)
	f |= e.Reg.A & (FlagX | FlagY)
	f |= FlagC
	e.Reg.F = f
}

func (e *Emulator) ccf() {
	oldC := e.Reg.Flag(FlagC)
	f := e.Reg.F & (FlagS | FlagZ | FlagPV)
	if oldC {
		f |= FlagH
	}
	if !oldC {
		f |= FlagC
	}
	f |= e.Reg.A & (FlagX | FlagY)
	e.Reg.F = f
}

func (e *Emulator) neg() {
	a := e.Reg.A
	e.Reg.A = 0
	e.subA(a, false)
}

func (e *Emulator) exAF() {
	e.Reg.A, e.Reg.APrime = e.Reg.APrime, e.Reg.A
	e.Reg.F, e.Reg.FPrime = e.Reg.FPrime, e.Reg.F
}

func (e *Emulator) exx() {
	e.Reg.B, e.Reg.BPrime = e.Reg.BPrime, e.Reg.B
	e.Reg.C, e.Reg.CPrime = e.Reg.CPrime, e.Reg.C
	e.Reg.D, e.Reg.DPrime = e.Reg.DPrime, e.Reg.D
	e.Reg.E, e.Reg.EPrime = e.Reg.EPrime, e.Reg.E
	e.Reg.H, e.Reg.HPrime = e.Reg.HPrime, e.Reg.H
	e.Reg.L, e.Reg.LPrime = e.Reg.LPrime, e.Reg.L
}

func (e *Emulator) exDEHL() {
	de := e.Reg.DE()
	e.Reg.SetDE(e.Reg.HL())
	e.Reg.SetHL(de)
}

func (e *Emulator) exSPHL() {
	lo := e.Bus.Read(e.Reg.SP)
	hi := e.Bus.Read(e.Reg.SP + 1)
	hl := e.Reg.HL()
	e.Bus.Write(e.Reg.SP, byte(hl))
	e.Bus.Write(e.Reg.SP+1, byte(hl>>8))
	e.Reg.SetHL(uint16(lo) | (uint16(hi) << 8))
}

func (e *Emulator) ldi() {
	v := e.Bus.Read(e.Reg.HL())
	e.Bus.Write(e.Reg.DE(), v)
	e.Reg.SetHL(e.Reg.HL() + 1)
	e.Reg.SetDE(e.Reg.DE() + 1)
	e.Reg.SetBC(e.Reg.BC() - 1)

	szc := e.Reg.F & (FlagS | FlagZ | FlagC)
	f := szc
	if e.Reg.BC() != 0 {
		f |= FlagPV
	}
	sum := e.Reg.A + v
	f |= sum & (FlagX | FlagY)
	e.Reg.F = f
}

func (e *Emulator) ldd() {
	v := e.Bus.Read(e.Reg.HL())
	e.Bus.Write(e.Reg.DE(), v)
	e.Reg.SetHL(e.Reg.HL() - 1)
	e.Reg.SetDE(e.Reg.DE() - 1)
	e.Reg.SetBC(e.Reg.BC() - 1)

	szc := e.Reg.F & (FlagS | FlagZ | FlagC)
	f := szc
	if e.Reg.BC() != 0 {
		f |= FlagPV
	}
	sum := e.Reg.A + v
	f |= sum & (FlagX | FlagY)
	e.Reg.F = f
}

func (e *Emulator) cpi(reverse bool) {
	hl := e.Reg.HL()
	v := e.Bus.Read(hl)
	res := byte(int(e.Reg.A) - int(v))

	if reverse {
		e.Reg.SetHL(hl - 1)
	} else {
		e.Reg.SetHL(hl + 1)
	}
	e.Reg.SetBC(e.Reg.BC() - 1)

	f := e.Reg.F & FlagC
	f |= FlagN
	if (res & 0x80) != 0 {
		f |= FlagS
	}
	if res == 0 {
		f |= FlagZ
	}
	if int(e.Reg.A&0x0F)-int(v&0x0F) < 0 {
		f |= FlagH
	}
	if e.Reg.BC() != 0 {
		f |= FlagPV
	}
	adjust := res
	if (f & FlagH) != 0 {
		adjust--
	}
	f |= adjust & (FlagX | FlagY)
	e.Reg.F = f
}

func (e *Emulator) ini(reverse bool) {
	port := uint16(e.Reg.C) | (uint16(e.Reg.B) << 8)
	v := e.Bus.In(port)
	e.Bus.Write(e.Reg.HL(), v)
	if reverse {
		e.Reg.SetHL(e.Reg.HL() - 1)
	} else {
		e.Reg.SetHL(e.Reg.HL() + 1)
	}
	e.Reg.B--
	// Approximate flags for now; enough to keep flow and basic condition logic.
	f := e.Reg.F & FlagC
	if e.Reg.B == 0 {
		f |= FlagZ
	}
	if (e.Reg.B & 0x80) != 0 {
		f |= FlagS
	}
	e.Reg.F = f
}

func (e *Emulator) outi(reverse bool) {
	v := e.Bus.Read(e.Reg.HL())
	port := uint16(e.Reg.C) | (uint16(e.Reg.B) << 8)
	e.Bus.Out(port, v)
	if reverse {
		e.Reg.SetHL(e.Reg.HL() - 1)
	} else {
		e.Reg.SetHL(e.Reg.HL() + 1)
	}
	e.Reg.B--
	f := e.Reg.F & FlagC
	if e.Reg.B == 0 {
		f |= FlagZ
	}
	if (e.Reg.B & 0x80) != 0 {
		f |= FlagS
	}
	e.Reg.F = f
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
