package disasm

import (
	"encoding/json"
	"os"
)

// OpcodeSpec represents one instruction template entry from assets/opcode-table.json.
type OpcodeSpec struct {
	Bytes        []string `json:"bytes"`
	Mnemonic     string   `json:"mnemonic"`
	Description  string   `json:"description"`
	Category     string   `json:"category"`
	Undocumented bool     `json:"undocumented"`
}

func LoadOpcodeSpecs(path string) ([]OpcodeSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var specs []OpcodeSpec
	if err := json.Unmarshal(data, &specs); err != nil {
		return nil, err
	}
	return specs, nil
}

func NewFromFile(path string) (*Disassembler, error) {
	specs, err := LoadOpcodeSpecs(path)
	if err != nil {
		return nil, err
	}
	return New(specs)
}
