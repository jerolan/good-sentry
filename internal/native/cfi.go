package native

import (
	"bytes"
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"errors"
	"sort"
)

const (
	cfiRuleSame = iota
	cfiRuleOffset
	cfiRuleValueOffset
	cfiRuleRegister
	cfiRuleExpression
	cfiRuleValueExpression
	cfiRuleUndefined
)

type UnwindSet map[string][]*UnwindTable

type UnwindTable struct {
	entries []cfiFDE
}

type cfiFDE struct {
	start, end   uint64
	instructions []byte
	cie          cfiCIE
}

type cfiCIE struct {
	codeAlign       uint64
	dataAlign       int64
	returnReg       uint32
	encoding        byte
	augmentation    string
	signalFrame     bool
	addrSize        int
	initialLocation uint64
	initial         cfiState
}

type cfiCFARule struct {
	register uint32
	offset   int64
	expr     []byte
	useExpr  bool
}

type cfiRegisterRule struct {
	kind  uint8
	value int64
	reg   uint32
	expr  []byte
}

type cfiExpressionResult struct {
	value      uint64
	stackValue bool
}

type cfiState struct {
	cfa  cfiCFARule
	regs map[uint32]cfiRegisterRule
}

func newCFIState() cfiState {
	return cfiState{regs: map[uint32]cfiRegisterRule{}}
}

func (s cfiState) clone() cfiState {
	result := cfiState{cfa: s.cfa, regs: make(map[uint32]cfiRegisterRule, len(s.regs))}
	for register, rule := range s.regs {
		result.regs[register] = rule
	}
	return result
}

type cfiCursor struct {
	data []byte
	off  int
	err  error
}

func (c *cfiCursor) need(n int) bool {
	if c.err != nil {
		return false
	}
	if n < 0 || c.off < 0 || c.off > len(c.data)-n {
		c.err = errors.New("CFI record is truncated")
		return false
	}
	return true
}

func (c *cfiCursor) byte() byte {
	if !c.need(1) {
		return 0
	}
	value := c.data[c.off]
	c.off++
	return value
}

func (c *cfiCursor) u16() uint16 {
	if !c.need(2) {
		return 0
	}
	value := binary.LittleEndian.Uint16(c.data[c.off:])
	c.off += 2
	return value
}

func (c *cfiCursor) u32() uint32 {
	if !c.need(4) {
		return 0
	}
	value := binary.LittleEndian.Uint32(c.data[c.off:])
	c.off += 4
	return value
}

func (c *cfiCursor) u64() uint64 {
	if !c.need(8) {
		return 0
	}
	value := binary.LittleEndian.Uint64(c.data[c.off:])
	c.off += 8
	return value
}

func (c *cfiCursor) uintN(size int) uint64 {
	if size <= 0 || size > 8 || !c.need(size) {
		c.err = errors.New("invalid CFI integer size")
		return 0
	}
	var value uint64
	for i := 0; i < size; i++ {
		value |= uint64(c.data[c.off+i]) << (8 * i)
	}
	c.off += size
	return value
}

func (c *cfiCursor) uleb() uint64 {
	var value uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if !c.need(1) {
			return 0
		}
		current := c.data[c.off]
		c.off++
		value |= uint64(current&0x7f) << shift
		if current&0x80 == 0 {
			return value
		}
	}
	c.err = errors.New("CFI ULEB128 is too large")
	return 0
}

func (c *cfiCursor) sleb() int64 {
	var value int64
	var shift uint
	var current byte
	for {
		if !c.need(1) {
			return 0
		}
		current = c.data[c.off]
		c.off++
		value |= int64(current&0x7f) << shift
		shift += 7
		if current&0x80 == 0 {
			break
		}
		if shift >= 64 {
			c.err = errors.New("CFI SLEB128 is too large")
			return 0
		}
	}
	if shift < 64 && current&0x40 != 0 {
		value |= ^int64(0) << shift
	}
	return value
}

func (c *cfiCursor) string() string {
	start := c.off
	for c.need(1) && c.data[c.off] != 0 {
		c.off++
	}
	if c.err != nil {
		return ""
	}
	value := string(c.data[start:c.off])
	c.off++
	return value
}

