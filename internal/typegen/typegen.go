// Package typegen guesses a type for a JSON body, or for several bodies merged,
// and writes it in TypeScript, JSON Schema, Zod, Go or Pydantic. One request is
// one example, so the result is a best-effort starting point; several requests
// make it better, since a field missing from some of them becomes optional.
//
// The UI, the API, the MCP server and the CLI all use this one package, so
// "Copy as type" and `testhooks requests types` give the same answer.
package typegen

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Lang is an output language.
type Lang string

const (
	TypeScript Lang = "typescript"
	JSONSchema Lang = "jsonschema"
	Zod        Lang = "zod"
	Go         Lang = "go"
	Pydantic   Lang = "pydantic"
)

// Langs lists the languages, in the order the UI shows them.
func Langs() []Lang { return []Lang{TypeScript, Zod, JSONSchema, Go, Pydantic} }

// ParseLang accepts a language name or a common alias (ts, schema, py, golang).
func ParseLang(s string) (Lang, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "typescript", "ts":
		return TypeScript, nil
	case "jsonschema", "json-schema", "json_schema", "schema":
		return JSONSchema, nil
	case "zod":
		return Zod, nil
	case "go", "golang":
		return Go, nil
	case "pydantic", "python", "py":
		return Pydantic, nil
	}
	return "", fmt.Errorf("unknown language %q; use typescript, zod, jsonschema, go or pydantic", s)
}

// Generate guesses a type named name from the JSON bodies and writes it in lang.
// Several bodies are merged into one shape. A body that is not JSON is an error.
func Generate(lang Lang, name string, bodies ...[]byte) (string, error) {
	if len(bodies) == 0 {
		return "", errors.New("no body to infer a type from")
	}
	root := empty()
	for i, b := range bodies {
		v, err := decode(b)
		if err != nil {
			if len(bodies) == 1 {
				return "", fmt.Errorf("the body is not JSON: %w", err)
			}
			return "", fmt.Errorf("body %d is not JSON: %w", i+1, err)
		}
		observe(root, v)
	}
	if name == "" {
		name = "WebhookPayload"
	}
	note := "inferred from one request, best effort"
	if len(bodies) > 1 {
		note = fmt.Sprintf("inferred from %d requests, best effort", len(bodies))
	}
	switch lang {
	case TypeScript:
		return toTypeScript(root, name, note), nil
	case JSONSchema:
		return toJSONSchema(root, name, note), nil
	case Zod:
		return toZod(root, name, note), nil
	case Go:
		return toGo(root, name, note)
	case Pydantic:
		return toPydantic(root, name, note), nil
	}
	return "", fmt.Errorf("unknown language %q", lang)
}

// EventOf returns the event name a JSON body carries in a type, event,
// event_type, action or topic field, or "" when it has none. It is what the
// UI shows as the request's label and what TypeName turns into a type name.
func EventOf(body []byte) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil {
		return ""
	}
	for _, k := range []string{"type", "event", "event_type", "action", "topic"} {
		var s string
		if raw, ok := m[k]; ok && json.Unmarshal(raw, &s) == nil && s != "" {
			return s
		}
	}
	return ""
}

var validName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidName reports whether name can be a type name in every output language.
func ValidName(name string) bool { return validName.MatchString(name) }

// TypeName turns an event name into a type name: "invoice.paid" becomes
// "InvoicePaidPayload"; anything unusable becomes "WebhookPayload".
func TypeName(event string) string {
	if p := pascal(event); p != "" {
		return p + "Payload"
	}
	return "WebhookPayload"
}

// --- the shape of what was seen

type prim string

const (
	pString  prim = "string"
	pInteger prim = "integer"
	pNumber  prim = "number"
	pBoolean prim = "boolean"
	pNull    prim = "null"
)

// shape is what was seen at one place in the JSON, merged across bodies and
// array elements.
type shape struct {
	prims map[prim]bool
	obj   *objShape // set when an object was seen here
	arr   *arrShape // set when an array was seen here
}

type objShape struct {
	seen   int
	keys   []string // first-seen order
	fields map[string]*field
}

type field struct {
	shape *shape
	count int
}

// arrShape's items is nil while every array seen was empty.
type arrShape struct{ items *shape }

func empty() *shape { return &shape{prims: map[prim]bool{}} }

func observe(s *shape, v any) {
	switch x := v.(type) {
	case nil:
		s.prims[pNull] = true
	case string:
		s.prims[pString] = true
	case bool:
		s.prims[pBoolean] = true
	case float64:
		if x == math.Trunc(x) && !math.IsInf(x, 0) {
			s.prims[pInteger] = true
		} else {
			s.prims[pNumber] = true
		}
	case []any:
		if s.arr == nil {
			s.arr = &arrShape{}
		}
		for _, el := range x {
			if s.arr.items == nil {
				s.arr.items = empty()
			}
			observe(s.arr.items, el)
		}
	case *object:
		if s.obj == nil {
			s.obj = &objShape{fields: map[string]*field{}}
		}
		s.obj.seen++
		for _, k := range x.keys {
			f := s.obj.fields[k]
			if f == nil {
				f = &field{shape: empty()}
				s.obj.fields[k] = f
				s.obj.keys = append(s.obj.keys, k)
			}
			f.count++
			observe(f.shape, x.vals[k])
		}
	}
}

// --- names

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]+`)

// pascal joins the words of s in PascalCase; a leading digit gets a T in front.
func pascal(s string) string {
	var b strings.Builder
	for _, w := range nonAlnum.Split(s, -1) {
		if w == "" {
			continue
		}
		b.WriteString(strings.ToUpper(w[:1]) + w[1:])
	}
	out := b.String()
	if out != "" && out[0] >= '0' && out[0] <= '9' {
		return "T" + out
	}
	return out
}

var ident = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// namer hands out type names that don't repeat.
type namer struct{ used map[string]bool }

func (n *namer) unique(name string) string {
	if n.used == nil {
		n.used = map[string]bool{}
	}
	if name == "" {
		name = "Item"
	}
	out := name
	for i := 2; n.used[out]; i++ {
		out = fmt.Sprintf("%s%d", name, i)
	}
	n.used[out] = true
	return out
}

// quoteJS quotes s as a JSON/JS string literal.
func quoteJS(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// isRootObject reports whether the root is a plain object with fields, which
// gets its own interface or class rather than a type alias.
func isRootObject(s *shape) bool {
	return s.obj != nil && len(s.obj.keys) > 0 && len(s.prims) == 0 && s.arr == nil
}
