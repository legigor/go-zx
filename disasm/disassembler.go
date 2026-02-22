package disasm

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type partKind int

const (
	partFixed partKind = iota
	partCaptureN
	partCaptureD
	partCaptureNNLo
	partCaptureNNHi
)

type bytePart struct {
	kind  partKind
	value byte
}

type compiledInstruction struct {
	order         int
	template      string
	description   string
	category      string
	undocumented  bool
	parts         []bytePart
	vars          map[string]int
	varCount      int
	fixedCount    int
	wildcardCount int
}

type capture struct {
	hasN  bool
	n     byte
	hasD  bool
	d     byte
	hasNN bool
	nn    uint16

	nnLo byte
	nnHi byte
}

type Disassembler struct {
	index [256][]compiledInstruction
}

type Line struct {
	Address  int
	Bytes    []byte
	Mnemonic string
	Comment  string
	Unknown  bool
}

func New(specs []OpcodeSpec) (*Disassembler, error) {
	d := &Disassembler{}

	for i, spec := range specs {
		variants, err := compileVariants(i, spec)
		if err != nil {
			return nil, err
		}
		for _, v := range variants {
			if len(v.parts) == 0 || v.parts[0].kind != partFixed {
				continue
			}
			first := v.parts[0].value
			d.index[first] = append(d.index[first], v)
		}
	}

	for i := range d.index {
		sort.SliceStable(d.index[i], func(a, b int) bool {
			left := d.index[i][a]
			right := d.index[i][b]

			if len(left.parts) != len(right.parts) {
				return len(left.parts) > len(right.parts)
			}
			if left.fixedCount != right.fixedCount {
				return left.fixedCount > right.fixedCount
			}
			if left.wildcardCount != right.wildcardCount {
				return left.wildcardCount < right.wildcardCount
			}
			if left.varCount != right.varCount {
				return left.varCount < right.varCount
			}
			if left.undocumented != right.undocumented {
				return !left.undocumented
			}
			return left.order < right.order
		})
	}

	return d, nil
}

func (d *Disassembler) DecodeAt(mem []byte, pc int) Line {
	if pc >= len(mem) {
		return Line{Address: pc}
	}

	candidates := d.index[mem[pc]]
	for _, inst := range candidates {
		if pc+len(inst.parts) > len(mem) {
			continue
		}

		cap := capture{}
		matched := true
		for i, p := range inst.parts {
			b := mem[pc+i]
			switch p.kind {
			case partFixed:
				if b != p.value {
					matched = false
				}
			case partCaptureN:
				cap.hasN = true
				cap.n = b
			case partCaptureD:
				cap.hasD = true
				cap.d = b
			case partCaptureNNLo:
				cap.nnLo = b
			case partCaptureNNHi:
				cap.nnHi = b
				cap.hasNN = true
				cap.nn = uint16(cap.nnLo) | (uint16(cap.nnHi) << 8)
			}
			if !matched {
				break
			}
		}
		if !matched {
			continue
		}

		instrBytes := append([]byte(nil), mem[pc:pc+len(inst.parts)]...)
		return Line{
			Address:  pc,
			Bytes:    instrBytes,
			Mnemonic: renderMnemonic(inst, pc, cap),
			Comment:  renderComment(inst, cap),
		}
	}

	return Line{
		Address:  pc,
		Bytes:    []byte{mem[pc]},
		Mnemonic: fmt.Sprintf("DB $%02X", mem[pc]),
		Comment:  "Unknown opcode/data byte",
		Unknown:  true,
	}
}

func (d *Disassembler) Disassemble(mem []byte) []Line {
	lines := make([]Line, 0, len(mem)/2)
	for pc := 0; pc < len(mem); {
		line := d.DecodeAt(mem, pc)
		if len(line.Bytes) == 0 {
			break
		}
		lines = append(lines, line)
		pc += len(line.Bytes)
	}
	return lines
}

func FormatLine(line Line) string {
	out := fmt.Sprintf("%04X: %-12s %s", line.Address, formatBytes(line.Bytes), line.Mnemonic)
	if line.Comment == "" {
		return out
	}

	const commentColumn = 44
	if len(out) < commentColumn {
		out += strings.Repeat(" ", commentColumn-len(out))
	} else {
		out += " "
	}
	return out + "; " + line.Comment
}

func formatBytes(bytes []byte) string {
	parts := make([]string, len(bytes))
	for i, b := range bytes {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, " ")
}