func ParseObjectUnwind(name string, data []byte) (string, *UnwindTable) {
	key := moduleKey(name)
	var table *UnwindTable
	if len(data) >= 4 && bytes.Equal(data[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		file, err := elf.NewFile(bytes.NewReader(data))
		if err != nil {
			return key, nil
		}
		base := elfImageBase(file)
		pointerSize := 8
		if file.Class == elf.ELFCLASS32 {
			pointerSize = 4
		}
		section := file.Section(".eh_frame")
		if section == nil {
			section = file.Section(".debug_frame")
		}
		if section != nil {
			content, err := section.Data()
			if err == nil {
				table, _ = parseCFISection(content, section.Addr, section.Name == ".eh_frame", pointerSize)
			}
		}
		if table != nil {
			table.normalize(base)
		}
	} else if len(data) >= 4 && isMachOMagic(data[:4]) {
		file, err := macho.NewFile(bytes.NewReader(data))
		if err != nil {
			return key, nil
		}
		base := machoImageBase(file)
		section := file.Section("__eh_frame")
		if section == nil {
			section = file.Section("__debug_frame")
		}
		if section != nil {
			content, err := section.Data()
			if err == nil {
				table, _ = parseCFISection(content, section.Addr, section.Name == "__eh_frame", 8)
			}
		}
		if table != nil {
			table.normalize(base)
		}
	}
	if table == nil || len(table.entries) == 0 {
		return key, nil
	}
	return key, table
}

func (t *UnwindTable) normalize(base uint64) {
	result := t.entries[:0]
	for _, entry := range t.entries {
		if entry.start < base || entry.end <= base {
			continue
		}
		entry.start -= base
		entry.end -= base
		result = append(result, entry)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].start < result[j].start })
	t.entries = result
}

func parseCFISection(data []byte, sectionAddr uint64, isEH bool, pointerSize int) (*UnwindTable, error) {
	if pointerSize != 4 && pointerSize != 8 {
		return nil, errors.New("invalid CFI pointer size")
	}
	cies := map[int]cfiCIE{}
	table := &UnwindTable{}
	for off := 0; off+4 <= len(data); {
		recordStart := off
		length := binary.LittleEndian.Uint32(data[off:])
		off += 4
		if length == 0 {
			break
		}
		headerSize := 4
		if length == 0xffffffff {
			if off+8 > len(data) {
				return nil, errors.New("CFI 64-bit length is truncated")
			}
			length64 := binary.LittleEndian.Uint64(data[off:])
			off += 8
			headerSize = 12
			if length64 > uint64(len(data)-off) {
				return nil, errors.New("CFI record exceeds section")
			}
			length = uint32(length64)
		}
		recordEnd := recordStart + headerSize + int(length)
		if recordEnd < off || recordEnd > len(data) || off+4 > recordEnd {
			return nil, errors.New("CFI record exceeds section")
		}
		idOffset := off
		id := binary.LittleEndian.Uint32(data[off:])
		off += 4
		isCIE := isEH && id == 0 || !isEH && id == 0xffffffff
		if isCIE {
			cie, err := parseCFIECIE(data[off:recordEnd], pointerSize, isEH)
			if err != nil {
				return nil, err
			}
			cies[recordStart] = cie
			off = recordEnd
			continue
		}
		cieOffset := int(id)
		if isEH {
			cieOffset = idOffset - int(id)
		}
		cie, ok := cies[cieOffset]
		if !ok {
			off = recordEnd
			continue
		}
		fde, err := parseCFIFDE(data[off:recordEnd], sectionAddr+uint64(off), cie)
		if err != nil {
			off = recordEnd
			continue
		}
		if fde.end > fde.start {
			table.entries = append(table.entries, fde)
		}
		off = recordEnd
	}
	return table, nil
}

