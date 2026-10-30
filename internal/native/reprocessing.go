package native

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

type DebugSymbolFile struct {
	Name    string
	DebugID string
	Content []byte
}

// BuildSymbolTables parses stored debug objects with the same identity checks
// used by live Minidump ingestion.
func BuildSymbolTables(files []DebugSymbolFile) (SymbolSet, UnwindSet) {
	symbols := SymbolSet{}
	unwindTables := UnwindSet{}
	companions := pdbCompanions{}
	for _, file := range files {
		collectPDBCompanions(companions, file.Name, file.Content, file.DebugID)
	}
	for _, file := range files {
		debugKey := DebugKey(file.DebugID)
		for _, object := range parseDebugObjectsWithCompanions(file.Name, file.Content, companions) {
			objectDebugKey := DebugKey(object.DebugID)
			aliasKey := DebugKey(object.debugIDAlias)
			if objectDebugKey != "" && debugKey != "" && debugKey != objectDebugKey && debugKey != aliasKey {
				continue
			}
			if object.DebugID == "" {
				symbols[object.Name] = append(symbols[object.Name], object.Functions...)
			}
			if objectDebugKey == "" {
				objectDebugKey = debugKey
			}
			if object.Unwind != nil {
				unwindTables[object.Name] = append(unwindTables[object.Name], object.Unwind)
			}
			for _, key := range []string{objectDebugKey, aliasKey} {
				if key == "" {
					continue
				}
				symbols[key] = append(symbols[key], object.Functions...)
				if object.Unwind != nil && key != object.Name {
					unwindTables[key] = append(unwindTables[key], object.Unwind)
				}
			}
		}
	}
	return symbols, unwindTables
}

// ResymbolicateEvent fills missing symbols on frames already stored in one
// native event. It never adds frames or changes any non-symbol event fields.
func ResymbolicateEvent(data []byte, symbols SymbolSet) ([]byte, bool, error) {
	var event map[string]json.RawMessage
	if err := json.Unmarshal(data, &event); err != nil || event == nil {
		if err == nil {
			err = errors.New("native event must be a JSON object")
		}
		return nil, false, err
	}
	var platform string
	if json.Unmarshal(event["platform"], &platform) != nil || platform != "native" {
		return data, false, nil
	}
	modules := nativeEventModules(event["debug_meta"])
	if len(modules) == 0 || len(symbols) == 0 {
		return data, false, nil
	}
	changed := false
	if raw, ok := event["stacktrace"]; ok {
		updated, modified := resymbolicateStacktrace(raw, modules, symbols)
		if modified {
			event["stacktrace"] = updated
			changed = true
		}
	}
	if raw, ok := event["exception"]; ok {
		updated, modified := resymbolicateExceptions(raw, modules, symbols)
		if modified {
			event["exception"] = updated
			changed = true
		}
	}
	if !changed {
		return data, false, nil
	}
	updated, err := json.Marshal(event)
	if err != nil {
		return nil, false, err
	}
	return updated, true, nil
}

// ReprocessingModules returns the typed native images already present in an
// event. It does not infer missing identities or modify the event.
func ReprocessingModules(data []byte) []Module {
	var event struct {
		Platform  string          `json:"platform"`
		DebugMeta json.RawMessage `json:"debug_meta"`
	}
	if json.Unmarshal(data, &event) != nil || event.Platform != "native" {
		return nil
	}
	return nativeEventModules(event.DebugMeta)
}

func nativeEventModules(raw json.RawMessage) []Module {
	var meta map[string]json.RawMessage
	if json.Unmarshal(raw, &meta) != nil {
		return nil
	}
	var images []json.RawMessage
	if json.Unmarshal(meta["images"], &images) != nil {
		return nil
	}
	modules := make([]Module, 0, len(images))
	for _, rawImage := range images {
		var image struct {
			Name      string          `json:"code_file"`
			DebugID   string          `json:"debug_id"`
			Type      json.RawMessage `json:"type"`
			DebugFile json.RawMessage `json:"debug_file"`
			CodeID    json.RawMessage `json:"code_id"`
			Base      json.RawMessage `json:"image_addr"`
			Size      json.RawMessage `json:"image_size"`
		}
		if json.Unmarshal(rawImage, &image) != nil || image.Name == "" {
			continue
		}
		base, baseOK := eventUint(image.Base, true)
		size, sizeOK := eventUint(image.Size, false)
		if !baseOK || !sizeOK || size == 0 || size > uint64(^uint32(0)) || base > ^uint64(0)-size {
			continue
		}
		module := Module{Name: image.Name, Base: base, Size: uint32(size), DebugID: image.DebugID}
		var imageType string
		_ = json.Unmarshal(image.Type, &imageType)
		switch imageType {
		case "pe":
			module.DebugFormat = "pe"
			var path string
			_ = json.Unmarshal(image.DebugFile, &path)
			module.DebugFile = safePDBFilename(path)
		case "elf":
			module.DebugFormat = "elf"
			var codeID string
			_ = json.Unmarshal(image.CodeID, &codeID)
			module.CodeID = safeELFCodeID(codeID)
		}
		modules = append(modules, module)
	}
	return modules
}

