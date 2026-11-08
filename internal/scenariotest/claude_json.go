package scenariotest

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
)

type jsonField struct {
	key   string
	value any
}

type jsonObject []jsonField

func (o jsonObject) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}

		key, err := marshalJSON(f.key)
		if err != nil {
			return nil, err
		}

		value, err := marshalJSON(f.value)
		if err != nil {
			return nil, err
		}

		b.Write(key)
		b.WriteByte(':')
		b.Write(value)
	}

	b.WriteByte('}')
	return b.Bytes(), nil
}

func (o jsonObject) get(key string) (any, bool) {
	for _, f := range o {
		if f.key == key {
			return f.value, true
		}
	}

	return nil, false
}

func (o jsonObject) set(key string, value any) jsonObject {
	out := slices.Clone(o)
	for i, f := range out {
		if f.key == key {
			out[i].value = value
			return out
		}
	}

	return append(out, jsonField{key, value})
}

// rawString reads v as a JSON string, or "" if it is none.
func rawString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case json.RawMessage:
		var s string
		if json.Unmarshal(v, &s) == nil {
			return s
		}
	}

	return ""
}

// Claude Code writes "<" and "&" as they are, and json.Marshal would escape them.
func marshalJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}

	return bytes.TrimSuffix(b.Bytes(), []byte{'\n'}), nil
}

func parseObject(raw []byte) (jsonObject, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("%s is not a JSON object", raw)
	}

	obj := jsonObject{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}

		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("%s is not a JSON object", raw)
		}

		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}

		obj = append(obj, jsonField{key, value})
	}

	if _, err := dec.Token(); err != nil {
		return nil, err
	}

	if dec.More() {
		return nil, errors.New("more than one JSON value where one object was expected")
	}

	return obj, nil
}

func readFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	return b, nil
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func newID(prefix string) string {
	return prefix + rand.Text()
}