func parseCFIECIE(data []byte, pointerSize int, isEH bool) (cfiCIE, error) {
	cursor := cfiCursor{data: data}
	var cie cfiCIE
	cie.addrSize = pointerSize
	version := cursor.byte()
	cie.augmentation = cursor.string()
	cie.codeAlign = cursor.uleb()
	cie.dataAlign = cursor.sleb()
	if version == 1 {
		cie.returnReg = uint32(cursor.byte())
	} else {
		cie.returnReg = uint32(cursor.uleb())
	}
	if version >= 4 {
		cie.addrSize = int(cursor.byte())
		_ = cursor.byte()
	}
	if cie.addrSize != 4 && cie.addrSize != 8 {
		return cfiCIE{}, errors.New("unsupported CIE address size")
	}
	if cie.codeAlign == 0 {
		cie.codeAlign = 1
	}
	if len(cie.augmentation) > 0 && cie.augmentation[0] == 'z' {
		augmentationValue := cursor.uleb()
		if augmentationValue > uint64(len(data)-cursor.off) {
			return cfiCIE{}, errors.New("CIE augmentation exceeds record")
		}
		augmentationLength := int(augmentationValue)
		augmentationEnd := cursor.off + augmentationLength
		if augmentationEnd < cursor.off || augmentationEnd > len(data) {
			return cfiCIE{}, errors.New("CIE augmentation exceeds record")
		}
		for i := 1; i < len(cie.augmentation); i++ {
			switch cie.augmentation[i] {
			case 'R':
				cie.encoding = cursor.byte()
			case 'P':
				encoding := cursor.byte()
				_, _, err := readCFIPointer(data, &cursor.off, encoding, 0, cie.addrSize)
				if err != nil {
					return cfiCIE{}, err
				}
			case 'L':
				_ = cursor.byte()
			case 'S':
				cie.signalFrame = isEH
			case 'B':
			default:
				return cfiCIE{}, errors.New("unsupported CIE augmentation")
			}
		}
		if cursor.err != nil || cursor.off != augmentationEnd {
			return cfiCIE{}, errors.New("invalid CIE augmentation")
		}
	}
	instructions := data[cursor.off:]
	cie.initial = newCFIState()
	initial, err := applyCFIInstructions(instructions, cie.initial, cie, 0, false)
	if err != nil {
		return cfiCIE{}, err
	}
	cie.initial = initial
	return cie, nil
}

func parseCFIFDE(data []byte, fieldAddress uint64, cie cfiCIE) (cfiFDE, error) {
	cursor := cfiCursor{data: data, off: 0}
	encoding := cie.encoding
	if encoding == 0 {
		encoding = 0x00
	}
	start, _, err := readCFIPointer(data, &cursor.off, encoding, fieldAddress, cie.addrSize)
	if err != nil {
		return cfiFDE{}, err
	}
	rangeEncoding := encoding & 0x0f
	addressRange, _, err := readCFIPointer(data, &cursor.off, rangeEncoding, 0, cie.addrSize)
	if err != nil {
		return cfiFDE{}, err
	}
	if len(cie.augmentation) > 0 && cie.augmentation[0] == 'z' {
		augmentationValue := cursor.uleb()
		if augmentationValue > uint64(len(data)-cursor.off) {
			return cfiFDE{}, errors.New("FDE augmentation exceeds record")
		}
		augmentationLength := int(augmentationValue)
		cursor.off += augmentationLength
		if cursor.err != nil || cursor.off > len(data) {
			return cfiFDE{}, errors.New("FDE augmentation exceeds record")
		}
	}
	return cfiFDE{start: start, end: start + addressRange, instructions: data[cursor.off:], cie: cie}, nil
}

func readCFIPointer(data []byte, offset *int, encoding byte, fieldAddress uint64, addrSize int) (uint64, int, error) {
	if encoding == 0xff {
		return 0, 0, errors.New("omit pointer encoding")
	}
	if encoding == 0 {
		encoding = 0x00
	}
	if encoding&0x80 != 0 {
		return 0, 0, errors.New("indirect CFI pointer is unsupported")
	}
	start := *offset
	cursor := cfiCursor{data: data, off: start}
	var value uint64
	switch encoding & 0x0f {
	case 0x00:
		value = cursor.uintN(addrSize)
	case 0x01:
		value = uint64(cursor.byte())
	case 0x02:
		value = uint64(cursor.u16())
	case 0x03:
		value = uint64(cursor.u32())
	case 0x04:
		value = cursor.u64()
	case 0x09:
		value = uint64(int64(int8(cursor.byte())))
	case 0x0a:
		value = uint64(int64(int16(cursor.u16())))
	case 0x0b:
		value = uint64(int64(int32(cursor.u32())))
	case 0x0c:
		value = cursor.u64()
	default:
		return 0, 0, errors.New("unsupported CFI pointer format")
	}
	if cursor.err != nil {
		return 0, 0, cursor.err
	}
	switch encoding & 0x70 {
	case 0x00:
	case 0x10:
		value += fieldAddress
	case 0x30:
		alignment := uint64(addrSize)
		if value%alignment != 0 {
			value += alignment - value%alignment
		}
	default:
		return 0, 0, errors.New("unsupported CFI pointer application")
	}
	*offset = cursor.off
	return value, cursor.off - start, nil
}

