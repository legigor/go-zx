package emu

import "fmt"

const (
	romSize48K = 0x4000
	ramSize48K = 0x10000 - romSize48K
)

// Machine48K is a minimal ZX Spectrum 48K memory + I/O surface.
type Machine48K struct {
	rom [romSize48K]byte
	ram [ramSize48K]byte
}

func NewMachine48K(rom []byte) (*Machine48K, error) {
	if len(rom) != romSize48K {
		return nil, fmt.Errorf("invalid 48K ROM size: got=%d want=%d", len(rom), romSize48K)
	}

	m := &Machine48K{}
	copy(m.rom[:], rom)
	return m, nil
}

func (m *Machine48K) Read(addr uint16) byte {
	if addr < romSize48K {
		return m.rom[addr]
	}
	return m.ram[addr-romSize48K]
}

func (m *Machine48K) Write(addr uint16, value byte) {
	if addr < romSize48K {
		return // ROM is read-only
	}
	m.ram[addr-romSize48K] = value
}

func (m *Machine48K) In(_ uint16) byte {
	return 0xFF
}

func (m *Machine48K) Out(_ uint16, _ byte) {}

func (m *Machine48K) PeekRange(start uint16, n int) []byte {
	if n <= 0 {
		return nil
	}
	out := make([]byte, 0, n)
	addr := start
	for i := 0; i < n; i++ {
		out = append(out, m.Read(addr))
		addr++
	}
	return out
}
