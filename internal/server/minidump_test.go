//go:build integration

package server

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"debug/dwarf"
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"goodsentry/internal/store"
	"goodsentry/internal/testfixture/nativepdb"
)

func minidumpForm(t *testing.T, content []byte, extra map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	file, err := w.CreateFormFile("upload_file_minidump", "crash.dmp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
	for name, value := range extra {
		if err := w.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, w.FormDataContentType()
}

func TestNativeMinidumpSymbolicationAndIngestion(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	symbol := []byte("MODULE windows x86_64 TEST test_app.pdb\nFILE 1 C:\\src\\crash.cc\nFUNC 1010 20 0 CrashFunction\n1010 20 42 1\n")
	digest := sha1.Sum(symbol)
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, "test_app.sym", "sym", "5A9832E5287241C1838ED98914E9B7FF1", "x86_64", "test_app.pdb", hex.EncodeToString(digest[:]), nil, nil, symbol); err != nil {
		t.Fatal(err)
	}
	dump, err := os.ReadFile("testdata/amd64-minidump.dmp")
	if err != nil {
		t.Fatal(err)
	}
	body, contentType := minidumpForm(t, dump, map[string]string{"sentry[release]": "native@1", "sentry[environment]": "production", "crash_context": "unit"})
	req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good/api/"+f.project.ID+"/minidump/?sentry_version=7&sentry_key="+f.project.PublicKey, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var accepted struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&accepted); err != nil || res.StatusCode != http.StatusOK || accepted.ID == "" {
		t.Fatalf("minidump response=%d %+v %v", res.StatusCode, accepted, err)
	}
	events, err := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 100})
	if err != nil || len(events) == 0 {
		t.Fatalf("stored minidump event: %d %v", len(events), err)
	}
	var payload struct {
		Release     string `json:"release"`
		Environment string `json:"environment"`
		Exception   struct {
			Values []struct {
				Type       string `json:"type"`
				Stacktrace struct {
					Frames []struct {
						Function string `json:"function"`
						Filename string `json:"filename"`
						Line     int    `json:"lineno"`
					} `json:"frames"`
				} `json:"stacktrace"`
			} `json:"values"`
		} `json:"exception"`
		DebugMeta struct {
			Images []struct {
				CodeFile  string `json:"code_file"`
				ImageAddr string `json:"image_addr"`
			} `json:"images"`
		} `json:"debug_meta"`
	}
	if err := json.Unmarshal(events[0].Data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Exception.Values) != 1 || payload.Exception.Values[0].Type != "EXCEPTION_ACCESS_VIOLATION" || len(payload.Exception.Values[0].Stacktrace.Frames) < 2 {
		t.Fatalf("native exception projection: %+v", payload.Exception)
	}
	first := payload.Exception.Values[0].Stacktrace.Frames[0]
	if first.Function != "CrashFunction" || first.Filename != "C:\\src\\crash.cc" || first.Line != 42 {
		t.Fatalf("native frame symbols: %+v", first)
	}
	if len(payload.DebugMeta.Images) == 0 || !strings.Contains(payload.DebugMeta.Images[0].CodeFile, "test_app.pdb") || payload.DebugMeta.Images[0].ImageAddr != "0x140000000" {
		t.Fatalf("native debug metadata: %+v", payload.DebugMeta)
	}
	files, err := f.db.DebugFiles(f.ctx, access, f.project.ID, 0, 10)
	if err != nil || len(files) != 1 {
		t.Fatalf("symbol storage should contain only the uploaded symbol: %+v %v", files, err)
	}

	badBody, badType := minidumpForm(t, []byte("not a minidump"), nil)
	req, _ = http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good/api/"+f.project.ID+"/minidump/?sentry_version=7&sentry_key="+f.project.PublicKey, badBody)
	req.Header.Set("Content-Type", badType)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid minidump=%d", res.StatusCode)
	}
	body, contentType = minidumpForm(t, dump, nil)
	req, _ = http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good/api/"+f.project.ID+"/minidump/?sentry_version=7&sentry_key=wrong", body)
	req.Header.Set("Content-Type", contentType)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("invalid minidump DSN=%d", res.StatusCode)
	}
}