func applyCFIInstructions(instructions []byte, initial cfiState, cie cfiCIE, target uint64, useTarget bool) (cfiState, error) {
	state := initial.clone()
	location := uint64(0)
	if useTarget {
		location = cie.initialLocation
	}
	remembered := []cfiState{}
	cursor := cfiCursor{data: instructions}
	for cursor.off < len(cursor.data) {
		if useTarget && location > target {
			break
		}
		opcode := cursor.byte()
		high := opcode & 0xc0
		low := opcode & 0x3f
		switch high {
		case 0x40:
			location += uint64(low) * cie.codeAlign
			continue
		case 0x80:
			state.regs[uint32(low)] = cfiRegisterRule{kind: cfiRuleOffset, value: int64(cursor.uleb()) * cie.dataAlign}
			continue
		case 0xc0:
			if rule, ok := initial.regs[uint32(low)]; ok {
				state.regs[uint32(low)] = rule
			} else {
				delete(state.regs, uint32(low))
			}
			continue
		}
		switch opcode {
		case 0x00:
		case 0x01:
			location = cursor.uintN(cie.addrSize)
		case 0x02:
			location += uint64(cursor.byte()) * cie.codeAlign
		case 0x03:
			location += uint64(cursor.u16()) * cie.codeAlign
		case 0x04:
			location += uint64(cursor.u32()) * cie.codeAlign
		case 0x05:
			register := uint32(cursor.uleb())
			state.regs[register] = cfiRegisterRule{kind: cfiRuleOffset, value: int64(cursor.uleb()) * cie.dataAlign}
		case 0x06:
			register := uint32(cursor.uleb())
			if rule, ok := initial.regs[register]; ok {
				state.regs[register] = rule
			} else {
				delete(state.regs, register)
			}
		case 0x07:
			state.regs[uint32(cursor.uleb())] = cfiRegisterRule{kind: cfiRuleUndefined}
		case 0x08:
			state.regs[uint32(cursor.uleb())] = cfiRegisterRule{kind: cfiRuleSame}
		case 0x09:
			register := uint32(cursor.uleb())
			state.regs[register] = cfiRegisterRule{kind: cfiRuleRegister, reg: uint32(cursor.uleb())}
		case 0x0a:
			remembered = append(remembered, state.clone())
		case 0x0b:
			if len(remembered) == 0 {
				return state, errors.New("CFI restore-state underflow")
			}
			state = remembered[len(remembered)-1]
			remembered = remembered[:len(remembered)-1]
		case 0x0c:
			state.cfa = cfiCFARule{register: uint32(cursor.uleb()), offset: int64(cursor.uleb())}
		case 0x0d:
			state.cfa.register = uint32(cursor.uleb())
			state.cfa.useExpr = false
		case 0x0e:
			state.cfa.offset = int64(cursor.uleb())
			state.cfa.useExpr = false
		case 0x0f:
			length := int(cursor.uleb())
			if !cursor.need(length) {
				return state, cursor.err
			}
			state.cfa = cfiCFARule{expr: append([]byte(nil), cursor.data[cursor.off:cursor.off+length]...), useExpr: true}
			cursor.off += length
		case 0x10:
			register := uint32(cursor.uleb())
			length := int(cursor.uleb())
			if !cursor.need(length) {
				return state, cursor.err
			}
			state.regs[register] = cfiRegisterRule{kind: cfiRuleExpression, expr: append([]byte(nil), cursor.data[cursor.off:cursor.off+length]...)}
			cursor.off += length
		case 0x11:
			register := uint32(cursor.uleb())
			state.regs[register] = cfiRegisterRule{kind: cfiRuleOffset, value: cursor.sleb() * cie.dataAlign}
		case 0x12:
			state.cfa = cfiCFARule{register: uint32(cursor.uleb()), offset: cursor.sleb()}
		case 0x13:
			state.cfa.offset = cursor.sleb()
			state.cfa.useExpr = false
		case 0x14:
			register := uint32(cursor.uleb())
			state.regs[register] = cfiRegisterRule{kind: cfiRuleValueOffset, value: int64(cursor.uleb()) * cie.dataAlign}
		case 0x15:
			register := uint32(cursor.uleb())
			state.regs[register] = cfiRegisterRule{kind: cfiRuleValueOffset, value: cursor.sleb() * cie.dataAlign}
		case 0x16:
			register := uint32(cursor.uleb())
			length := int(cursor.uleb())
			if !cursor.need(length) {
				return state, cursor.err
			}
			state.regs[register] = cfiRegisterRule{kind: cfiRuleValueExpression, expr: append([]byte(nil), cursor.data[cursor.off:cursor.off+length]...)}
			cursor.off += length
		case 0x2e:
			_ = cursor.uleb()
		case 0x2f:
			register := uint32(cursor.uleb())
			state.regs[register] = cfiRegisterRule{kind: cfiRuleOffset, value: -int64(cursor.uleb()) * cie.dataAlign}
		default:
			return state, errors.New("unsupported CFI opcode")
		}
		if cursor.err != nil {
			return state, cursor.err
		}
	}
	return state, nil
}

