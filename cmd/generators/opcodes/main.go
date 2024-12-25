package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go-zx/pkg/opcodes"
	"log"
	"os"
	"strings"
	"text/template"
)

type OpcodeTable []OpcodeEntity

type OpcodeEntity struct {
	Bytes       []string `json:"bytes"`
	Cycles      string   `json:"cycles"`
	Description string   `json:"description"`
	Flags       struct {
		C  string `json:"c"`
		H  string `json:"h"`
		N  string `json:"n"`
		PV string `json:"p/v"`
		S  string `json:"s"`
		Z  string `json:"z"`
	} `json:"flags"`
	Mnemonic     string `json:"mnemonic"`
	Category     string `json:"category,omitempty"`
	Undocumented bool   `json:"undocumented,omitempty"`
	Z180         bool   `json:"z180,omitempty"`
	Reference    string `json:"reference,omitempty"`
}

var (
	registerOffsets = map[string]byte{
		"B": 0x00,
		"C": 0x01,
		"D": 0x02,
		"E": 0x03,
		"H": 0x04,
		"L": 0x05,
		"A": 0x07,
	}
)

func decodeByte(hexCode string) ([]byte, error) {

	if strings.HasPrefix(hexCode, "r+") {
		opcodeString := hexCode[3:]
		opcodeBases, err := hex.DecodeString(opcodeString)
		if err != nil {
			return nil, err
		}
		if len(opcodeBases) != 1 {
			return nil, fmt.Errorf("expected 1 byte, got %d", len(opcodeBases))
		}
		opcodeBase := opcodeBases[0]
		codes := []byte{}
		for _, regOffset := range registerOffsets {
			opcode := opcodeBase + regOffset
			codes = append(codes, opcode)
		}
		return codes, nil
	}

	byteValues, err := hex.DecodeString(hexCode)
	if err != nil {
		return nil, err
	}
	if len(byteValues) != 1 {
		return nil, fmt.Errorf("expected 1 byte, got %d", len(byteValues))
	}
	return byteValues, nil

}

func main() {
	inputFile := flag.String("input", "", "Path to the input JSON file")
	outputFile := flag.String("output", "", "Path to the output Go file")
	templateFile := "./cmd/generators/opcodes/template.tmpl" //flag.String("template", "template.go", "Path to the template file")
	flag.Parse()

	if *inputFile == "" || *outputFile == "" {
		log.Fatalf("Both -input and -output flags must be provided")
	}

	data, err := os.ReadFile(*inputFile)
	if err != nil {
		log.Fatalf("failed to read input file: %v", err)
	}

	var input OpcodeTable
	if err := json.Unmarshal(data, &input); err != nil {
		log.Fatalf("failed to unmarshal JSON: %v", err)
	}

	instructions := map[byte]opcodes.Instruction{}
	for _, entry := range input {
		hexCode := entry.Bytes[0]
		codeBytes, err := decodeByte(hexCode)
		if err != nil {
			log.Fatalf("failed to decode hex %s for `%s`: %v", hexCode, entry.Mnemonic, err)
		}
		for _, b := range codeBytes {
			if _, exists := instructions[b]; exists {
				log.Fatalf("duplicate opcode 0x%02X for `%s`", b, entry.Mnemonic)
			}
			instruction := opcodes.Instruction{
				Mnemonic: entry.Mnemonic,
				Length:   len(entry.Bytes),
			}
			instructions[b] = instruction
		}
	}

	tmpl, err := template.ParseFiles(templateFile)
	if err != nil {
		log.Fatalf("failed to parse template: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, input); err != nil {
		log.Fatalf("failed to execute template: %v", err)
	}

	if err := os.WriteFile(*outputFile, buf.Bytes(), 0644); err != nil {
		log.Fatalf("failed to write output file: %v", err)
	}
}