func TestNativePDBSymbolicationUsesEmbeddedDebugID(t *testing.T) {
	const embeddedID = "5A9832E5287241C1838ED98914E9B7FF1"
	for _, test := range []struct {
		name       string
		uploadedID string
		wantSymbol bool
	}{
		{name: "matching identity", uploadedID: embeddedID, wantSymbol: true},
		{name: "mismatched uploaded identity", uploadedID: "5A9832E5287241C1838ED98914E9B7FF2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newResolutionHTTPFixture(t)
			pdb := nativepdb.Minimal().Data
			digest := sha1.Sum(pdb)
			access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
			if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, "crash.pdb", "pdb", test.uploadedID, "x86_64", "crash.exe", hex.EncodeToString(digest[:]), nil, nil, pdb); err != nil {
				t.Fatal(err)
			}

			minidump := buildNativeModuleMinidump(t, "C:\\test\\crash.exe", embeddedID, 0x1015)
			body, contentType := minidumpForm(t, minidump, nil)
			req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good/api/"+f.project.ID+"/minidump/?sentry_version=7&sentry_key="+f.project.PublicKey, body)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", contentType)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			var accepted struct {
				ID string `json:"id"`
			}
			if err := json.NewDecoder(res.Body).Decode(&accepted); err != nil || res.StatusCode != http.StatusOK || accepted.ID == "" {
				t.Fatalf("PDB minidump response=%d %+v %v", res.StatusCode, accepted, err)
			}

			events, err := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 10})
			if err != nil || len(events) == 0 {
				t.Fatalf("stored PDB minidump event: %d %v", len(events), err)
			}
			var payload struct {
				Exception struct {
					Values []struct {
						Stacktrace struct {
							Frames []nativeFrame `json:"frames"`
						} `json:"stacktrace"`
					} `json:"values"`
				} `json:"exception"`
			}
			if err := json.Unmarshal(events[0].Data, &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Exception.Values) != 1 || len(payload.Exception.Values[0].Stacktrace.Frames) == 0 {
				t.Fatalf("PDB minidump frames: %+v", payload.Exception)
			}
			gotSymbol := payload.Exception.Values[0].Stacktrace.Frames[0].Function == "CrashFunction"
			if gotSymbol != test.wantSymbol {
				t.Fatalf("PDB symbolication=%+v, want symbol=%t", payload.Exception.Values[0].Stacktrace.Frames[0], test.wantSymbol)
			}
		})
	}
}

const nativeTestDebugID = "11223344-5566-7788-aabb-ccddeeff00111"

type nativeFrame struct {
	Function string `json:"function"`
	Filename string `json:"filename"`
	Line     int    `json:"lineno"`
}