func eventUint(raw json.RawMessage, address bool) (uint64, bool) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		text = strings.TrimSpace(text)
		base := 10
		if strings.HasPrefix(strings.ToLower(text), "0x") {
			text = text[2:]
			base = 16
		} else if address && strings.IndexFunc(text, func(r rune) bool { return r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' }) >= 0 {
			base = 16
		}
		value, err := strconv.ParseUint(text, base, 64)
		return value, err == nil
	}
	value, err := strconv.ParseUint(string(raw), 10, 64)
	return value, err == nil
}

func resymbolicateExceptions(raw json.RawMessage, modules []Module, symbols SymbolSet) (json.RawMessage, bool) {
	var collection map[string]json.RawMessage
	if json.Unmarshal(raw, &collection) != nil {
		return raw, false
	}
	var values []json.RawMessage
	if json.Unmarshal(collection["values"], &values) != nil {
		return raw, false
	}
	changed := false
	for i, value := range values {
		var exception map[string]json.RawMessage
		if json.Unmarshal(value, &exception) != nil {
			continue
		}
		stack, ok := exception["stacktrace"]
		if !ok {
			continue
		}
		updated, modified := resymbolicateStacktrace(stack, modules, symbols)
		if modified {
			exception["stacktrace"] = updated
			values[i], _ = json.Marshal(exception)
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	collection["values"], _ = json.Marshal(values)
	updated, err := json.Marshal(collection)
	if err != nil {
		return raw, false
	}
	return updated, true
}

func resymbolicateStacktrace(raw json.RawMessage, modules []Module, symbols SymbolSet) (json.RawMessage, bool) {
	var stack map[string]json.RawMessage
	if json.Unmarshal(raw, &stack) != nil {
		return raw, false
	}
	var frames []json.RawMessage
	if json.Unmarshal(stack["frames"], &frames) != nil {
		return raw, false
	}
	changed := false
	for i, rawFrame := range frames {
		updated, modified := resymbolicateFrame(rawFrame, modules, symbols)
		if modified {
			frames[i] = updated
			changed = true
		}
	}
	if !changed {
		return raw, false
	}
	stack["frames"], _ = json.Marshal(frames)
	updated, err := json.Marshal(stack)
	if err != nil {
		return raw, false
	}
	return updated, true
}

func resymbolicateFrame(raw json.RawMessage, modules []Module, symbols SymbolSet) (json.RawMessage, bool) {
	var frame map[string]json.RawMessage
	if json.Unmarshal(raw, &frame) != nil {
		return raw, false
	}
	var existing string
	if json.Unmarshal(frame["function"], &existing) == nil && existing != "" {
		return raw, false
	}
	var moduleName string
	if json.Unmarshal(frame["package"], &moduleName) != nil || moduleName == "" {
		_ = json.Unmarshal(frame["module"], &moduleName)
	}
	if moduleName == "" {
		return raw, false
	}
	instruction, ok := eventUint(frame["instruction_addr"], true)
	if !ok {
		return raw, false
	}
	for _, module := range modules {
		if moduleKey(moduleName) != moduleKey(module.Name) || instruction < module.Base || instruction >= module.Base+uint64(module.Size) {
			continue
		}
		crash := Crash{Modules: []Module{module}, Frames: []Frame{{Instruction: instruction, Module: moduleName}}}
		Symbolicate(&crash, symbols)
		resolved := crash.Frames[0]
		if resolved.Function == "" {
			continue
		}
		frame["function"], _ = json.Marshal(resolved.Function)
		if resolved.File != "" {
			frame["filename"], _ = json.Marshal(resolved.File)
		}
		if resolved.Line > 0 {
			frame["lineno"], _ = json.Marshal(resolved.Line)
		}
		updated, err := json.Marshal(frame)
		return updated, err == nil
	}
	return raw, false
}
