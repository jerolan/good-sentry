// Package native parses public Minidump and Breakpad symbol formats. It does not
// contain copied server implementation from Sentry, Breakpad or GlitchTip.
package native

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	minidumpExceptionLegacySize    = 32
	minidumpExceptionStreamSize    = 168
	minidumpExceptionContextOffset = 160
)

const (
	minidumpSignature      = 0x504d444d
	minidumpVersion        = 0xa793
	streamThreadList       = 3
	streamModuleList       = 4
	streamException        = 6
	streamMemory64         = 9
	streamSystemInfo       = 7
	cpuArchX86             = 0
	cpuArchARM             = 5
	cpuArchAMD64           = 9
	cpuArchARM64           = 12
	cpuArchARM64Old        = 0x8003
	cpuArchRISCV64         = 0x8006
	amd64ContextSize       = 1232
	x86ContextSize         = 716
	arm64OldContextSize    = 796
	arm64OldContextInteger = 0x80000002
	arm64OldContextFull    = 0x80000006
	maxMinidumpXState      = 16384
	minidumpContextXState  = 0x40
	// Crashpad's native struct size includes four bytes of tail alignment.
	riscv64ContextSize = 528
	riscv64ContextV1   = 1
	riscv64ContextInt  = 0x08000001
	riscv64ContextAll  = 0x08000003
)

type Module struct {
	Name        string
	Base        uint64
	Size        uint32
	Timestamp   uint32
	DebugID     string
	DebugFormat string
	DebugFile   string
	CodeID      string
}

type Frame struct {
	Instruction  uint64
	Module       string
	Function     string
	File         string
	Line         int
	InlineFrames []InlineFrame
}

type InlineFrame struct {
	Function string
	File     string
	Line     int
}

type Crash struct {
	PID               uint32
	ThreadID          uint32
	Code              uint32
	CrashAddr         uint64
	RIP, RSP, RBP, LR uint64
	Arch              string
	PointerSize       int
	Modules           []Module
	Frames            []Frame
	registers         map[uint32]uint64
	stack             []memoryRange
}

type memoryRange struct {
	start uint64
	data  []byte
}

type reader struct {
	b   []byte
	off int
	err error
}

func (r *reader) need(n int) bool {
	if r.err != nil {
		return false
	}
	if n < 0 || r.off < 0 || r.off > len(r.b)-n {
		r.err = errors.New("minidump is truncated")
		return false
	}
	return true
}

func (r *reader) u16() uint16 {
	if !r.need(2) {
		return 0
	}
	v := binary.LittleEndian.Uint16(r.b[r.off:])
	r.off += 2
	return v
}
func (r *reader) u32() uint32 {
	if !r.need(4) {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b[r.off:])
	r.off += 4
	return v
}
func (r *reader) u64() uint64 {
	if !r.need(8) {
		return 0
	}
	v := binary.LittleEndian.Uint64(r.b[r.off:])
	r.off += 8
	return v
}

type stream struct{ typ, rva, size uint32 }