func buildNativeModuleMinidump(t *testing.T, moduleName, debugID string, functionOffset uint64, stackData ...[]byte) []byte {
	t.Helper()
	const moduleBase = uint64(0x0000000140000000)
	crashAddress := moduleBase + functionOffset
	context := make([]byte, 1232)
	binary.LittleEndian.PutUint64(context[152:], 0x200000)
	binary.LittleEndian.PutUint64(context[160:], 0x200000)
	binary.LittleEndian.PutUint64(context[248:], crashAddress)
	stack := make([]byte, 0x100)
	if len(stackData) > 0 {
		copy(stack, stackData[0])
	}
	modules := make([]byte, 4+108)
	binary.LittleEndian.PutUint32(modules, 1)
	binary.LittleEndian.PutUint64(modules[4:], moduleBase)
	binary.LittleEndian.PutUint32(modules[12:], 0x10000)
	binary.LittleEndian.PutUint32(modules[4+76:], 24)
	threads := make([]byte, 4+48)
	binary.LittleEndian.PutUint32(threads, 1)
	binary.LittleEndian.PutUint32(threads[4:], 7)
	exception := make([]byte, 32)
	binary.LittleEndian.PutUint32(exception, 7)
	binary.LittleEndian.PutUint32(exception[8:], 0xC0000005)
	binary.LittleEndian.PutUint64(exception[24:], crashAddress)
	system := make([]byte, 2)
	binary.LittleEndian.PutUint16(system, 9)
	streams := [][]byte{system, threads, modules, exception}
	buf := make([]byte, 32+len(streams)*12)
	appendData := func(value []byte) uint32 {
		rva := uint32(len(buf))
		buf = append(buf, value...)
		return rva
	}
	encodeUTF16 := func(value string) []byte {
		out := make([]byte, 4+len(value)*2)
		binary.LittleEndian.PutUint32(out, uint32(len(value)*2))
		for i, character := range value {
			binary.LittleEndian.PutUint16(out[4+i*2:], uint16(character))
		}
		return out
	}
	stackRVA := appendData(stack)
	contextRVA := appendData(context)
	binary.LittleEndian.PutUint32(modules[4+20:], appendData(encodeUTF16(moduleName)))
	codeView := make([]byte, 24)
	copy(codeView, "RSDS")
	if err := setCodeViewDebugID(codeView, debugID); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(modules[4+80:], appendData(codeView))
	binary.LittleEndian.PutUint64(threads[28:], 0x200000)
	binary.LittleEndian.PutUint32(threads[36:], uint32(len(stack)))
	binary.LittleEndian.PutUint32(threads[40:], stackRVA)
	binary.LittleEndian.PutUint32(threads[44:], uint32(len(context)))
	binary.LittleEndian.PutUint32(threads[48:], contextRVA)
	types := []uint32{7, 3, 4, 6}
	locations := make([]struct{ typ, size, rva uint32 }, 0, len(streams))
	for i, stream := range streams {
		locations = append(locations, struct{ typ, size, rva uint32 }{types[i], uint32(len(stream)), appendData(stream)})
	}
	copy(buf, "MDMP")
	binary.LittleEndian.PutUint16(buf[4:], 0xa793)
	binary.LittleEndian.PutUint32(buf[8:], uint32(len(streams)))
	binary.LittleEndian.PutUint32(buf[12:], 32)
	for i, location := range locations {
		offset := 32 + i*12
		binary.LittleEndian.PutUint32(buf[offset:], location.typ)
		binary.LittleEndian.PutUint32(buf[offset+4:], location.size)
		binary.LittleEndian.PutUint32(buf[offset+8:], location.rva)
	}
	return buf
}

func setCodeViewDebugID(codeView []byte, debugID string) error {
	debugID = strings.ToUpper(strings.NewReplacer("-", "", "{", "", "}", "").Replace(debugID))
	if len(debugID) < 33 || len(debugID) > 40 {
		return fmt.Errorf("invalid fixture debug ID %q", debugID)
	}
	fields := []struct {
		start, end int
		bits       int
	}{
		{0, 8, 32}, {8, 12, 16}, {12, 16, 16}, {16, 32, 64}, {32, len(debugID), 32},
	}
	values := make([]uint64, len(fields))
	for i, field := range fields {
		value, err := strconv.ParseUint(debugID[field.start:field.end], 16, field.bits)
		if err != nil {
			return err
		}
		values[i] = value
	}
	binary.LittleEndian.PutUint32(codeView[4:], uint32(values[0]))
	binary.LittleEndian.PutUint16(codeView[8:], uint16(values[1]))
	binary.LittleEndian.PutUint16(codeView[10:], uint16(values[2]))
	binary.LittleEndian.PutUint64(codeView[12:], values[3])
	binary.LittleEndian.PutUint32(codeView[20:], uint32(values[4]))
	return nil
}

