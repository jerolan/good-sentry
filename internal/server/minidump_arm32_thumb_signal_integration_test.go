//go:build integration

package server

import (
	"bytes"
	"crypto/sha1"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"goodsentry/internal/native"
	"goodsentry/internal/store"
)

// The initial frame has real ELF DWARF CFI that returns a tagged Thumb pointer.
func arm32ThumbSignalMinidump(t *testing.T, mode string) []byte {
	t.Helper()
	data := arm32SignalTieMinidump(t, "tie")
	thread := nativeTestMinidumpStreamRVA(data, 3)
	exception := nativeTestMinidumpStreamRVA(data, 6)
	module := nativeTestMinidumpStreamRVA(data, 4)
	memory := nativeTestMinidumpStreamRVA(data, 9)
	stack := binary.LittleEndian.Uint32(data[thread+40:])
	context := binary.LittleEndian.Uint32(data[thread+48:])
	code := binary.LittleEndian.Uint64(data[memory+8:])
	binary.LittleEndian.PutUint64(data[module+4:], 0x400000)
	binary.LittleEndian.PutUint32(data[context+4+15*4:], 0x401010)
	binary.LittleEndian.PutUint64(data[exception+24:], 0x401010)
	// CFA advances to SP+8 and retrieves the tagged return pointer at SP+4.
	copy(data[stack+8:stack+0x300], data[stack:stack+0x2f8])
	binary.LittleEndian.PutUint32(data[stack+4:], 0x6001003)
	binary.LittleEndian.PutUint64(data[memory+16:], 0x6001002)
	binary.LittleEndian.PutUint32(data[code:], 0xdf002777)
	if mode == "wrong code" {
		binary.LittleEndian.PutUint32(data[code:], 0xdf002778)
	}
	if mode == "invalid CPSR" {
		binary.LittleEndian.PutUint32(data[stack+8+32+16*4:], 0x30)
	}
	return data
}

func thumbHTTPBuildELF32DebugFrame(machine elf.Machine, sectionData []byte) []byte {
	const (
		elfHeaderSize     = 52
		sectionHeaderSize = 40
	)
	sectionNames := []byte("\x00.shstrtab\x00.debug_frame\x00")
	sectionNamesOffset := elfHeaderSize
	sectionDataOffset := (sectionNamesOffset + len(sectionNames) + 3) &^ 3
	sectionTableOffset := (sectionDataOffset + len(sectionData) + 3) &^ 3
	data := make([]byte, sectionTableOffset+3*sectionHeaderSize)
	copy(data[sectionNamesOffset:], sectionNames)
	copy(data[sectionDataOffset:], sectionData)
	copy(data[:4], []byte{0x7f, 'E', 'L', 'F'})
	data[4], data[5], data[6] = byte(elf.ELFCLASS32), byte(elf.ELFDATA2LSB), 1
	binary.LittleEndian.PutUint16(data[16:], uint16(elf.ET_REL))
	binary.LittleEndian.PutUint16(data[18:], uint16(machine))
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint32(data[32:], uint32(sectionTableOffset))
	binary.LittleEndian.PutUint16(data[40:], elfHeaderSize)
	binary.LittleEndian.PutUint16(data[46:], sectionHeaderSize)
	binary.LittleEndian.PutUint16(data[48:], 3)
	binary.LittleEndian.PutUint16(data[50:], 1)
	writeSection := func(index int, name, kind, offset, size, alignment uint32) {
		start := sectionTableOffset + index*sectionHeaderSize
		binary.LittleEndian.PutUint32(data[start:], name)
		binary.LittleEndian.PutUint32(data[start+4:], kind)
		binary.LittleEndian.PutUint32(data[start+16:], offset)
		binary.LittleEndian.PutUint32(data[start+20:], size)
		binary.LittleEndian.PutUint32(data[start+32:], alignment)
	}
	writeSection(1, 1, uint32(elf.SHT_STRTAB), uint32(sectionNamesOffset), uint32(len(sectionNames)), 1)
	writeSection(2, 11, uint32(elf.SHT_PROGBITS), uint32(sectionDataOffset), uint32(len(sectionData)), 4)
	return data
}