func ParseMinidump(data []byte) (Crash, error) {
	var crash Crash
	if len(data) < 32 {
		return crash, errors.New("minidump header is truncated")
	}
	head := reader{b: data}
	if head.u32() != minidumpSignature || head.u16() != minidumpVersion {
		return crash, errors.New("invalid minidump signature or version")
	}
	_ = head.u16()
	count := head.u32()
	directory := int(head.u32())
	_ = head.u32()
	_ = head.u32()
	_ = head.u64()
	if count == 0 || count > 256 || directory < 32 || directory > len(data)-int(count)*12 {
		return crash, errors.New("invalid minidump stream directory")
	}
	streams := make([]stream, count)
	d := reader{b: data, off: directory}
	for i := range streams {
		typ, size, rva := d.u32(), d.u32(), d.u32()
		streams[i] = stream{typ, rva, size}
		if uint64(streams[i].rva)+uint64(streams[i].size) > uint64(len(data)) {
			return crash, errors.New("minidump stream exceeds file")
		}
	}
	if d.err != nil {
		return crash, d.err
	}
	var declaredArch uint16
	for _, s := range streams {
		if s.typ == streamSystemInfo && s.size >= 2 {
			declaredArch = binary.LittleEndian.Uint16(data[s.rva:])
		}
	}
	var ranges []memoryRange
	var firstContext []byte
	var exceptionContext []byte
	contexts := map[uint32][]byte{}
	var moduleEntries []struct{ base, size, stamp, nameRVA, cvRVA, cvSize uint64 }
	for _, s := range streams {
		body := data[s.rva : s.rva+s.size]
		switch s.typ {
		case streamThreadList:
			tr := reader{b: body}
			n := tr.u32()
			if n > 4096 {
				return crash, errors.New("too many minidump threads")
			}
			for i := uint32(0); i < n; i++ {
				threadID := tr.u32()
				tr.off += 12
				tr.off += 8
				stackStart := tr.u64()
				stackSize := tr.u32()
				stackRVA := tr.u32()
				contextSize := tr.u32()
				contextRVA := tr.u32()
				if stackSize > 0 && uint64(stackRVA)+uint64(stackSize) <= uint64(len(data)) {
					ranges = append(ranges, memoryRange{stackStart, data[stackRVA : stackRVA+stackSize]})
				}
				if contextSize >= 256 && uint64(contextRVA)+uint64(contextSize) <= uint64(len(data)) {
					context := data[contextRVA : contextRVA+contextSize]
					contexts[threadID] = context
					if len(firstContext) == 0 {
						firstContext = context
					}
				}
			}
			if tr.err != nil {
				return crash, tr.err
			}
		case streamMemory64:
			mr := reader{b: body}
			n := mr.u64()
			base := mr.u64()
			if n > 1_000_000 {
				return crash, errors.New("too many minidump memory ranges")
			}
			for i := uint64(0); i < n; i++ {
				addr, size := mr.u64(), mr.u64()
				fileSize := uint64(len(data))
				if base > fileSize || size > fileSize-base {
					return crash, errors.New("minidump memory range exceeds file")
				}
				end := base + size
				ranges = append(ranges, memoryRange{addr, data[int(base):int(end)]})
				base = end
			}
			if mr.err != nil {
				return crash, mr.err
			}
		case streamException:
			if len(body) != minidumpExceptionLegacySize && len(body) < minidumpExceptionStreamSize {
				return crash, errors.New("invalid minidump exception stream size")
			}
			exceptionContext = nil
			er := reader{b: body}
			crash.ThreadID = er.u32()
			er.off += 4
			crash.Code = er.u32()
			er.off += 4
			_ = er.u64()
			crash.CrashAddr = er.u64()
			if er.err != nil {
				return crash, er.err
			}
			if len(body) >= minidumpExceptionStreamSize {
				contextSize := binary.LittleEndian.Uint32(body[minidumpExceptionContextOffset:])
				contextRVA := binary.LittleEndian.Uint32(body[minidumpExceptionContextOffset+4:])
				if (contextSize == 0) != (contextRVA == 0) {
					return crash, errors.New("invalid minidump exception thread context descriptor")
				}
				if contextSize > 0 {
					contextEnd := uint64(contextRVA) + uint64(contextSize)
					if contextEnd > uint64(len(data)) {
						return crash, errors.New("minidump exception thread context exceeds file")
					}
					exceptionContext = data[int(contextRVA):int(contextEnd)]
				}
			}
		case streamModuleList:
			mr := reader{b: body}
			n := mr.u32()
			if n > 10000 {
				return crash, errors.New("too many minidump modules")
			}
			for i := uint32(0); i < n; i++ {
				base := mr.u64()
				size := mr.u32()
				_ = mr.u32()
				stamp := mr.u32()
				nameRVA := mr.u32()
				mr.off += 52
				cvSize, cvRVA := mr.u32(), mr.u32()
				miscSize, miscRVA := mr.u32(), mr.u32()
				_ = mr.u64()
				_ = mr.u64()
				_ = miscSize
				_ = miscRVA
				moduleEntries = append(moduleEntries, struct{ base, size, stamp, nameRVA, cvRVA, cvSize uint64 }{base, uint64(size), uint64(stamp), uint64(nameRVA), uint64(cvRVA), uint64(cvSize)})
			}
			if mr.err != nil {
				return crash, mr.err
			}
		}
	}
	for _, entry := range moduleEntries {
		name := readUTF16(data, entry.nameRVA)
		if name == "" {
			name = "unknown"
		}
		module := Module{Name: name, Base: entry.base, Size: uint32(entry.size), Timestamp: uint32(entry.stamp)}
		if entry.cvSize >= 4 && entry.cvRVA+entry.cvSize <= uint64(len(data)) {
			cv := data[entry.cvRVA : entry.cvRVA+entry.cvSize]
			module.DebugID = codeViewDebugID(cv)
			if module.DebugID != "" {
				switch binary.LittleEndian.Uint32(cv[:4]) {
				case 0x53445352: // RSDS
					module.DebugFormat = "pe"
					module.DebugFile = codeViewPDBFilename(cv[24:])
				case 0x3031424e: // NB10
					module.DebugFormat = "pe"
					module.DebugFile = codeViewPDBFilename(cv[16:])
				case 0x4270454c: // BpEL
					module.DebugFormat = "elf"
					module.CodeID = module.DebugID
				}
			}
		}
		crash.Modules = append(crash.Modules, module)
	}
	rawContext := exceptionContext
	if len(rawContext) == 0 {
		rawContext = contexts[crash.ThreadID]
	}
	if len(rawContext) == 0 {
		rawContext = firstContext
	}
	switch {
	case declaredArch == cpuArchAMD64 && len(rawContext) >= amd64ContextSize && len(rawContext) <= amd64ContextSize+maxMinidumpXState:
		if len(rawContext) > amd64ContextSize && binary.LittleEndian.Uint32(rawContext[48:])&minidumpContextXState == 0 {
			return Crash{}, errors.New("extended AMD64 minidump context lacks XSTATE flag")
		}
		crash.Arch, crash.PointerSize = "amd64", 8
		crash.RSP = binary.LittleEndian.Uint64(rawContext[152:])
		crash.RBP = binary.LittleEndian.Uint64(rawContext[160:])
		crash.RIP = binary.LittleEndian.Uint64(rawContext[248:])
	case declaredArch == cpuArchX86 && len(rawContext) >= x86ContextSize && len(rawContext) <= x86ContextSize+maxMinidumpXState:
		if len(rawContext) > x86ContextSize && binary.LittleEndian.Uint32(rawContext)&minidumpContextXState == 0 {
			return Crash{}, errors.New("extended x86 minidump context lacks XSTATE flag")
		}
		crash.Arch, crash.PointerSize = "x86", 4
		crash.RBP = uint64(binary.LittleEndian.Uint32(rawContext[180:]))
		crash.RIP = uint64(binary.LittleEndian.Uint32(rawContext[184:]))
		crash.RSP = uint64(binary.LittleEndian.Uint32(rawContext[196:]))
	default:
		if declaredArch == cpuArchRISCV64 && len(rawContext) == riscv64ContextSize {
			flags := binary.LittleEndian.Uint32(rawContext)
			version := binary.LittleEndian.Uint32(rawContext[4:])
			if version != riscv64ContextV1 || flags != riscv64ContextInt && flags != riscv64ContextAll {
				return Crash{}, errors.New("invalid RISC-V64 minidump context flags or version")
			}
			crash.Arch, crash.PointerSize = "riscv64", 8
			crash.RIP = binary.LittleEndian.Uint64(rawContext[8:])
			crash.registers = make(map[uint32]uint64, 32)
			crash.registers[0] = 0
			for register := uint32(1); register <= 31; register++ {
				offset := 16 + (register-1)*8
				crash.registers[register] = binary.LittleEndian.Uint64(rawContext[offset:])
			}
			crash.RSP = crash.registers[2]
			crash.RBP = crash.registers[8]
			crash.LR = crash.registers[1]
		} else if declaredArch == cpuArchARM && len(rawContext) == 368 {
			// Crashpad and Breakpad ARM32 contexts share the general-register offsets.
			crash.Arch, crash.PointerSize = "arm", 4
			crash.registers = make(map[uint32]uint64, 16)
			for register := uint32(0); register < 16; register++ {
				crash.registers[register] = uint64(binary.LittleEndian.Uint32(rawContext[4+4*register:]))
			}
			crash.RBP = uint64(binary.LittleEndian.Uint32(rawContext[48:]))
			crash.RSP = uint64(binary.LittleEndian.Uint32(rawContext[56:]))
			crash.LR = uint64(binary.LittleEndian.Uint32(rawContext[60:]))
			crash.RIP = uint64(binary.LittleEndian.Uint32(rawContext[64:]))
		} else if declaredArch == cpuArchARM64Old && len(rawContext) == arm64OldContextSize {
			flags := binary.LittleEndian.Uint64(rawContext)
			if flags != arm64OldContextInteger && flags != arm64OldContextFull {
				return Crash{}, errors.New("invalid legacy ARM64 minidump context flags")
			}
			crash.Arch, crash.PointerSize = "arm64", 8
			crash.RBP = binary.LittleEndian.Uint64(rawContext[240:])
			crash.RSP = binary.LittleEndian.Uint64(rawContext[256:])
			crash.RIP = binary.LittleEndian.Uint64(rawContext[264:])
		} else if declaredArch == cpuArchARM64 && len(rawContext) >= 272 {
			if len(rawContext) >= 4 && binary.LittleEndian.Uint32(rawContext)&0xffffff00 == 0x80000000 {
				return Crash{}, errors.New("legacy ARM64 minidump context conflicts with SystemInfo architecture")
			}
			crash.Arch, crash.PointerSize = "arm64", 8
			crash.RBP = binary.LittleEndian.Uint64(rawContext[240:])
			crash.RSP = binary.LittleEndian.Uint64(rawContext[256:])
			crash.RIP = binary.LittleEndian.Uint64(rawContext[264:])
		} else {
			return Crash{}, fmt.Errorf("unsupported minidump context size %d (declared architecture %d)", len(rawContext), declaredArch)
		}
	}
	crash.stack = ranges
	crash.Frames = unwind(crash, ranges)
	return crash, nil
}

