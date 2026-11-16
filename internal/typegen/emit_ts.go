package typegen

import (
	"encoding/json"
	"strings"
)

// toTypeScript writes TypeScript interfaces, the root first and nested
// objects after it in the order they are first referenced.
func toTypeScript(root *shape, name, note string) string {
	var blocks []string
	n := &namer{}

	var ts func(s *shape, hint string) string
	iface := func(name string, obj *objShape) string {
		own := n.unique(name)
		idx := len(blocks)
		blocks = append(blocks, "") // reserve the slot so parents come before children
		var lines []string
		for _, k := range obj.keys {
			f := obj.fields[k]
			key := k
			if !ident.MatchString(k) {
				key = quoteJS(k)
			}
			opt := ""
			if f.count < obj.seen {
				opt = "?"
			}
			lines = append(lines, "  "+key+opt+": "+ts(f.shape, own+pascal(k))+";")
		}
		body := strings.Join(lines, "\n")
		if len(lines) > 0 {
			body += "\n"
		}
		blocks[idx] = "export interface " + own + " {\n" + body + "}"
		return own
	}
	ts = func(s *shape, hint string) string {
		var parts []string
		if s.obj != nil {
			if len(s.obj.keys) > 0 {
				parts = append(parts, iface(hint, s.obj))
			} else {
				parts = append(parts, "Record<string, unknown>")
			}
		}
		if s.arr != nil {
			el := "unknown"
			if s.arr.items != nil {
				el = ts(s.arr.items, hint+"Item")
			}
			if strings.Contains(el, "|") {
				parts = append(parts, "("+el+")[]")
			} else {
				parts = append(parts, el+"[]")
			}
		}
		if s.prims[pString] {
			parts = append(parts, "string")
		}
		if s.prims[pInteger] || s.prims[pNumber] {
			parts = append(parts, "number")
		}
		if s.prims[pBoolean] {
			parts = append(parts, "boolean")
		}
		if s.prims[pNull] {
			parts = append(parts, "null")
		}
		if len(parts) == 0 {
			return "unknown"
		}
		return strings.Join(parts, " | ")
	}

	if isRootObject(root) {
		iface(name, root.obj)
	} else {
		n.unique(name)
		body := ts(root, name)
		blocks = append([]string{"export type " + name + " = " + body + ";"}, blocks...)
	}
	return "// " + note + "\n" + strings.Join(blocks, "\n\n") + "\n"
}

// --- JSON Schema

// orderedSchema is a JSON Schema object whose keys keep their order when
// marshalled, so the output reads the way the body did.
type orderedSchema struct {
	keys []string
	vals map[string]any
}

func newSchema() *orderedSchema { return &orderedSchema{vals: map[string]any{}} }

func (o *orderedSchema) set(k string, v any) *orderedSchema {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
	return o
}

func (o *orderedSchema) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(quoteJS(k))
		b.WriteByte(':')
		v, err := json.Marshal(o.vals[k])
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

func schemaOf(s *shape) *orderedSchema {
	var variants []*orderedSchema
	var types []string
	if s.obj != nil {
		props := newSchema()
		var required []string
		for _, k := range s.obj.keys {
			f := s.obj.fields[k]
			props.set(k, schemaOf(f.shape))
			if f.count == s.obj.seen {
				required = append(required, k)
			}
		}
		o := newSchema().set("type", "object").set("properties", props)
		if len(required) > 0 {
			o.set("required", required)
		}
		variants = append(variants, o)
	}
	if s.arr != nil {
		a := newSchema().set("type", "array")
		if s.arr.items != nil {
			a.set("items", schemaOf(s.arr.items))
		}
		variants = append(variants, a)
	}
	if s.prims[pString] {
		types = append(types, "string")
	}
	if s.prims[pNumber] {
		types = append(types, "number")
	} else if s.prims[pInteger] {
		types = append(types, "integer")
	}
	if s.prims[pBoolean] {
		types = append(types, "boolean")
	}
	if s.prims[pNull] {
		types = append(types, "null")
	}

	// Fold plain types into a single object/array schema as a type list.
	if len(variants) == 1 && len(types) > 0 {
		v := variants[0]
		v.set("type", append([]string{v.vals["type"].(string)}, types...))
		return v
	}
	if len(types) == 1 {
		variants = append(variants, newSchema().set("type", types[0]))
	} else if len(types) > 1 {
		variants = append(variants, newSchema().set("type", types))
	}
	switch len(variants) {
	case 0:
		return newSchema()
	case 1:
		return variants[0]
	}
	return newSchema().set("anyOf", variants)
}

// toJSONSchema writes a JSON Schema (draft 2020-12) for the shape.
func toJSONSchema(root *shape, title, note string) string {
	body := schemaOf(root)
	out := newSchema().
		set("$schema", "https://json-schema.org/draft/2020-12/schema").
		set("$comment", note).
		set("title", title)
	for _, k := range body.keys {
		out.set(k, body.vals[k])
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b) + "\n"
}
