// Package nativepdb builds small Microsoft PDB fixtures for parser and server
// acceptance tests. The fixture uses only the documented MSF 7 and CodeView
// structures needed to carry two procedure symbols.
package nativepdb

import (
	"bytes"
	"encoding/binary"
)

type Fixture struct {
	Data                 []byte
	DBIBlock             int
	ModuleBlock          int
	FirstProcedureLength int
}

func Minimal() Fixture {
	const blockSize = 512
	info := make([]byte, 28)
	binary.LittleEndian.PutUint32(info, 20000404)
	binary.LittleEndian.PutUint32(info[4:], 1)
	binary.LittleEndian.PutUint32(info[8:], 1)
	copy(info[12:], []byte{0xe5, 0x32, 0x98, 0x5a, 0x72, 0x28, 0xc1, 0x41, 0xff, 0xb7, 0xe9, 0x14, 0x89, 0xd9, 0x8e, 0x83})

	firstProcedure := procedure("CrashFunction", 0x10, 0x20, 0x1110)
	secondProcedure := procedure("OtherFunction", 0x80, 0x10, 0x1147)
	module := append([]byte{4, 0, 0, 0}, firstProcedure...)
	module = append(module, secondProcedure...)

	moduleInfo := make([]byte, 64)
	binary.LittleEndian.PutUint16(moduleInfo[34:], 4)
	binary.LittleEndian.PutUint32(moduleInfo[36:], uint32(len(module)))
	moduleInfo = append(moduleInfo, "fixture.obj\x00fixture.obj\x00"...)
	for len(moduleInfo)%4 != 0 {
		moduleInfo = append(moduleInfo, 0)
	}
	const optionalHeaderBytes = 11 * 2
	dbi := make([]byte, 64)
	binary.LittleEndian.PutUint32(dbi, ^uint32(0))
	binary.LittleEndian.PutUint32(dbi[4:], 19990903)
	binary.LittleEndian.PutUint32(dbi[8:], 1)
	binary.LittleEndian.PutUint16(dbi[12:], ^uint16(0))
	binary.LittleEndian.PutUint16(dbi[16:], ^uint16(0))
	binary.LittleEndian.PutUint16(dbi[20:], ^uint16(0))
	binary.LittleEndian.PutUint32(dbi[24:], uint32(len(moduleInfo)))
	binary.LittleEndian.PutUint32(dbi[48:], optionalHeaderBytes)
	dbi = append(dbi, moduleInfo...)
	optional := bytes.Repeat([]byte{0xff}, optionalHeaderBytes)
	binary.LittleEndian.PutUint16(optional[10:], 5)
	dbi = append(dbi, optional...)

	section := make([]byte, 40)
	copy(section, ".text\x00\x00\x00")
	binary.LittleEndian.PutUint32(section[8:], 0x100)
	binary.LittleEndian.PutUint32(section[12:], 0x1000)
	binary.LittleEndian.PutUint32(section[16:], 0x100)
	binary.LittleEndian.PutUint32(section[36:], 0x60000020)

	data, blocks := writeMSF(blockSize, [][]byte{nil, info, nil, dbi, module, section})
	return Fixture{Data: data, DBIBlock: blocks[3][0], ModuleBlock: blocks[4][0], FirstProcedureLength: len(firstProcedure)}
}

func procedure(name string, offset, codeSize uint32, kind uint16) []byte {
	body := make([]byte, 35)
	binary.LittleEndian.PutUint32(body[12:], codeSize)
	binary.LittleEndian.PutUint32(body[28:], offset)
	binary.LittleEndian.PutUint16(body[32:], 1)
	body = append(body, name...)
	body = append(body, 0)
	var record bytes.Buffer
	binary.Write(&record, binary.LittleEndian, uint16(len(body)+2))
	binary.Write(&record, binary.LittleEndian, kind)
	record.Write(body)
	return record.Bytes()
}

func writeMSF(blockSize int, streams [][]byte) ([]byte, [][]int) {
	streamBlocks := make([][]int, len(streams))
	block := 5
	for i, stream := range streams {
		if stream == nil {
			continue
		}
		count := (len(stream) + blockSize - 1) / blockSize
		for j := 0; j < count; j++ {
			streamBlocks[i] = append(streamBlocks[i], block)
			block++
		}
	}
	directory := make([]byte, 4+4*len(streams))
	binary.LittleEndian.PutUint32(directory, uint32(len(streams)))
	for i, stream := range streams {
		size := ^uint32(0)
		if stream != nil {
			size = uint32(len(stream))
		}
		binary.LittleEndian.PutUint32(directory[4+i*4:], size)
	}
	for _, pages := range streamBlocks {
		for _, page := range pages {
			word := make([]byte, 4)
			binary.LittleEndian.PutUint32(word, uint32(page))
			directory = append(directory, word...)
		}
	}
	if (len(directory)+blockSize-1)/blockSize != 1 {
		panic("native PDB fixture directory no longer fits one block")
	}
	data := make([]byte, block*blockSize)
	copy(data, []byte("Microsoft C/C++ MSF 7.00\r\n\x1aDS\x00\x00\x00"))
	binary.LittleEndian.PutUint32(data[32:], uint32(blockSize))
	binary.LittleEndian.PutUint32(data[36:], 1)
	binary.LittleEndian.PutUint32(data[40:], uint32(block))
	binary.LittleEndian.PutUint32(data[44:], uint32(len(directory)))
	binary.LittleEndian.PutUint32(data[52:], 4)
	binary.LittleEndian.PutUint32(data[4*blockSize:], 3)
	copy(data[3*blockSize:], directory)
	for i, stream := range streams {
		for j, page := range streamBlocks[i] {
			start := j * blockSize
			end := min(start+blockSize, len(stream))
			copy(data[page*blockSize:], stream[start:end])
		}
	}
	return data, streamBlocks
}
