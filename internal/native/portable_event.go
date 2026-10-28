package native

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"path"
	"strconv"
	"strings"
)

type portableImage struct {
	Type    string `json:"type"`
	DebugID string `json:"debug_id"`
}

type portableCandidate struct {
	pdb       *PortablePDB
	hash      [32]byte
	points    int
	nameBytes int
	ambiguous bool
}

// PortableEventNeedsSymbols avoids loading debug files for unrelated captures.
func PortableEventNeedsSymbols(raw []byte) bool {
	var event struct {
		DebugMeta struct {
			Images []portableImage `json:"images"`
		} `json:"debug_meta"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return false
	}
	for _, image := range event.DebugMeta.Images {
		if image.Type == "pe_dotnet" && image.DebugID != "" {
			return true
		}
	}
	return false
}

func portableCandidates(files []DebugSymbolFile, wanted map[string]bool) map[string]portableCandidate {
	selected := map[string]portableCandidate{}
	total := 0
	totalNames := 0
	for _, file := range files {
		local := map[string]portableCandidate{}
		localTotal := 0
		localNames := 0
		exceeded := false
		_, valid := scanDebugInput(file.Name, file.Content, func(_ string, data []byte) (DebugObject, bool) {
			if exceeded || !bytes.HasPrefix(data, []byte("BSJB")) {
				return DebugObject{}, false
			}
			streams, ok := portableStreams(data)
			if !ok || len(streams["#Pdb"]) < 32 {
				return DebugObject{}, false
			}
			id := portableIdentity(streams["#Pdb"])
			if !wanted[id] || DebugKey(file.DebugID) != "" && DebugKey(file.DebugID) != id {
				return DebugObject{}, false
			}
			hash := sha256.Sum256(data)
			previous, exists := local[id]
			if exists && previous.hash == hash {
				return DebugObject{}, false
			}
			pdb, ok := ParsePortablePDB(data)
			if !ok {
				return DebugObject{}, false
			}
			if exists {
				previous.ambiguous = true
				local[id] = previous
				return DebugObject{}, false
			}
			count, nameBytes := 0, 0
			names := make(map[string]bool)
			for _, points := range pdb.Methods {
				count += len(points)
				for _, point := range points {
					if !names[point.Document] {
						names[point.Document] = true
						if len(point.Document) > maxPDBBytes-nameBytes {
							exceeded = true
							return DebugObject{}, false
						}
						nameBytes += len(point.Document)
					}
				}
			}
			if count > maxPortableRows-localTotal || nameBytes > maxPDBBytes-localNames {
				exceeded = true
				return DebugObject{}, false
			}
			localTotal += count
			localNames += nameBytes
			local[id] = portableCandidate{pdb: pdb, hash: hash, points: count, nameBytes: nameBytes}
			return DebugObject{}, false
		})
		if !valid {
			continue
		}
		if exceeded {
			return nil
		}
		for id, candidate := range local {
			if previous, exists := selected[id]; exists {
				if previous.hash != candidate.hash || candidate.ambiguous {
					previous.ambiguous = true
					selected[id] = previous
				}
				continue
			}
			if candidate.points > maxPortableRows-total || candidate.nameBytes > maxPDBBytes-totalNames {
				return nil
			}
			total += candidate.points
			totalNames += candidate.nameBytes
			selected[id] = candidate
		}
	}
	return selected
}

// SymbolicatePortableEvent enriches managed frames without altering unrelated
// fields. Its files must already be selected from the event's project.
func SymbolicatePortableEvent(raw []byte, files []DebugSymbolFile) ([]byte, bool, error) {
	var event map[string]json.RawMessage
	if json.Unmarshal(raw, &event) != nil || event == nil {
		return raw, false, errors.New("event must be a JSON object")
	}
	var metadata struct {
		Images []portableImage `json:"images"`
	}
	if json.Unmarshal(event["debug_meta"], &metadata) != nil {
		return raw, false, nil
	}
	wanted := map[string]bool{}
	for _, image := range metadata.Images {
		if image.Type == "pe_dotnet" && image.DebugID != "" {
			wanted[DebugKey(image.DebugID)] = true
		}
	}
	if len(wanted) == 0 {
		return raw, false, nil
	}
	selected := portableCandidates(files, wanted)
	if len(selected) == 0 {
		return raw, false, nil
	}
	changed := false
	if value, ok := event["stacktrace"]; ok {
		if updated, yes := portableStacktrace(value, metadata.Images, selected); yes {
			event["stacktrace"] = updated
			changed = true
		}
	}
	for _, key := range []string{"exception", "threads"} {
		if value, ok := event[key]; ok {
			if updated, yes := portableCollection(value, metadata.Images, selected); yes {
				event[key] = updated
				changed = true
			}
		}
	}
	if !changed {
		return raw, false, nil
	}
	out, err := json.Marshal(event)
	if err != nil {
		return raw, false, err
	}
	return out, true, nil
}

func portableCollection(raw json.RawMessage, images []portableImage, candidates map[string]portableCandidate) (json.RawMessage, bool) {
	var collection map[string]json.RawMessage
	var values []json.RawMessage
	array := json.Unmarshal(raw, &values) == nil
	if !array {
		if json.Unmarshal(raw, &collection) != nil || collection == nil || json.Unmarshal(collection["values"], &values) != nil {
			return raw, false
		}
	}
	changed := false
	for i, value := range values {
		var item map[string]json.RawMessage
		if json.Unmarshal(value, &item) != nil || item == nil {
			continue
		}
		if stack, ok := item["stacktrace"]; ok {
			if updated, yes := portableStacktrace(stack, images, candidates); yes {
				item["stacktrace"] = updated
				values[i], _ = json.Marshal(item)
				changed = true
			}
		}
	}
	if !changed {
		return raw, false
	}
	if array {
		out, _ := json.Marshal(values)
		return out, true
	}
	collection["values"], _ = json.Marshal(values)
	out, _ := json.Marshal(collection)
	return out, true
}

func portableStacktrace(raw json.RawMessage, images []portableImage, candidates map[string]portableCandidate) (json.RawMessage, bool) {
	var stack map[string]json.RawMessage
	var frames []json.RawMessage
	if json.Unmarshal(raw, &stack) != nil || stack == nil || json.Unmarshal(stack["frames"], &frames) != nil {
		return raw, false
	}
	changed := false
	for i, frame := range frames {
		if updated, yes := portableFrame(frame, images, candidates); yes {
			frames[i] = updated
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	stack["frames"], _ = json.Marshal(frames)
	out, _ := json.Marshal(stack)
	return out, true
}

func portableFrame(raw json.RawMessage, images []portableImage, candidates map[string]portableCandidate) (json.RawMessage, bool) {
	var frame map[string]json.RawMessage
	if json.Unmarshal(raw, &frame) != nil || frame == nil {
		return raw, false
	}
	var mode string
	if json.Unmarshal(frame["addr_mode"], &mode) != nil {
		return raw, false
	}
	indexText, ok := strings.CutPrefix(mode, "rel:")
	if !ok || indexText == "" {
		return raw, false
	}
	index, err := strconv.ParseUint(indexText, 10, 32)
	if err != nil || index >= uint64(len(images)) {
		return raw, false
	}
	image := images[index]
	if image.Type != "pe_dotnet" || image.DebugID == "" {
		return raw, false
	}
	candidate, ok := candidates[DebugKey(image.DebugID)]
	if !ok || candidate.ambiguous {
		return raw, false
	}
	method, methodOK := eventUint(frame["function_id"], true)
	offset, offsetOK := eventUint(frame["instruction_addr"], true)
	if !methodOK || method == 0 || method > 0xffffff || !offsetOK || offset > uint64(^uint32(0)) {
		return raw, false
	}
	point, ok := candidate.pdb.Lookup(uint32(method), uint32(offset))
	if !ok || point.Line <= 0 || point.Document == "" {
		return raw, false
	}
	var filename, absolute string
	for key, destination := range map[string]*string{"filename": &filename, "abs_path": &absolute} {
		if value, exists := frame[key]; exists && json.Unmarshal(value, destination) != nil {
			return raw, false
		}
	}
	base := path.Base(strings.ReplaceAll(point.Document, "\\", "/"))
	if absolute != "" && absolute != point.Document || filename != "" && filename != point.Document && filename != base {
		return raw, false
	}
	changed := false
	if filename == "" {
		frame["filename"], _ = json.Marshal(point.Document)
		changed = true
	}
	if absolute == "" {
		frame["abs_path"], _ = json.Marshal(point.Document)
		changed = true
	}
	if value, exists := frame["lineno"]; !exists || bytes.Equal(value, []byte("null")) {
		frame["lineno"], _ = json.Marshal(point.Line)
		changed = true
	}
	if !changed {
		return raw, false
	}
	out, _ := json.Marshal(frame)
	return out, true
}
