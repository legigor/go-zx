package emu

const (
	FlagC  byte = 1 << 0
	FlagN  byte = 1 << 1
	FlagPV byte = 1 << 2
	FlagX  byte = 1 << 3
	FlagH  byte = 1 << 4
	FlagY  byte = 1 << 5
	FlagZ  byte = 1 << 6
	FlagS  byte = 1 << 7
)

// Registers holds the complete user-visible Z80 register file.
type Registers struct {
	A, F byte
	B, C byte
	D, E byte
	H, L byte

	APrime, FPrime byte
	BPrime, CPrime byte
	DPrime, EPrime byte
	HPrime, LPrime byte

	IX, IY uint16
	SP, PC uint16

	I byte
	R byte
}

func (r *Registers) AF() uint16 { return uint16(r.A)<<8 | uint16(r.F) }
func (r *Registers) BC() uint16 { return uint16(r.B)<<8 | uint16(r.C) }
func (r *Registers) DE() uint16 { return uint16(r.D)<<8 | uint16(r.E) }
func (r *Registers) HL() uint16 { return uint16(r.H)<<8 | uint16(r.L) }

func (r *Registers) SetAF(v uint16) { r.A = byte(v >> 8); r.F = byte(v) }
func (r *Registers) SetBC(v uint16) { r.B = byte(v >> 8); r.C = byte(v) }
func (r *Registers) SetDE(v uint16) { r.D = byte(v >> 8); r.E = byte(v) }
func (r *Registers) SetHL(v uint16) { r.H = byte(v >> 8); r.L = byte(v) }

func (r *Registers) Flag(mask byte) bool {
	return (r.F & mask) != 0
}

func (r *Registers) SetFlag(mask byte, enabled bool) {
	if enabled {
		r.F |= mask
		return
	}
	r.F &^= mask
}