func ingestNativeObjectFrames(t *testing.T, f *resolutionHTTPFixture, object []byte, objectName, symbolType string, functionOffset uint64, expectedFunction string, stackData ...[]byte) []nativeFrame {
	t.Helper()
	digest := sha1.Sum(object)
	access := store.ManagementAccess{UserID: f.owner.ID, OrganizationID: f.org.ID, AllProjects: true}
	if _, err := f.db.SaveDebugFile(f.ctx, access, f.project.ID, objectName, symbolType, nativeTestDebugID, "x86_64", objectName, hex.EncodeToString(digest[:]), nil, nil, object); err != nil {
		t.Fatal(err)
	}
	buf := buildNativeModuleMinidump(t, "C:\\test\\different.module", nativeTestDebugID, functionOffset, stackData...)
	body, contentType := minidumpForm(t, buf, nil)
	req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.server.URL+"/good/api/"+f.project.ID+"/minidump/?sentry_version=7&sentry_key="+f.project.PublicKey, body)
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
		t.Fatalf("native object minidump response=%d", res.StatusCode)
	}
	events, err := f.db.CompatibleEvents(f.ctx, access, store.EventProjectTarget(f.project.ID), store.CompatibleEventFilter{Limit: 10})
	if err != nil || len(events) == 0 {
		t.Fatalf("stored native object event: %d %v", len(events), err)
	}
	var payload struct {
		Exception struct {
			Values []struct {
				Stacktrace struct {
					Frames []nativeFrame `json:"frames"`
				} `json:"stacktrace"`
			} `json:"values"`
		} `json:"exception"`
		DebugMeta struct {
			Images []struct {
				DebugID string `json:"debug_id"`
			} `json:"images"`
		} `json:"debug_meta"`
	}
	found := false
	for _, event := range events {
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Exception.Values) == 1 && len(payload.Exception.Values[0].Stacktrace.Frames) > 0 &&
			payload.Exception.Values[0].Stacktrace.Frames[0].Function == expectedFunction {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("native object exception projection: %+v events=%d", payload.Exception, len(events))
	}
	if len(payload.DebugMeta.Images) != 1 || payload.DebugMeta.Images[0].DebugID != "1122334455667788AABBCCDDEEFF00111" {
		t.Fatalf("native object debug metadata: %+v", payload.DebugMeta)
	}
	return payload.Exception.Values[0].Stacktrace.Frames
}

func ingestNativeObject(t *testing.T, f *resolutionHTTPFixture, object []byte, objectName, symbolType string, functionOffset uint64, expectedFunction string) nativeFrame {
	t.Helper()
	return ingestNativeObjectFrames(t, f, object, objectName, symbolType, functionOffset, expectedFunction)[0]
}

