package otlp

import (
	"strconv"

	"google.golang.org/protobuf/encoding/protowire"
)

const profilesWarning = "Profiles dictionary indices are unavailable for logs."

// SuccessResponse encodes the OTLP logs export result.
func SuccessResponse(encoding Encoding, profileWarning bool) []byte {
	if !profileWarning {
		if encoding == Protobuf {
			return nil
		}
		return []byte(`{}`)
	}
	if encoding != Protobuf {
		return []byte(`{"partialSuccess":{"rejectedLogRecords":"0","errorMessage":` + strconv.Quote(profilesWarning) + `}}`)
	}
	// ExportLogsPartialSuccess.error_message is field 2. Its omitted int64
	// rejected_log_records has the required zero value.
	partial := protowire.AppendString(protowire.AppendTag(nil, 2, protowire.BytesType), profilesWarning)
	return protowire.AppendBytes(protowire.AppendTag(nil, 1, protowire.BytesType), partial)
}

// ErrorResponse encodes the public google.rpc.Status for an HTTP failure.
func ErrorResponse(encoding Encoding, status int) []byte {
	var code uint64
	var message string
	switch status {
	case 400:
		code, message = 3, "Malformed OTLP logs request."
	case 403:
		code, message = 7, "Invalid project key."
	case 413:
		code, message = 8, "OTLP logs capacity exceeded."
	case 415:
		code, message = 12, "Unsupported OTLP encoding."
	case 404:
		code, message = 12, "Logs are disabled."
	case 405:
		code, message = 12, "Method is not supported."
	default:
		code, message = 14, "Durable log storage is unavailable."
	}
	if encoding == Protobuf {
		response := protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), code)
		return protowire.AppendString(protowire.AppendTag(response, 2, protowire.BytesType), message)
	}
	response := strconv.AppendUint([]byte(`{"code":`), code, 10)
	response = append(response, `,"message":`...)
	response = strconv.AppendQuote(response, message)
	return append(response, '}')
}