func (t *UnwindTable) rulesAt(address uint64) (cfiState, cfiCIE, bool) {
	index := sort.Search(len(t.entries), func(i int) bool { return t.entries[i].end > address })
	if index >= len(t.entries) || address < t.entries[index].start {
		return cfiState{}, cfiCIE{}, false
	}
	entry := t.entries[index]
	cie := entry.cie
	state, err := applyCFIInstructionsWithLocation(entry.instructions, entry.cie.initial, cie, address, entry.start)
	if err != nil {
		return cfiState{}, cfiCIE{}, false
	}
	return state, cie, true
}

func applyCFIInstructionsWithLocation(instructions []byte, initial cfiState, cie cfiCIE, target, start uint64) (cfiState, error) {
	cie.initialLocation = start
	return applyCFIInstructions(instructions, initial, cie, target, true)
}

func UnwindCFI(crash *Crash, tables UnwindSet) []Frame {
	if crash == nil || len(crash.stack) == 0 || len(tables) == 0 || crash.RIP == 0 {
		return nil
	}
	pcRegister, spRegister, fpRegister := uint32(16), uint32(7), uint32(6)
	switch crash.Arch {
	case "x86":
		pcRegister, spRegister, fpRegister = 8, 4, 5
	case "arm64":
		pcRegister, spRegister, fpRegister = 32, 31, 29
	case "amd64":
	default:
		return nil
	}
	registers := map[uint32]uint64{pcRegister: crash.RIP, spRegister: crash.RSP, fpRegister: crash.RBP}
	pc, lookupPC := crash.RIP, crash.RIP
	seen := map[uint64]bool{crash.RIP: true}
	frames := make([]Frame, 0, 32)
	for len(frames) < 64 {
		module := moduleFor(lookupPC, crash.Modules)
		if module == nil {
			break
		}
		if lookupPC < module.Base {
			break
		}
		candidates := tables[moduleKey(module.Name)]
		if module.DebugID != "" {
			candidates = append(candidates, tables[DebugKey(module.DebugID)]...)
		}
		var rules cfiState
		var cie cfiCIE
		found := false
		for _, table := range candidates {
			if rules, cie, found = table.rulesAt(lookupPC - module.Base); found {
				break
			}
		}
		if !found {
			break
		}
		cfa, ok := evaluateCFA(rules, registers, crash.stack, crash.PointerSize)
		if !ok {
			break
		}
		frame := Frame{Instruction: pc, Module: module.Name}
		frames = append(frames, frame)
		returnAddress, ok := evaluateRegisterRule(cie.returnReg, rules, cfa, registers, crash.stack, crash.PointerSize)
		if !ok || returnAddress == 0 || seen[returnAddress] {
			break
		}
		nextFP, _ := evaluateRegisterRule(fpRegister, rules, cfa, registers, crash.stack, crash.PointerSize)
		nextSP := cfa
		if nextSP <= registers[spRegister] {
			break
		}
		seen[returnAddress] = true
		pc = returnAddress
		lookupPC = returnAddress
		if !cie.signalFrame {
			lookupPC--
		}
		registers[pcRegister] = returnAddress
		registers[spRegister] = nextSP
		registers[fpRegister] = nextFP
	}
	return frames
}