func compileVariants(order int, spec OpcodeSpec) ([]compiledInstruction, error) {
	vars := collectVariables(spec)
	combos := generateCombinations(vars)
	if len(combos) == 0 {
		combos = []map[string]int{{}}
	}

	variants := make([]compiledInstruction, 0, len(combos))
	for _, combo := range combos {
		parts := make([]bytePart, 0, len(spec.Bytes)+1)
		fixedCount := 0
		wildCount := 0

		valid := true
		for _, token := range spec.Bytes {
			switch {
			case token == "n":
				parts = append(parts, bytePart{kind: partCaptureN})
				wildCount++
			case token == "d" || token == "d-$-2":
				parts = append(parts, bytePart{kind: partCaptureD})
				wildCount++
			case token == "nn":
				parts = append(parts,
					bytePart{kind: partCaptureNNLo},
					bytePart{kind: partCaptureNNHi},
				)
				wildCount += 2
			default:
				if isHexByte(token) {
					v, _ := strconv.ParseUint(token, 16, 8)
					parts = append(parts, bytePart{kind: partFixed, value: byte(v)})
					fixedCount++
					continue
				}

				value, ok := evalExpr(token, combo)
				if !ok || value < 0 || value > 0xFF {
					valid = false
					break
				}
				parts = append(parts, bytePart{kind: partFixed, value: byte(value)})
				fixedCount++
			}
		}
		if !valid || len(parts) == 0 {
			continue
		}

		variants = append(variants, compiledInstruction{
			order:         order,
			template:      spec.Mnemonic,
			description:   spec.Description,
			category:      spec.Category,
			undocumented:  spec.Undocumented,
			parts:         parts,
			vars:          combo,
			varCount:      len(combo),
			fixedCount:    fixedCount,
			wildcardCount: wildCount,
		})
	}

	if len(variants) == 0 {
		return nil, fmt.Errorf("no variants compiled for mnemonic %q", spec.Mnemonic)
	}

	return variants, nil
}

var exprVarRegex = regexp.MustCompile(`\b([a-z][a-z0-9]*)\b`)
var mnemonicVarRegex = regexp.MustCompile(`\b(b|dd|r|r1|r2|p)\b`)

func collectVariables(spec OpcodeSpec) []string {
	set := map[string]bool{}
	for _, token := range spec.Bytes {
		if token == "n" || token == "nn" || token == "d" || token == "d-$-2" || isHexByte(token) {
			continue
		}
		for _, m := range exprVarRegex.FindAllStringSubmatch(token, -1) {
			if isSupportedVariable(m[1]) {
				set[m[1]] = true
			}
		}
	}
	for _, m := range mnemonicVarRegex.FindAllStringSubmatch(spec.Mnemonic, -1) {
		set[m[1]] = true
	}

	vars := make([]string, 0, len(set))
	for k := range set {
		vars = append(vars, k)
	}
	sort.Strings(vars)
	return vars
}

func isSupportedVariable(name string) bool {
	switch name {
	case "b", "dd", "r", "r1", "r2", "p":
		return true
	default:
		return false
	}
}

func generateCombinations(vars []string) []map[string]int {
	if len(vars) == 0 {
		return nil
	}

	result := []map[string]int{{}}
	for _, v := range vars {
		domain := variableDomain(v)
		next := make([]map[string]int, 0, len(result)*len(domain))
		for _, base := range result {
			for _, value := range domain {
				clone := make(map[string]int, len(base)+1)
				for k, v := range base {
					clone[k] = v
				}
				clone[v] = value
				next = append(next, clone)
			}
		}
		result = next
	}
	return result
}

func variableDomain(name string) []int {
	switch name {
	case "dd":
		return []int{0, 1, 2, 3}
	case "p":
		return []int{0x00, 0x08, 0x10, 0x18, 0x20, 0x28, 0x30, 0x38}
	default:
		return []int{0, 1, 2, 3, 4, 5, 6, 7}
	}
}

func evalExpr(expr string, vars map[string]int) (int, bool) {
	expr = strings.ReplaceAll(expr, " ", "")
	parts := strings.Split(expr, "+")
	total := 0
	for _, p := range parts {
		switch {
		case strings.HasPrefix(p, "$"):
			v, err := strconv.ParseInt(p[1:], 16, 32)
			if err != nil {
				return 0, false
			}
			total += int(v)
		case strings.HasPrefix(p, "(") && strings.HasSuffix(p, ")") && strings.Contains(p, "<<"):
			inner := strings.TrimSuffix(strings.TrimPrefix(p, "("), ")")
			pair := strings.Split(inner, "<<")
			if len(pair) != 2 {
				return 0, false
			}
			name := pair[0]
			shift, err := strconv.Atoi(pair[1])
			if err != nil {
				return 0, false
			}
			value, ok := vars[name]
			if !ok {
				return 0, false
			}
			total += value << shift
		default:
			value, ok := vars[p]
			if !ok {
				return 0, false
			}
			total += value
		}
	}
	return total, true
}