func readUTF16(data []byte, rva uint64) string {
	if rva < 0 || rva+4 > uint64(len(data)) {
		return ""
	}
	n := int(binary.LittleEndian.Uint32(data[rva:]))
	if n < 0 || n > 4096 || n%2 != 0 || rva+4+uint64(n) > uint64(len(data)) {
		return ""
	}
	u := make([]uint16, n/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(data[int(rva)+4+i*2:])
	}
	return strings.TrimRight(string(utf16.Decode(u)), "\x00")
}

func codeViewID(cv []byte) string {
	if len(cv) < 24 {
		return ""
	}
	return fmt.Sprintf("%08X%04X%04X%016X%X", binary.LittleEndian.Uint32(cv[4:]), binary.LittleEndian.Uint16(cv[8:]), binary.LittleEndian.Uint16(cv[10:]), binary.BigEndian.Uint64(cv[12:]), binary.LittleEndian.Uint32(cv[20:]))
}

func codeViewDebugID(cv []byte) string {
	if len(cv) < 4 {
		return ""
	}
	switch binary.LittleEndian.Uint32(cv[:4]) {
	case 0x53445352: // RSDS: PDB GUID, age and path.
		return codeViewID(cv)
	case 0x3031424e: // NB10: PDB 2.0 timestamp, age and NUL-terminated path.
		if len(cv) < 17 || cv[len(cv)-1] != 0 {
			return ""
		}
		return fmt.Sprintf("%08X%x", binary.LittleEndian.Uint32(cv[8:]), binary.LittleEndian.Uint32(cv[12:]))
	case 0x4270454c: // BpEL: GNU ELF build ID from Crashpad/Breakpad.
		if len(cv) == 4 || len(cv) > 4+64 {
			return ""
		}
		return hex.EncodeToString(cv[4:])
	default:
		return ""
	}
}

