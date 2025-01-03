// Automatically generated with 'go generate' - DO NOT EDIT.

package opcodes

var Table = map[byte]Instruction{
	0x00: {"NOP", 1, ""},
	0x01: {"LD BC,nn", 3, "BC,0x%04X"},
	0x02: {"LD (BC),A", 1, "(BC),A"},
	0x03: {"INC BC", 1, "BC"},
	0x04: {"INC B", 1, "B"},
	0x05: {"DEC B", 1, "B"},
	0x06: {"LD B,n", 2, "B,0x%02X"},
	0x07: {"RLCA", 1, ""},
	0x08: {"EX AF,AF'", 1, ""},
	0x09: {"ADD HL,BC", 1, "HL,BC"},
	0x0A: {"LD A,(BC)", 1, "A,(BC)"},
	0x0B: {"DEC BC", 1, "BC"},
	0x0C: {"INC C", 1, "C"},
	0x0D: {"DEC C", 1, "C"},
	0x0E: {"LD C,n", 2, "C,0x%02X"},
	0x0F: {"RRCA", 1, ""},
	//... (continue for all opcodes)
}