func evaluateCFA(state cfiState, registers map[uint32]uint64, ranges []memoryRange, pointerSize int) (uint64, bool) {
	if state.cfa.useExpr {
		result, ok := evaluateCFIExpressionResult(state.cfa.expr, registers, 0, ranges, pointerSize)
		return result.value, ok
	}
	register, ok := registers[state.cfa.register]
	if !ok {
		return 0, false
	}
	return addSigned(register, state.cfa.offset), true
}

func evaluateRegisterRule(register uint32, state cfiState, cfa uint64, registers map[uint32]uint64, ranges []memoryRange, pointerSize int) (uint64, bool) {
	rule, ok := state.regs[register]
	if !ok {
		return 0, false
	}
	switch rule.kind {
	case cfiRuleSame:
		value, ok := registers[register]
		return value, ok
	case cfiRuleOffset:
		return readPointer(ranges, addSigned(cfa, rule.value), pointerSize)
	case cfiRuleValueOffset:
		return addSigned(cfa, rule.value), true
	case cfiRuleRegister:
		value, ok := registers[rule.reg]
		return value, ok
	case cfiRuleExpression:
		result, ok := evaluateCFIExpressionResult(rule.expr, registers, cfa, ranges, pointerSize)
		if !ok {
			return 0, false
		}
		if result.stackValue {
			return result.value, true
		}
		return readPointer(ranges, result.value, pointerSize)
	case cfiRuleValueExpression:
		result, ok := evaluateCFIExpressionResult(rule.expr, registers, cfa, ranges, pointerSize)
		return result.value, ok
	default:
		return 0, false
	}
}

func addSigned(value uint64, offset int64) uint64 {
	if offset >= 0 {
		return value + uint64(offset)
	}
	return value - uint64(-offset)
}

func readCFIValue(ranges []memoryRange, address uint64, size, pointerSize int) (uint64, bool) {
	if (pointerSize != 4 && pointerSize != 8) || size < 1 || size > pointerSize {
		return 0, false
	}
	for _, memory := range ranges {
		if address < memory.start {
			continue
		}
		offset := address - memory.start
		if offset > uint64(len(memory.data)) || uint64(size) > uint64(len(memory.data))-offset {
			continue
		}
		var value uint64
		for i, b := range memory.data[int(offset) : int(offset)+size] {
			value |= uint64(b) << (8 * i)
		}
		return value, true
	}
	return 0, false
}

func evaluateCFIExpressionResult(expression []byte, registers map[uint32]uint64, cfa uint64, ranges []memoryRange, pointerSize int) (cfiExpressionResult, bool) {
	stackValue := len(expression) > 0 && expression[len(expression)-1] == 0x9f
	if stackValue {
		expression = expression[:len(expression)-1]
		if len(expression) == 0 {
			return cfiExpressionResult{}, false
		}
	}
	value, ok := evaluateCFIExpression(expression, registers, cfa, ranges, pointerSize)
	return cfiExpressionResult{value: value, stackValue: stackValue}, ok
}