func codeViewPDBFilename(path []byte) string {
	end := bytes.IndexByte(path, 0)
	if end < 0 {
		return ""
	}
	return safePDBFilename(string(path[:end]))
}

func safePDBFilename(path string) string {
	if len(path) == 0 || len(path) > 4096 || !utf8.ValidString(path) {
		return ""
	}
	name := path[strings.LastIndexAny(path, `/\`)+1:]
	if len(name) == 0 || len(name) > 255 || name == "." || name == ".." || strings.ContainsRune(name, ':') {
		return ""
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return name
}

func safeELFCodeID(id string) string {
	if len(id) == 0 || len(id) > 128 || len(id)%2 != 0 {
		return ""
	}
	if _, err := hex.DecodeString(id); err != nil {
		return ""
	}
	return strings.ToLower(id)
}

func readQword(ranges []memoryRange, addr uint64) (uint64, bool) {
	for _, r := range ranges {
		if addr >= r.start && addr+8 >= addr && addr+8 <= r.start+uint64(len(r.data)) {
			return binary.LittleEndian.Uint64(r.data[addr-r.start:]), true
		}
	}
	return 0, false
}

func moduleFor(addr uint64, modules []Module) *Module {
	for i := range modules {
		m := &modules[i]
		if addr >= m.Base && addr < m.Base+uint64(m.Size) {
			return m
		}
	}
	return nil
}

func unwind(crash Crash, ranges []memoryRange) []Frame {
	var addresses []uint64
	if crash.RIP != 0 {
		addresses = append(addresses, crash.RIP)
	}
	seen := map[uint64]bool{}
	for _, addr := range addresses {
		seen[addr] = true
	}
	bp := crash.RBP
	if crash.Arch == "riscv64" {
		for len(addresses) < 64 && crash.PointerSize == 8 && bp >= 16 {
			next, ok1 := readPointer(ranges, bp-16, 8)
			ret, ok2 := readPointer(ranges, bp-8, 8)
			if !ok1 || !ok2 || ret == 0 || seen[ret] || next != 0 && next <= bp {
				break
			}
			addresses = append(addresses, ret)
			seen[ret] = true
			if next == 0 {
				break
			}
			bp = next
		}
	} else {
		for len(addresses) < 64 && bp != 0 {
			next, ok1 := readPointer(ranges, bp, crash.PointerSize)
			ret, ok2 := readPointer(ranges, bp+uint64(crash.PointerSize), crash.PointerSize)
			if !ok1 || !ok2 || ret == 0 || seen[ret] || next != 0 && next <= bp {
				break
			}
			addresses = append(addresses, ret)
			seen[ret] = true
			if next == 0 {
				break
			}
			bp = next
		}
	}
	if len(addresses) == 1 {
		switch crash.Arch {
		case "arm", "riscv64":
			if crash.LR != 0 && !seen[crash.LR] {
				addresses = append(addresses, crash.LR)
			}
		default:
			if ret, ok := readPointer(ranges, crash.RSP, crash.PointerSize); ok && ret != 0 && !seen[ret] {
				addresses = append(addresses, ret)
			}
		}
	}
	frames := make([]Frame, 0, len(addresses))
	for _, addr := range addresses {
		frame := Frame{Instruction: addr}
		if m := moduleFor(addr, crash.Modules); m != nil {
			frame.Module = m.Name
		}
		frames = append(frames, frame)
	}
	return frames
}

func readPointer(ranges []memoryRange, addr uint64, size int) (uint64, bool) {
	if size == 8 {
		return readQword(ranges, addr)
	}
	for _, r := range ranges {
		if addr >= r.start && addr+4 <= r.start+uint64(len(r.data)) {
			return uint64(binary.LittleEndian.Uint32(r.data[addr-r.start:])), true
		}
	}
	return 0, false
}

type SymbolFunction struct {
	Start, End     uint64
	Name           string
	File           string
	Line           int
	inlineSites    *pdbInlineSiteSet
	procedureStart uint64
	publicFallback bool
	omapOrigin     uint64
	omapMapped     bool
}

type SymbolSet map[string][]SymbolFunction

func ParseBreakpad(name string, data []byte) (string, []SymbolFunction) {
	module := moduleKey(name)
	files := map[int]string{}
	var funcs []SymbolFunction
	var symbolStarts []uint64
	var current *SymbolFunction
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		line := strings.TrimSpace(string(raw))
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "MODULE":
			if len(fields) >= 5 {
				module = moduleKey(fields[len(fields)-1])
			}
		case "FILE":
			if len(fields) >= 3 {
				id, err := strconv.Atoi(fields[1])
				if err == nil {
					files[id] = strings.Join(fields[2:], " ")
				}
			}
		case "FUNC", "PUBLIC":
			current = nil
			fieldOffset := 1
			if fields[1] == "m" {
				fieldOffset++
			}
			if len(fields) <= fieldOffset {
				continue
			}
			start, err := strconv.ParseUint(fields[fieldOffset], 16, 64)
			if err != nil {
				continue
			}
			var size uint64
			var nameAt int
			publicFallback := fields[0] == "PUBLIC"
			if fields[0] == "FUNC" {
				nameAt = fieldOffset + 3
				if len(fields) <= nameAt {
					continue
				}
				parsedSize, err := strconv.ParseUint(fields[fieldOffset+1], 16, 64)
				if err != nil {
					continue
				}
				size = parsedSize
			} else {
				nameAt = fieldOffset + 2
				if len(fields) <= nameAt {
					continue
				}
			}
			if size == 0 {
				size = 1
			}
			funcs = append(funcs, SymbolFunction{Start: start, End: start + size, Name: strings.Join(fields[nameAt:], " "), Line: -1, publicFallback: publicFallback})
			symbolStarts = append(symbolStarts, start)
			if fields[0] == "FUNC" {
				current = &funcs[len(funcs)-1]
			}
		case "STACK", "INFO", "INLINE", "INLINE_END":
			continue
		default:
			if current != nil && len(fields) >= 4 {
				addr, err := strconv.ParseUint(fields[0], 16, 64)
				if err != nil {
					continue
				}
				size, err := strconv.ParseUint(fields[1], 16, 64)
				if err != nil {
					continue
				}
				lineNo, _ := strconv.Atoi(fields[2])
				fileIndex, _ := strconv.Atoi(fields[3])
				candidate := SymbolFunction{Start: addr, End: addr + size, Name: current.Name, File: files[fileIndex], Line: lineNo}
				funcs = append(funcs, candidate)
			}
		}
	}
	sort.Slice(symbolStarts, func(i, j int) bool { return symbolStarts[i] < symbolStarts[j] })
	uniqueStarts := symbolStarts[:0]
	for _, start := range symbolStarts {
		if len(uniqueStarts) == 0 || uniqueStarts[len(uniqueStarts)-1] != start {
			uniqueStarts = append(uniqueStarts, start)
		}
	}
	for i := range funcs {
		if !funcs[i].publicFallback {
			continue
		}
		nextStart := sort.Search(len(uniqueStarts), func(j int) bool { return uniqueStarts[j] > funcs[i].Start })
		if nextStart < len(uniqueStarts) {
			funcs[i].End = uniqueStarts[nextStart]
		} else {
			funcs[i].End = ^uint64(0)
		}
	}
	sort.SliceStable(funcs, func(i, j int) bool { return funcs[i].Start < funcs[j].Start })
	return module, funcs
}

func moduleKey(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	return strings.ToLower(filepath.Base(name))
}

func Symbolicate(crash *Crash, symbols SymbolSet) {
	for i := range crash.Frames {
		frame := &crash.Frames[i]
		frame.InlineFrames = nil
		module := moduleFor(frame.Instruction, crash.Modules)
		if frame.Module == "" || module == nil {
			continue
		}
		offset := frame.Instruction - module.Base
		funcs := symbols[moduleKey(frame.Module)]
		if module.DebugID != "" {
			funcs = append(funcs, symbols[DebugKey(module.DebugID)]...)
		}
		best := -1
		for j := range funcs {
			f := &funcs[j]
			if offset >= f.Start && offset < f.End {
				if best < 0 || symbolBetter(*f, funcs[best]) {
					best = j
				}
			}
		}
		if best >= 0 {
			f := funcs[best]
			frame.Function, frame.File, frame.Line = f.Name, f.File, f.Line
			procedureStart := f.procedureStart
			if procedureStart == 0 && !f.omapMapped {
				procedureStart = f.Start
			}
			originalOffset := offset
			if f.omapMapped {
				originalOffset = f.omapOrigin + offset - f.Start
			}
			for j := range funcs {
				candidate := &funcs[j]
				matchingProcedure := candidate.Start == procedureStart
				if f.omapMapped {
					matchingProcedure = candidate.omapMapped && candidate.procedureStart == procedureStart && offset >= candidate.Start && offset < candidate.End
				}
				if matchingProcedure && candidate.inlineSites != nil && originalOffset >= procedureStart {
					frame.InlineFrames = pdbInlineFramesAt(candidate.inlineSites, procedureStart, originalOffset-procedureStart)
					break
				}
			}
		}
	}
}

func symbolBetter(candidate, current SymbolFunction) bool {
	if candidate.publicFallback != current.publicFallback {
		return !candidate.publicFallback
	}
	candidateSource, currentSource := candidate.File != "" && candidate.Line > 0, current.File != "" && current.Line > 0
	if candidateSource != currentSource {
		return candidateSource
	}
	return candidate.End-candidate.Start < current.End-current.Start
}

func DebugKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer("-", "", "{", "", "}", "").Replace(value)
}
