package typegen

import "strings"

// toZod writes Zod schemas: nested objects first, since each must be declared
// before the schema that uses it, and the root last with its inferred type.
func toZod(root *shape, name, note string) string {
	var blocks []string // parents first, reversed at the end
	n := &namer{}

	var zod func(s *shape, hint string) string
	object := func(name string, obj *objShape) string {
		own := n.unique(name)
		idx := len(blocks)
		blocks = append(blocks, "")
		var lines []string
		for _, k := range obj.keys {
			f := obj.fields[k]
			key := k
			if !ident.MatchString(k) {
				key = quoteJS(k)
			}
			t := zod(f.shape, own+pascal(k))
			if f.count < obj.seen {
				t += ".optional()"
			}
			lines = append(lines, "  "+key+": "+t+",")
		}
		body := strings.Join(lines, "\n")
		if len(lines) > 0 {
			body += "\n"
		}
		blocks[idx] = "export const " + own + " = z.object({\n" + body + "});"
		return own
	}
	zod = func(s *shape, hint string) string {
		var parts []string
		if s.obj != nil {
			if len(s.obj.keys) > 0 {
				parts = append(parts, object(hint, s.obj))
			} else {
				parts = append(parts, "z.record(z.string(), z.unknown())")
			}
		}
		if s.arr != nil {
			el := "z.unknown()"
			if s.arr.items != nil {
				el = zod(s.arr.items, hint+"Item")
			}
			parts = append(parts, "z.array("+el+")")
		}
		if s.prims[pString] {
			parts = append(parts, "z.string()")
		}
		if s.prims[pInteger] || s.prims[pNumber] {
			parts = append(parts, "z.number()")
		}
		if s.prims[pBoolean] {
			parts = append(parts, "z.boolean()")
		}
		if s.prims[pNull] {
			parts = append(parts, "z.null()")
		}
		switch len(parts) {
		case 0:
			return "z.unknown()"
		case 1:
			return parts[0]
		}
		return "z.union([" + strings.Join(parts, ", ") + "])"
	}

	var rootName string
	if isRootObject(root) {
		rootName = object(name, root.obj)
	} else {
		rootName = n.unique(name)
		body := zod(root, name)
		blocks = append([]string{"export const " + rootName + " = " + body + ";"}, blocks...)
	}
	// Children before parents.
	for i, j := 0, len(blocks)-1; i < j; i, j = i+1, j-1 {
		blocks[i], blocks[j] = blocks[j], blocks[i]
	}
	return "// " + note + "\n" +
		"import { z } from \"zod\";\n\n" +
		strings.Join(blocks, "\n\n") + "\n\n" +
		"export type " + rootName + " = z.infer<typeof " + rootName + ">;\n"
}