func evaluateCFIExpression(expression []byte, registers map[uint32]uint64, cfa uint64, ranges []memoryRange, pointerSize int) (uint64, bool) {
	stack := make([]uint64, 0, 8)
	cursor := cfiCursor{data: expression}
	push := func(value uint64) bool {
		if len(stack) >= 64 {
			return false
		}
		stack = append(stack, value)
		return true
	}
	pop := func() (uint64, bool) {
		if len(stack) == 0 {
			return 0, false
		}
		value := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return value, true
	}
	for cursor.off < len(expression) {
		opcode := cursor.byte()
		switch {
		case opcode >= 0x30 && opcode <= 0x4f:
			if !push(uint64(opcode - 0x30)) {
				return 0, false
			}
			continue
		case opcode >= 0x50 && opcode <= 0x6f:
			value, ok := registers[uint32(opcode-0x50)]
			if !ok || !push(value) {
				return 0, false
			}
			continue
		case opcode >= 0x70 && opcode <= 0x8f:
			value, ok := registers[uint32(opcode-0x70)]
			if !ok || !push(addSigned(value, cursor.sleb())) {
				return 0, false
			}
			continue
		}
		switch opcode {
		case 0x03:
			if !push(cursor.uintN(pointerSize)) {
				return 0, false
			}
		case 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f:
			sizes := []int{1, 1, 2, 2, 4, 4, 8, 8}
			size := sizes[opcode-0x08]
			value := cursor.uintN(size)
			if opcode == 0x09 || opcode == 0x0b || opcode == 0x0d || opcode == 0x0f {
				switch size {
				case 1:
					value = uint64(int64(int8(value)))
				case 2:
					value = uint64(int64(int16(value)))
				case 4:
					value = uint64(int64(int32(value)))
				}
			}
			if !push(value) {
				return 0, false
			}
		case 0x10:
			if !push(cursor.uleb()) {
				return 0, false
			}
		case 0x11:
			if !push(uint64(cursor.sleb())) {
				return 0, false
			}
		case 0x12:
			if len(stack) == 0 || !push(stack[len(stack)-1]) {
				return 0, false
			}
		case 0x13:
			if _, ok := pop(); !ok {
				return 0, false
			}
		case 0x14:
			if len(stack) < 2 || !push(stack[len(stack)-2]) {
				return 0, false
			}
		case 0x15:
			depth := int(cursor.byte())
			if depth < 0 || depth >= len(stack) || !push(stack[len(stack)-1-depth]) {
				return 0, false
			}
		case 0x16:
			if len(stack) < 2 {
				return 0, false
			}
			stack[len(stack)-1], stack[len(stack)-2] = stack[len(stack)-2], stack[len(stack)-1]
		case 0x1a:
			right, ok1 := pop()
			left, ok2 := pop()
			if !ok1 || !ok2 || !push(left&right) {
				return 0, false
			}
		case 0x1c:
			right, ok1 := pop()
			left, ok2 := pop()
			if !ok1 || !ok2 || !push(left-right) {
				return 0, false
			}
		case 0x1e:
			right, ok1 := pop()
			left, ok2 := pop()
			if !ok1 || !ok2 || !push(left*right) {
				return 0, false
			}
		case 0x21:
			right, ok1 := pop()
			left, ok2 := pop()
			if !ok1 || !ok2 || !push(left|right) {
				return 0, false
			}
		case 0x22:
			right, ok1 := pop()
			left, ok2 := pop()
			if !ok1 || !ok2 || !push(left+right) {
				return 0, false
			}
		case 0x23:
			value, ok := pop()
			if !ok || !push(value+cursor.uleb()) {
				return 0, false
			}
		case 0x27:
			right, ok1 := pop()
			left, ok2 := pop()
			if !ok1 || !ok2 || !push(left^right) {
				return 0, false
			}
		case 0x2f:
			_ = cursor.u16()
		case 0x90:
			register := uint32(cursor.uleb())
			value, ok := registers[register]
			if !ok || !push(value) {
				return 0, false
			}
		case 0x92:
			register := uint32(cursor.uleb())
			value, ok := registers[register]
			if !ok || !push(addSigned(value, cursor.sleb())) {
				return 0, false
			}
		case 0x94:
			address, ok := pop()
			if !ok {
				return 0, false
			}
			size := int(cursor.byte())
			value, ok := readCFIValue(ranges, address, size, pointerSize)
			if cursor.err != nil || !ok || !push(value) {
				return 0, false
			}
		case 0x06:
			address, ok := pop()
			if !ok {
				return 0, false
			}
			value, ok := readCFIValue(ranges, address, pointerSize, pointerSize)
			if !ok || !push(value) {
				return 0, false
			}
		case 0x9c:
			if !push(cfa) {
				return 0, false
			}
		default:
			return 0, false
		}
		if cursor.err != nil {
			return 0, false
		}
	}
	return pop()
}
