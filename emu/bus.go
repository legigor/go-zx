package emu

// Bus represents the memory and I/O surface of a Z80 machine.
type Bus interface {
	Read(addr uint16) byte
	Write(addr uint16, value byte)
	In(port uint16) byte
	Out(port uint16, value byte)
}
