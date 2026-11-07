package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

type accountCredential struct {
	Relay string `json:"relay"`
	Token string `json:"token"`
}

// The canonical record has exactly two string fields. Missing, null, duplicate
// or misspelled fields must not be confused with the explicit empty record.
func decodeAccountCredential(raw []byte) (accountCredential, error) {
	invalid := errors.New("身份凭据文件无法读取，请重新导入或清除")
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return accountCredential{}, invalid
	}
	fields := make(map[string]string, 2)
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || (name != "relay" && name != "token") {
			return accountCredential{}, invalid
		}
		if _, exists := fields[name]; exists {
			return accountCredential{}, invalid
		}
		value, err := d.Token()
		text, ok := value.(string)
		if err != nil || !ok {
			return accountCredential{}, invalid
		}
		fields[name] = text
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') || len(fields) != 2 {
		return accountCredential{}, invalid
	}
	if _, err := d.Token(); err != io.EOF {
		return accountCredential{}, invalid
	}
	return accountCredential{Relay: fields["relay"], Token: fields["token"]}, nil
}