func renderMnemonic(inst compiledInstruction, pc int, cap capture) string {
	m := inst.template

	if v, ok := inst.vars["dd"]; ok {
		dd := []string{"BC", "DE", "HL", "SP"}[v]
		m = replaceWord(m, "dd", dd)
	}
	if v, ok := inst.vars["b"]; ok {
		m = replaceWord(m, "b", strconv.Itoa(v))
	}
	if v, ok := inst.vars["p"]; ok {
		m = replaceWord(m, "p", fmt.Sprintf("$%02X", v))
	}

	for _, key := range []string{"r1", "r2", "r"} {
		if v, ok := inst.vars[key]; ok {
			m = replaceWord(m, key, registerName(inst, v))
		}
	}

	if cap.hasNN {
		m = replaceWord(m, "nn", fmt.Sprintf("$%04X", cap.nn))
	}
	if cap.hasN {
		m = replaceWord(m, "n", fmt.Sprintf("$%02X", cap.n))
	}
	if cap.hasD {
		m = replaceIndexedDisplacement(m, cap.d)
		if strings.HasPrefix(m, "JR") || strings.HasPrefix(m, "DJNZ") {
			target := uint16(int(pc) + len(inst.parts) + int(int8(cap.d)))
			m = replaceWord(m, "d", fmt.Sprintf("$%04X", target))
		} else {
			m = replaceWord(m, "d", formatSigned(cap.d))
		}
	}

	return m
}

func renderComment(inst compiledInstruction, cap capture) string {
	comment := inst.description
	if comment == "" {
		return ""
	}

	if v, ok := inst.vars["dd"]; ok {
		dd := []string{"BC", "DE", "HL", "SP"}[v]
		comment = strings.ReplaceAll(comment, "$dd", dd)
	}
	if v, ok := inst.vars["b"]; ok {
		comment = strings.ReplaceAll(comment, "$b", strconv.Itoa(v))
	}
	if v, ok := inst.vars["p"]; ok {
		comment = strings.ReplaceAll(comment, "$p", fmt.Sprintf("$%02X", v))
	}

	for _, key := range []string{"r1", "r2", "r"} {
		if v, ok := inst.vars[key]; ok {
			comment = strings.ReplaceAll(comment, "$"+key, registerName(inst, v))
		}
	}

	if cap.hasNN {
		comment = strings.ReplaceAll(comment, "$nn", fmt.Sprintf("$%04X", cap.nn))
	}
	if cap.hasN {
		comment = strings.ReplaceAll(comment, "$n", fmt.Sprintf("$%02X", cap.n))
	}
	if cap.hasD {
		comment = strings.ReplaceAll(comment, "$d", formatSigned(cap.d))
	}

	return comment
}

func registerName(inst compiledInstruction, index int) string {
	regs := []string{"B", "C", "D", "E", "H", "L", "(HL)", "A"}
	if inst.category == "ix" || strings.Contains(inst.template, "IX+d") {
		regs = []string{"B", "C", "D", "E", "IXH", "IXL", "(IX+d)", "A"}
	}
	if inst.category == "iy" || strings.Contains(inst.template, "IY+d") {
		regs = []string{"B", "C", "D", "E", "IYH", "IYL", "(IY+d)", "A"}
	}
	if index < 0 || index >= len(regs) {
		return "?"
	}
	return regs[index]
}

func replaceIndexedDisplacement(m string, d byte) string {
	disp := int8(d)
	signed := "+"
	value := byte(disp)
	if disp < 0 {
		signed = "-"
		value = byte(-disp)
	}
	m = strings.ReplaceAll(m, "IX+d", fmt.Sprintf("IX%s$%02X", signed, value))
	m = strings.ReplaceAll(m, "IY+d", fmt.Sprintf("IY%s$%02X", signed, value))
	return m
}

func formatSigned(d byte) string {
	disp := int8(d)
	if disp < 0 {
		return fmt.Sprintf("-$%02X", byte(-disp))
	}
	return fmt.Sprintf("+$%02X", byte(disp))
}

func replaceWord(input, word, replacement string) string {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`)
	escaped := strings.ReplaceAll(replacement, "$", "$$")
	return re.ReplaceAllString(input, escaped)
}

func isHexByte(token string) bool {
	if len(token) != 2 {
		return false
	}
	for i := 0; i < len(token); i++ {
		c := token[i]
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
