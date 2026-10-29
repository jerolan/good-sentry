package native

import (
	"encoding/binary"
	"sort"
)

const (
	maxPDBOMAPRecords   = 1_000_000
	maxPDBOMAPFragments = 1_000_000
	maxPDBOMAPWork      = 4_000_000
)

type pdbOMAPEntry struct {
	source, target uint32
}

func parsePDBOMAP(data []byte) ([]pdbOMAPEntry, bool) {
	if len(data) == 0 || len(data)%8 != 0 || len(data)/8 > maxPDBOMAPRecords {
		return nil, false
	}
	entries := make([]pdbOMAPEntry, len(data)/8)
	for i := range entries {
		entries[i] = pdbOMAPEntry{binary.LittleEndian.Uint32(data[i*8:]), binary.LittleEndian.Uint32(data[i*8+4:])}
		if i > 0 && entries[i-1].source >= entries[i].source {
			return nil, false
		}
	}
	return entries, true
}

// mapPDBFunctions splits source ranges at OMAP boundaries. The source
// position of each fragment is retained for procedure-relative inline sites.
func mapPDBFunctions(functions []SymbolFunction, entries []pdbOMAPEntry, targetSections []pdbSection) ([]SymbolFunction, bool) {
	sections := append([]pdbSection(nil), targetSections...)
	sort.Slice(sections, func(i, j int) bool { return sections[i].virtualAddr < sections[j].virtualAddr })
	for i, section := range sections {
		end := uint64(section.virtualAddr) + uint64(max(section.virtualSize, section.rawSize))
		if end > uint64(1)<<32 || i > 0 && uint64(sections[i-1].virtualAddr)+uint64(max(sections[i-1].virtualSize, sections[i-1].rawSize)) > uint64(section.virtualAddr) {
			return nil, false
		}
	}
	var mapped []SymbolFunction
	work := 0
	for _, function := range functions {
		if function.End > uint64(1)<<32 || function.Start >= function.End {
			return nil, false
		}
		index := sort.Search(len(entries), func(i int) bool { return uint64(entries[i].source) > function.Start }) - 1
		position := function.Start
		if index < 0 {
			index = 0
			position = max(position, uint64(entries[0].source))
		}
		for position < function.End && index < len(entries) {
			work++
			if work > maxPDBOMAPWork {
				return nil, false
			}
			entry := entries[index]
			end := function.End
			if index+1 < len(entries) {
				end = min(end, uint64(entries[index+1].source))
			}
			if entry.target != 0 && position < end {
				startTarget := uint64(entry.target) + position - uint64(entry.source)
				endTarget := uint64(entry.target) + end - uint64(entry.source)
				sectionIndex := sort.Search(len(sections), func(i int) bool { return uint64(sections[i].virtualAddr) > startTarget }) - 1
				if endTarget > uint64(1)<<32 || sectionIndex < 0 {
					return nil, false
				}
				section := sections[sectionIndex]
				if section.flags&pdbPESectionExecute == 0 || endTarget > uint64(section.virtualAddr)+uint64(max(section.virtualSize, section.rawSize)) || len(mapped) == maxPDBOMAPFragments {
					return nil, false
				}
				fragment := function
				if fragment.procedureStart == 0 && fragment.inlineSites != nil {
					fragment.procedureStart = function.Start
				}
				fragment.Start, fragment.End = startTarget, endTarget
				fragment.omapOrigin, fragment.omapMapped = position, true
				mapped = append(mapped, fragment)
			}
			position = end
			index++
		}
	}
	return mapped, true
}
