package native

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

func BuildEvent(crash Crash, fields map[string]string, now time.Time) (json.RawMessage, string, error) {
	rawID := make([]byte, 16)
	if _, err := rand.Read(rawID); err != nil {
		return nil, "", err
	}
	eventID := hex.EncodeToString(rawID)
	typeName := exceptionName(crash.Code)
	value := fmt.Sprintf("Minidump crash in thread %d (%s)", crash.ThreadID, typeName)
	frames := make([]map[string]any, 0, len(crash.Frames))
	for _, frame := range crash.Frames {
		entry := map[string]any{"instruction_addr": fmt.Sprintf("0x%x", frame.Instruction)}
		if frame.Module != "" {
			entry["package"] = frame.Module
		}
		if frame.Function != "" {
			entry["function"] = frame.Function
		}
		if frame.File != "" {
			entry["filename"] = frame.File
		}
		if frame.Line > 0 {
			entry["lineno"] = frame.Line
		}
		frames = append(frames, entry)
	}
	images := make([]map[string]any, 0, len(crash.Modules))
	for _, module := range crash.Modules {
		image := map[string]any{"type": "symbolic", "image_addr": fmt.Sprintf("0x%x", module.Base), "image_size": module.Size}
		if module.Name != "" {
			image["code_file"] = module.Name
		}
		if module.DebugID != "" {
			image["debug_id"] = module.DebugID
		}
		images = append(images, image)
	}
	extra := map[string]string{}
	for name, value := range fields {
		if len(extra) == 100 {
			break
		}
		if name != "upload_file_minidump" && len(name) <= 100 && len(value) <= 2048 {
			extra[name] = value
		}
	}
	payload := map[string]any{
		"event_id": eventID, "type": "error", "platform": "native", "level": "fatal", "timestamp": now.UTC().Format(time.RFC3339Nano),
		"logger": "minidump", "message": value,
		"exception":  map[string]any{"values": []any{map[string]any{"type": typeName, "value": value, "mechanism": map[string]any{"type": "minidump", "handled": false}, "stacktrace": map[string]any{"frames": frames}}}},
		"debug_meta": map[string]any{"images": images}, "extra": extra,
	}
	raw, err := json.Marshal(payload)
	return raw, eventID, err
}

func exceptionName(code uint32) string {
	switch code {
	case 0xC0000005:
		return "EXCEPTION_ACCESS_VIOLATION"
	case 0xC000001D:
		return "EXCEPTION_ILLEGAL_INSTRUCTION"
	case 0xC0000094:
		return "EXCEPTION_INT_DIVIDE_BY_ZERO"
	case 0xC0000409:
		return "EXCEPTION_STACK_BUFFER_OVERRUN"
	case 0x80000003:
		return "EXCEPTION_BREAKPOINT"
	default:
		return fmt.Sprintf("EXCEPTION_0x%08X", code)
	}
}