func nativeDebugArchive(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func nativeTARDebugArchive(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for name, content := range entries {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(content)), Typeflag: tar.TypeReg, Format: tar.FormatGNU}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func nativeGzipDebugArchive(t *testing.T, data []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func TestNativeELFObjectSymbolicationByDebugID(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	object, err := os.ReadFile("../native/testdata/elf-dwarf-amd64")
	if err != nil {
		t.Fatal(err)
	}
	file, err := elf.NewFile(bytes.NewReader(object))
	if err != nil {
		t.Fatal(err)
	}
	var imageBase uint64
	foundBase := false
	for _, program := range file.Progs {
		if program.Type == elf.PT_LOAD && program.Filesz > 0 && (!foundBase || program.Vaddr < imageBase) {
			imageBase, foundBase = program.Vaddr, true
		}
	}
	symbols, err := file.Symbols()
	if err != nil || !foundBase {
		t.Fatalf("ELF fixture metadata: base=%#x %v", imageBase, err)
	}
	var functionAddress uint64
	for _, symbol := range symbols {
		if symbol.Name == "fixture_function" && symbol.Value >= imageBase {
			functionAddress = symbol.Value
			break
		}
	}
	if functionAddress == 0 {
		t.Fatal("ELF fixture lacks fixture_function")
	}
	frame := ingestNativeObject(t, f, object, "uploaded.elf", "elf", functionAddress-imageBase, "fixture_function")
	if !strings.HasSuffix(frame.Filename, "elf-dwarf-source.c") || frame.Line < 1 {
		t.Fatalf("ELF minidump frame: %+v", frame)
	}
}

func TestNativeMachODSYMObjectSymbolicationByDebugID(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	object, err := os.ReadFile("../native/testdata/macho-dsym-amd64")
	if err != nil {
		t.Fatal(err)
	}
	file, err := macho.NewFile(bytes.NewReader(object))
	if err != nil {
		t.Fatal(err)
	}
	var imageBase uint64
	for _, load := range file.Loads {
		if segment, ok := load.(*macho.Segment); ok && segment.Name == "__TEXT" {
			imageBase = segment.Addr
			break
		}
	}
	table, err := file.DWARF()
	if err != nil {
		t.Fatal(err)
	}
	reader := table.Reader()
	var functionOffset uint64
	for functionOffset == 0 {
		entry, err := reader.Next()
		if err != nil {
			t.Fatal(err)
		}
		if entry == nil {
			t.Fatal("Mach-O fixture lacks main")
		}
		if entry.Tag != dwarf.TagSubprogram {
			continue
		}
		name, _ := entry.Val(dwarf.AttrName).(string)
		ranges, rangeErr := table.Ranges(entry)
		if name == "main" && rangeErr == nil && len(ranges) > 0 && ranges[0][0] >= imageBase {
			functionOffset = ranges[0][0] - imageBase
		}
	}
	frame := ingestNativeObject(t, f, object, "uploaded.dsym", "macho", functionOffset, "main")
	if !strings.HasSuffix(frame.Filename, "hello.c") || frame.Line < 1 {
		t.Fatalf("Mach-O dSYM minidump frame: %+v", frame)
	}
}

func TestNativeCFIUnwindWithoutFramePointer(t *testing.T) {
	f := newResolutionHTTPFixture(t)
	object, err := os.ReadFile("../native/testdata/cfi-amd64")
	if err != nil {
		t.Fatal(err)
	}
	file, err := elf.NewFile(bytes.NewReader(object))
	if err != nil {
		t.Fatal(err)
	}
	var imageBase uint64
	foundBase := false
	for _, program := range file.Progs {
		if program.Type == elf.PT_LOAD && program.Filesz > 0 && (!foundBase || program.Vaddr < imageBase) {
			imageBase = program.Vaddr
			foundBase = true
		}
	}
	symbols, err := file.Symbols()
	if err != nil {
		t.Fatal(err)
	}
	addresses := map[string]uint64{}
	for _, symbol := range symbols {
		switch symbol.Name {
		case "crash_frame", "middle_frame", "outer_frame":
			addresses[symbol.Name] = symbol.Value
		}
	}
	if !foundBase || imageBase == 0 || len(addresses) != 3 {
		t.Fatalf("CFI fixture metadata: base=%#x functions=%v", imageBase, addresses)
	}
	const moduleBase = uint64(0x0000000140000000)
	stack := make([]byte, 32)
	binary.LittleEndian.PutUint64(stack[0:], moduleBase+addresses["middle_frame"]-imageBase+0xc)
	binary.LittleEndian.PutUint64(stack[8:], moduleBase+addresses["outer_frame"]-imageBase+0xc)
	frames := ingestNativeObjectFrames(t, f, object, "uploaded-cfi.elf", "elf", addresses["crash_frame"]-imageBase, "crash_frame", stack)
	expected := []string{"crash_frame", "middle_frame", "outer_frame"}
	if len(frames) != len(expected) {
		t.Fatalf("CFI frames=%+v", frames)
	}
	for i, name := range expected {
		if frames[i].Function != name || !strings.HasSuffix(frames[i].Filename, "cfi-source.c") || frames[i].Line < 1 {
			t.Fatalf("CFI frame %d=%+v want %s", i, frames[i], name)
		}
	}
}

func TestNativeArchiveObjectSymbolicationByDebugID(t *testing.T) {
	machoObject, err := os.ReadFile("../native/testdata/macho-dsym-amd64")
	if err != nil {
		t.Fatal(err)
	}
	elfObject, err := os.ReadFile("../native/testdata/elf-dwarf-amd64")
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{
		"Example.dSYM/Contents/Resources/DWARF/Example": machoObject,
		"linux/libexample.so":                           elfObject,
	}
	formats := []struct {
		name  string
		build func(*testing.T, map[string][]byte) []byte
	}{
		{name: "zip", build: nativeDebugArchive},
		{name: "tar", build: nativeTARDebugArchive},
		{name: "tar.gz", build: func(t *testing.T, entries map[string][]byte) []byte {
			return nativeGzipDebugArchive(t, nativeTARDebugArchive(t, entries))
		}},
	}
	for _, format := range formats {
		t.Run(format.name, func(t *testing.T) {
			f := newResolutionHTTPFixture(t)
			archive := format.build(t, entries)
			frame := ingestNativeObject(t, f, archive, "bundle."+format.name, "dsym", 0xf6a, "main")
			if !strings.HasSuffix(frame.Filename, "hello.c") || frame.Line < 1 {
				t.Fatalf("archive dSYM frame: %+v", frame)
			}
		})
	}
}
