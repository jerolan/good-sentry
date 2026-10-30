package native

import (
	"encoding/binary"
	"testing"
)

// SystemInfo streams are assembled in directory order, exercising the same
// serialized boundary used for real Minidumps.
func arm64SignalSystemInfos(data []byte, infos ...[]byte) []byte {
	dir := binary.LittleEndian.Uint32(data[12:])
	count := binary.LittleEndian.Uint32(data[8:])
	entries := make([]byte, 0, int(count+uint32(len(infos)))*12)
	for i := uint32(0); i < count; i++ {
		entry := data[dir+i*12 : dir+(i+1)*12]
		if binary.LittleEndian.Uint32(entry) != streamSystemInfo {
			entries = append(entries, entry...)
		}
	}
	for _, info := range infos {
		rva := uint32(len(data))
		data = append(data, info...)
		entry := make([]byte, 12)
		binary.LittleEndian.PutUint32(entry, streamSystemInfo)
		binary.LittleEndian.PutUint32(entry[4:], uint32(len(info)))
		binary.LittleEndian.PutUint32(entry[8:], rva)
		entries = append(entries, entry...)
	}
	binary.LittleEndian.PutUint32(data[8:], uint32(len(entries)/12))
	binary.LittleEndian.PutUint32(data[12:], uint32(len(data)))
	return append(data, entries...)
}

func TestLinuxARM64SignalFallbackSystemInfoSelection(t *testing.T) {
	const pc, sp, restoredPC, restoredSP uint64 = 0x200001000, 0x5000, 0x100001100, 0x9000
	info := func(size int, platform uint32) []byte {
		b := make([]byte, size)
		binary.LittleEndian.PutUint16(b, cpuArchARM64)
		if size >= 24 {
			binary.LittleEndian.PutUint32(b[20:], platform)
		}
		return b
	}
	linux := info(24, 0x8201)
	other := info(24, 2)
	short := info(23, 0)
	code := make([]byte, 8)
	binary.LittleEndian.PutUint32(code, 0xd2801168)
	binary.LittleEndian.PutUint32(code[4:], 0xd4000001)
	signal := make([]byte, 576)
	binary.LittleEndian.PutUint64(signal[560:], restoredSP)
	binary.LittleEndian.PutUint64(signal[568:], restoredPC)
	for _, tc := range []struct {
		name         string
		infos        [][]byte
		wantFallback bool
	}{
		{"short SystemInfo", [][]byte{short}, false},
		{"Linux then short", [][]byte{linux, short}, false},
		{"Linux then truncated architecture", [][]byte{linux, {12}}, true},
		{"Linux then other OS", [][]byte{linux, other}, false},
		{"short then Linux", [][]byte{short, linux}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := arm64SignalMinidump(pc, sp, 0x8201, []arm64SignalRange{{pc, code}, {sp, signal}, {restoredSP, make([]byte, 16)}})
			crash, err := ParseMinidump(arm64SignalSystemInfos(data, tc.infos...))
			if err != nil {
				t.Fatal(err)
			}
			frames := UnwindCFI(&crash, nil)
			if got := len(frames) > 0; got != tc.wantFallback {
				t.Fatalf("frames=%+v, want fallback=%v", frames, tc.wantFallback)
			}
		})
	}
}

func TestLinuxARM64SignalFallbackRejectsMissingSystemInfo(t *testing.T) {
	data := arm64SignalMinidump(0x200001000, 0x5000, 0x8201, []arm64SignalRange{{0x5000, make([]byte, 576)}})
	if _, err := ParseMinidump(arm64SignalSystemInfos(data)); err == nil {
		t.Fatal("ARM64 context accepted without an architecture declaration")
	}
}

func TestLinuxARM64SignalFallbackRejectsWrappedInstructionAddress(t *testing.T) {
	const pc uint64 = ^uint64(0) - 3
	const sp uint64 = 0x5000
	first, second := make([]byte, 4), make([]byte, 4)
	binary.LittleEndian.PutUint32(first, 0xd2801168)
	binary.LittleEndian.PutUint32(second, 0xd4000001)
	signal := make([]byte, 576)
	binary.LittleEndian.PutUint64(signal[560:], 0x9000)
	binary.LittleEndian.PutUint64(signal[568:], 0x100001100)
	data := arm64SignalMinidump(pc, sp, 0x8201, []arm64SignalRange{{pc, first}, {0, second}, {sp, signal}})
	crash, err := ParseMinidump(data)
	if err != nil {
		t.Fatal(err)
	}
	if frames := UnwindCFI(&crash, nil); len(frames) != 0 {
		t.Fatalf("wrapped instruction address recognized: %+v", frames)
	}
}
