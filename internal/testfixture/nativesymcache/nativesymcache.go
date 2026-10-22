// Package nativesymcache builds small public-format SymCache inputs for parser
// and Minidump acceptance tests.
package nativesymcache

import (
	"bytes"
	"encoding/binary"
)

type Fixture struct {
	Data                     []byte
	FunctionsOffset          int
	SourceLocationsOffset    int
	RangesOffset             int
	StringsOffset            int
	FirstStringContentOffset int
	VariableHeaderOffset     int
}

func Build(version uint32, variableHeader uint16) Fixture {
	if version < 7 || version > 9 {
		panic("unsupported SymCache fixture version")
	}

	var stringTable bytes.Buffer
	putString := func(value string) uint32 {
		offset := uint32(stringTable.Len())
		if version == 7 {
			var length [4]byte
			binary.LittleEndian.PutUint32(length[:], uint32(len(value)))
			stringTable.Write(length[:])
		} else {
			var length [binary.MaxVarintLen32]byte
			n := binary.PutUvarint(length[:], uint64(len(value)))
			stringTable.Write(length[:n])
		}
		stringTable.WriteString(value)
		return offset
	}
	compDir := putString("C:/project")
	directory := putString("src")
	fileName := putString("crash.c")
	outerName := putString("outer")
	innerName := putString("CrashFunction")

	var data bytes.Buffer
	data.WriteString("SYMC")
	var versionInfo [4]byte
	binary.LittleEndian.PutUint32(versionInfo[:], version)
	data.Write(versionInfo[:])

	var header [72]byte
	copy(header[:16], []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff})
	binary.LittleEndian.PutUint32(header[16:], 1)
	binary.LittleEndian.PutUint32(header[32:], 1)
	binary.LittleEndian.PutUint32(header[36:], 1)
	binary.LittleEndian.PutUint32(header[40:], 2)
	binary.LittleEndian.PutUint32(header[44:], 3)
	binary.LittleEndian.PutUint32(header[48:], 2)
	binary.LittleEndian.PutUint32(header[52:], uint32(stringTable.Len()))
	if version == 9 {
		binary.LittleEndian.PutUint16(header[56:], variableHeader)
	}
	data.Write(header[:])

	align := func() {
		for data.Len()%8 != 0 {
			data.WriteByte(0)
		}
	}
	writeAligned := func(section []byte) int {
		align()
		offset := data.Len()
		data.Write(section)
		return offset
	}

	fileSize := 12
	if version == 9 {
		fileSize = 24
	}
	file := make([]byte, fileSize)
	binary.LittleEndian.PutUint32(file, compDir)
	binary.LittleEndian.PutUint32(file[4:], directory)
	binary.LittleEndian.PutUint32(file[8:], fileName)
	if version == 9 {
		binary.LittleEndian.PutUint32(file[12:], ^uint32(0))
		binary.LittleEndian.PutUint32(file[16:], ^uint32(0))
		binary.LittleEndian.PutUint32(file[20:], ^uint32(0))
	}
	writeAligned(file)

	functions := make([]byte, 2*16)
	putFunction(functions[:16], outerName, 0x1000)
	putFunction(functions[16:], innerName, 0x1000)
	functionsOffset := writeAligned(functions)

	locations := make([]byte, 3*16)
	putLocation(locations[:16], 0, 10, 0, ^uint32(0))
	putLocation(locations[16:32], 0, 42, 1, 0)
	putLocation(locations[32:], ^uint32(0), ^uint32(0), ^uint32(0), ^uint32(0))
	locationsOffset := writeAligned(locations)

	ranges := make([]byte, 2*4)
	binary.LittleEndian.PutUint32(ranges, 0x1000)
	binary.LittleEndian.PutUint32(ranges[4:], 0x1020)
	rangesOffset := writeAligned(ranges)

	stringsOffset := writeAligned(stringTable.Bytes())
	variableHeaderOffset := 0
	if version == 9 && variableHeader == 1 {
		variableHeaderOffset = writeAligned(make([]byte, 12))
		functionVariables := make([]byte, 2*8)
		binary.LittleEndian.PutUint32(functionVariables, ^uint32(0))
		binary.LittleEndian.PutUint32(functionVariables[8:], ^uint32(0))
		writeAligned(functionVariables)
		writeAligned(nil)
		writeAligned(nil)
		writeAligned(nil)
	}

	return Fixture{
		Data:                     append([]byte(nil), data.Bytes()...),
		FunctionsOffset:          functionsOffset,
		SourceLocationsOffset:    locationsOffset,
		RangesOffset:             rangesOffset,
		StringsOffset:            stringsOffset,
		FirstStringContentOffset: stringsOffset + stringLengthPrefixSize(version),
		VariableHeaderOffset:     variableHeaderOffset,
	}
}

func stringLengthPrefixSize(version uint32) int {
	if version == 7 {
		return 4
	}
	return 1
}

func putFunction(dst []byte, name uint32, entry uint32) {
	binary.LittleEndian.PutUint32(dst, name)
	binary.LittleEndian.PutUint32(dst[4:], ^uint32(0))
	binary.LittleEndian.PutUint32(dst[8:], entry)
}

func putLocation(dst []byte, file, line, function, parent uint32) {
	binary.LittleEndian.PutUint32(dst, file)
	binary.LittleEndian.PutUint32(dst[4:], line)
	binary.LittleEndian.PutUint32(dst[8:], function)
	binary.LittleEndian.PutUint32(dst[12:], parent)
}
