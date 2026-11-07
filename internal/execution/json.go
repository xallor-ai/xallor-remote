package execution

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"
)

// DecodeJSON also serves the IPC routing envelope. Unlike encoding/json alone,
// it rejects null values and lone UTF-16 surrogates instead of silently changing
// them to empty strings, zero values or replacement characters.
func DecodeJSON(raw []byte, dst any) error {
	if len(raw) > MaxJSONBytes {
		return ErrTooLarge
	}
	if !utf8.Valid(raw) || !validJSONValues(raw) {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil || d.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	return nil
}

// JSON syntax is checked by the decoder. This pass rejects lossy conversions;
// literal "null" and escaped backslashes inside strings remain ordinary data.
func validJSONValues(raw []byte) bool {
	quoted := false
	for i := 0; i < len(raw); i++ {
		if raw[i] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted {
			if bytes.HasPrefix(raw[i:], []byte("null")) {
				return false
			}
			continue
		}
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		value, ok := unicodeEscape(raw, i)
		if !ok || value >= 0xdc00 && value <= 0xdfff {
			return false
		}
		i += 4
		if value >= 0xd800 && value <= 0xdbff {
			if i+2 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, ok := unicodeEscape(raw, i+2)
			if !ok || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}

func unicodeEscape(raw []byte, u int) (uint64, bool) {
	if u+5 > len(raw) {
		return 0, false
	}
	v, err := strconv.ParseUint(string(raw[u+1:u+5]), 16, 16)
	return v, err == nil
}
