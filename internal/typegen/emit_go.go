package typegen

import (
	"fmt"
	"go/format"
	"strings"
)

// goInitialisms are spelled in capitals in Go field names, as gofmt's lint expects.
var goInitialisms = map[string]bool{
	"Id": true, "Url": true, "Uri": true, "Ip": true, "Http": true, "Https": true, "Json": true, "Api": true,
	"Uuid": true, "Sku": true, "Html": true, "Xml": true, "Sql": true, "Ssl": true, "Tls": true, "Cpu": true,
	"Ram": true, "Os": true, "Ui": true, "Utc": true, "Iso": true, "Eof": true, "Tcp": true, "Udp": true,
}

// goField turns a JSON key into an exported Go field name.
func goField(k string) string {
	var b strings.Builder
	for _, w := range nonAlnum.Split(k, -1) {
		if w == "" {
			continue
		}
		// Split camelCase words too, so "orderId" gives OrderID.
		for _, part := range splitCamel(w) {
			p := strings.ToUpper(part[:1]) + part[1:]
			if goInitialisms[p] {
				p = strings.ToUpper(p)
			}
			b.WriteString(p)
		}
	}
	out := b.String()
	if out == "" {
		return "Field"
	}
	if out[0] >= '0' && out[0] <= '9' {
		return "F" + out
	}
	return out
}

// splitCamel splits "orderId" into ["order", "Id"] and leaves "ORDER" alone.
func splitCamel(w string) []string {
	var parts []string
	start := 0
	for i := 1; i < len(w); i++ {
		if w[i] >= 'A' && w[i] <= 'Z' && w[i-1] >= 'a' && w[i-1] <= 'z' {
			parts = append(parts, w[start:i])
			start = i
		}
	}
	return append(parts, w[start:])
}

// toGo writes Go struct types, the root first, and formats them with gofmt.
// There is no package clause: the output is meant to be pasted into a file.
func toGo(root *shape, name, note string) (string, error) {
	var blocks []string
	n := &namer{}

	var gotype func(s *shape, hint string) string
	strct := func(name string, obj *objShape) string {
		own := n.unique(name)
		idx := len(blocks)
		blocks = append(blocks, "")
		used := map[string]bool{}
		var lines []string
		for _, k := range obj.keys {
			f := obj.fields[k]
			fname := goField(k)
			for i := 2; used[fname]; i++ {
				fname = fmt.Sprintf("%s%d", goField(k), i)
			}
			used[fname] = true
			t := gotype(f.shape, own+pascal(k))
			tag := k
			optional := f.count < obj.seen
			if optional {
				tag += ",omitempty"
				// A missing scalar or struct is a nil pointer; a slice or map is nil already.
				if !strings.HasPrefix(t, "[]") && !strings.HasPrefix(t, "map[") && !strings.HasPrefix(t, "*") && t != "any" {
					t = "*" + t
				}
			}
			lines = append(lines, fmt.Sprintf("\t%s %s `json:%q`", fname, t, tag))
		}
		blocks[idx] = "type " + own + " struct {\n" + strings.Join(lines, "\n") + "\n}"
		return own
	}
	gotype = func(s *shape, hint string) string {
		var parts []string
		if s.obj != nil {
			if len(s.obj.keys) > 0 {
				parts = append(parts, strct(hint, s.obj))
			} else {
				parts = append(parts, "map[string]any")
			}
		}
		if s.arr != nil {
			el := "any"
			if s.arr.items != nil {
				el = gotype(s.arr.items, hint+"Item")
			}
			parts = append(parts, "[]"+el)
		}
		if s.prims[pString] {
			parts = append(parts, "string")
		}
		if s.prims[pNumber] {
			parts = append(parts, "float64")
		} else if s.prims[pInteger] {
			parts = append(parts, "int64")
		}
		if s.prims[pBoolean] {
			parts = append(parts, "bool")
		}
		switch {
		case len(parts) == 0:
			return "any"
		case len(parts) == 1 && s.prims[pNull]:
			if strings.HasPrefix(parts[0], "[]") || strings.HasPrefix(parts[0], "map[") {
				return parts[0] // nil already means null
			}
			return "*" + parts[0]
		case len(parts) == 1:
			return parts[0]
		}
		return "any" // several types at one place: the caller decides
	}

	if isRootObject(root) {
		strct(name, root.obj)
	} else {
		n.unique(name)
		body := gotype(root, name)
		blocks = append([]string{"type " + name + " = " + body}, blocks...)
	}
	src := "// " + capitalise(note) + ".\n" + strings.Join(blocks, "\n\n") + "\n"
	out, err := format.Source([]byte(src))
	if err != nil {
		return "", fmt.Errorf("generated Go does not compile: %w\n%s", err, src)
	}
	return string(out), nil
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