func thumbHTTPBuildArchDebugFrame(addressSize int, returnReg, cfaReg, cfaOffset, dataAlign byte, start uint64, size uint64) []byte {
	var section bytes.Buffer
	appendRecord := func(record []byte) int {
		origin := section.Len()
		var length [4]byte
		binary.LittleEndian.PutUint32(length[:], uint32(len(record)))
		section.Write(length[:])
		section.Write(record)
		return origin
	}
	cie := []byte{0xff, 0xff, 0xff, 0xff, 1, 0, 1, dataAlign, returnReg, 0x0c, cfaReg, cfaOffset, 0x05, returnReg, 1}
	cieOffset := appendRecord(cie)
	var fde bytes.Buffer
	fde.Write([]byte{byte(cieOffset), byte(cieOffset >> 8), byte(cieOffset >> 16), byte(cieOffset >> 24)})
	if addressSize == 4 {
		var field [4]byte
		binary.LittleEndian.PutUint32(field[:], uint32(start))
		fde.Write(field[:])
		binary.LittleEndian.PutUint32(field[:], uint32(size))
		fde.Write(field[:])
	} else {
		var field [8]byte
		binary.LittleEndian.PutUint64(field[:], start)
		fde.Write(field[:])
		binary.LittleEndian.PutUint64(field[:], size)
		fde.Write(field[:])
	}
	appendRecord(fde.Bytes())
	return section.Bytes()
}

func TestNativeARM32ThumbSignalPersistsRecoveredPC(t *testing.T) {
	for _, tc := range []struct {
		mode       string
		historical int
		want       []string
	}{
		{"recovered", 2, []string{"0x401010", "0x6001003", "0x6002000"}},
		{"wrong code", 2, []string{"0x401010", "0x40001050"}},
		{"invalid CPSR", 2, []string{"0x401010", "0x40001050"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			f := newResolutionHTTPFixture(t)
			dump := arm32ThumbSignalMinidump(t, tc.mode)
			crash, err := native.ParseMinidump(dump)
			if err != nil {
				t.Fatal(err)
			}
			if len(crash.Frames) != tc.historical || crash.Frames[1].Instruction != 0x40001050 {
				t.Fatalf("unexpected historical chain: %+v", crash.Frames)
			}
			section := thumbHTTPBuildArchDebugFrame(4, 14, 13, 8, 0x7c, 0x1000, 0x100)
			obj := thumbHTTPBuildELF32DebugFrame(elf.EM_ARM, section)
			hash := sha1.Sum(obj)
			access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
			if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, "app.pdb", "elf", "", "arm", "app.pdb", hex.EncodeToString(hash[:]), nil, nil, obj); err != nil {
				t.Fatal(err)
			}
			body, contentType := minidumpForm(t, dump, nil)
			req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good/api/"+f.project.ID+"/minidump/?sentry_key="+f.project.PublicKey, body)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", contentType)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("minidump HTTP status=%d", res.StatusCode)
			}
			var accepted struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(res.Body).Decode(&accepted); err != nil || accepted.ID == "" {
				t.Fatalf("response id=%q err=%v", accepted.ID, err)
			}
			events, err := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 100})
			if err != nil {
				t.Fatalf("stored events=%d err=%v", len(events), err)
			}
			var stored json.RawMessage
			for _, event := range events {
				var identity struct {
					EventID string `json:"event_id"`
				}
				if err := json.Unmarshal(event.Data, &identity); err != nil {
					t.Fatal(err)
				}
				if identity.EventID == accepted.ID {
					stored = event.Data
					break
				}
			}
			if len(stored) == 0 {
				t.Fatalf("event %s missing", accepted.ID)
			}
			var payload struct {
				Exception struct {
					Values []struct {
						Stacktrace struct {
							Frames []struct {
								Instruction string `json:"instruction_addr"`
							} `json:"frames"`
						} `json:"stacktrace"`
					} `json:"values"`
				} `json:"exception"`
			}
			if err := json.Unmarshal(stored, &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Exception.Values) != 1 {
				t.Fatalf("exceptions=%+v", payload.Exception)
			}
			frames := payload.Exception.Values[0].Stacktrace.Frames
			if len(frames) != len(tc.want) {
				t.Fatalf("persisted frames=%+v; want %v", frames, tc.want)
			}
			for i, want := range tc.want {
				if frames[i].Instruction != want {
					t.Fatalf("persisted frames=%+v; want %v", frames, tc.want)
				}
			}
		})
	}
}
