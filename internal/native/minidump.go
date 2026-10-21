// Package native parses public Minidump and Breakpad symbol formats. It does not
// contain copied server implementation from Sentry, Breakpad or GlitchTip.
package native

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

const (
	minidumpSignature = 0x504d444d
	minidumpVersion   = 0xa793
	streamThreadList  = 3
	streamModuleList  = 4
	streamException   = 6
	streamMemory64    = 9
	streamSystemInfo  = 7
	cpuArchAMD64      = 9
)

type Module struct {
	Name      string
	Base      uint64
	Size      uint32
	Timestamp uint32
	DebugID   string
}

type Frame struct {
	Instruction uint64
	Module      string
	Function    string
	File        string
	Line        int
}

type Crash struct {
	PID           uint32
	ThreadID      uint32
	Code          uint32
	CrashAddr     uint64
	RIP, RSP, RBP uint64
	Arch          string
	PointerSize   int
	Modules       []Module
	Frames        []Frame
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
				if size > uint64(len(data)) || base+size > uint64(len(data)) {
					return crash, errors.New("minidump memory range exceeds file")
				}
				ranges = append(ranges, memoryRange{addr, data[base : base+size]})
				base += size
			}
			if mr.err != nil {
				return crash, mr.err
			}
		case streamException:
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
		debugID := ""
		if entry.cvSize >= 24 && entry.cvRVA+entry.cvSize <= uint64(len(data)) {
			cv := data[entry.cvRVA : entry.cvRVA+entry.cvSize]
			if len(cv) >= 24 && string(cv[:4]) == "RSDS" {
				debugID = codeViewID(cv)
			}
		}
		crash.Modules = append(crash.Modules, Module{Name: name, Base: entry.base, Size: uint32(entry.size), Timestamp: uint32(entry.stamp), DebugID: debugID})
	}
	rawContext := contexts[crash.ThreadID]
	if len(rawContext) == 0 {
		rawContext = firstContext
	}
	switch len(rawContext) {
	case 1232:
		crash.Arch, crash.PointerSize = "amd64", 8
		crash.RSP = binary.LittleEndian.Uint64(rawContext[152:])
		crash.RBP = binary.LittleEndian.Uint64(rawContext[160:])
		crash.RIP = binary.LittleEndian.Uint64(rawContext[248:])
	case 716:
		crash.Arch, crash.PointerSize = "x86", 4
		crash.RBP = uint64(binary.LittleEndian.Uint32(rawContext[180:]))
		crash.RIP = uint64(binary.LittleEndian.Uint32(rawContext[184:]))
		crash.RSP = uint64(binary.LittleEndian.Uint32(rawContext[196:]))
	default:
		return Crash{}, fmt.Errorf("unsupported minidump context size %d (declared architecture %d)", len(rawContext), declaredArch)
	}
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
	return fmt.Sprintf("%08X%04X%04X%016X%X", binary.LittleEndian.Uint32(cv[4:]), binary.LittleEndian.Uint16(cv[8:]), binary.LittleEndian.Uint16(cv[10:]), binary.LittleEndian.Uint64(cv[12:]), binary.LittleEndian.Uint32(cv[20:]))
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
	if len(addresses) == 1 {
		if ret, ok := readPointer(ranges, crash.RSP, crash.PointerSize); ok && ret != 0 {
			addresses = append(addresses, ret)
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
	Start, End uint64
	Name       string
	File       string
	Line       int
}

type SymbolSet map[string][]SymbolFunction

func ParseBreakpad(name string, data []byte) (string, []SymbolFunction) {
	module := moduleKey(name)
	files := map[int]string{}
	var funcs []SymbolFunction
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
			start, err := strconv.ParseUint(fields[1], 16, 64)
			if err != nil {
				continue
			}
			var size uint64
			var nameAt int
			if fields[0] == "FUNC" && len(fields) >= 5 {
				size, _ = strconv.ParseUint(fields[2], 16, 64)
				nameAt = 4
			} else {
				nameAt = 3
			}
			if len(fields) <= nameAt {
				continue
			}
			if size == 0 {
				size = 1
			}
			funcs = append(funcs, SymbolFunction{Start: start, End: start + size, Name: strings.Join(fields[nameAt:], " "), Line: -1})
			current = &funcs[len(funcs)-1]
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
	sort.Slice(funcs, func(i, j int) bool { return funcs[i].Start < funcs[j].Start })
	return module, funcs
}

func moduleKey(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	return strings.ToLower(filepath.Base(name))
}

func Symbolicate(crash *Crash, symbols SymbolSet) {
	for i := range crash.Frames {
		frame := &crash.Frames[i]
		module := moduleFor(frame.Instruction, crash.Modules)
		if frame.Module == "" || module == nil {
			continue
		}
		offset := frame.Instruction - module.Base
		funcs := symbols[moduleKey(frame.Module)]
		for j := range funcs {
			f := &funcs[j]
			if offset >= f.Start && offset < f.End {
				frame.Function, frame.File, frame.Line = f.Name, f.File, f.Line
			}
		}
	}
}
