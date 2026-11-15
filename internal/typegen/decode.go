package typegen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// object is a JSON object with its keys in the order they appeared, so the
// generated type lists fields the way the sender wrote them. A key that
// appears twice keeps its first position and its last value, as JSON.parse does.
type object struct {
	keys []string
	vals map[string]any
}

// decode parses one JSON value into nil, string, bool, float64, []any or
// *object, the way JSON.parse would, keeping object key order.
func decode(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("more than one JSON value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := &object{vals: map[string]any{}}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				if _, seen := obj.vals[k]; !seen {
					obj.keys = append(obj.keys, k)
				}
				obj.vals[k] = v
			}
			if _, err := dec.Token(); err != nil { // the closing brace
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil { // the closing bracket
				return nil, err
			}
			return arr, nil
		}
		return nil, fmt.Errorf("unexpected %v", t)
	case float64, string, bool, nil:
		return t, nil
	}
	return nil, fmt.Errorf("unexpected token %v", tok)
}
