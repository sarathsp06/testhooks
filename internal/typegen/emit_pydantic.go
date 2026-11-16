package typegen

import (
	"fmt"
	"regexp"
	"strings"
)

var pyKeywords = map[string]bool{
	"False": true, "None": true, "True": true, "and": true, "as": true, "assert": true, "async": true,
	"await": true, "break": true, "class": true, "continue": true, "def": true, "del": true, "elif": true,
	"else": true, "except": true, "finally": true, "for": true, "from": true, "global": true, "if": true,
	"import": true, "in": true, "is": true, "lambda": true, "nonlocal": true, "not": true, "or": true,
	"pass": true, "raise": true, "return": true, "try": true, "while": true, "with": true, "yield": true,
	"match": true, "case": true, "type": false,
}

// pyShadows are BaseModel attributes a field must not be named after.
var pyShadows = map[string]bool{
	"dict": true, "json": true, "copy": true, "schema": true, "validate": true, "construct": true,
	"fields": true, "parse_obj": true, "parse_raw": true, "schema_json": true, "update_forward_refs": true,
}

var pyIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// pyField turns a JSON key into a Python attribute name, and says whether it
// differs from the key (then the field needs an alias).
func pyField(k string) (name string, aliased bool) {
	name = k
	if !pyIdent.MatchString(name) {
		name = strings.Trim(nonAlnum.ReplaceAllString(name, "_"), "_")
		if name == "" {
			name = "field"
		}
		if name[0] >= '0' && name[0] <= '9' {
			name = "f_" + name
		}
	}
	if pyKeywords[name] || pyShadows[name] || strings.HasPrefix(name, "model_") {
		name += "_"
	}
	return name, name != k
}

// toPydantic writes Pydantic v2 models, nested ones first since each must be
// defined before the model that uses it. No forward references, so the
// module loads the same whether it is imported, exec'd or pasted.
func toPydantic(root *shape, name, note string) string {
	var blocks []string // parents first, reversed at the end
	n := &namer{}
	needField, needAny := false, false

	var py func(s *shape, hint string) string
	model := func(name string, obj *objShape) string {
		own := n.unique(name)
		idx := len(blocks)
		blocks = append(blocks, "")
		used := map[string]bool{}
		var lines []string
		for _, k := range obj.keys {
			f := obj.fields[k]
			fname, aliased := pyField(k)
			for i := 2; used[fname]; i++ {
				fname = fmt.Sprintf("%s_%d", strings.TrimSuffix(fname, "_"), i)
				aliased = true
			}
			used[fname] = true
			t := py(f.shape, own+pascal(k))
			optional := f.count < obj.seen
			if optional && !strings.HasSuffix(t, "| None") && t != "None" {
				t += " | None"
			}
			line := "    " + fname + ": " + t
			switch {
			case aliased && optional:
				needField = true
				line += " = Field(None, alias=" + quoteJS(k) + ")"
			case aliased:
				needField = true
				line += " = Field(alias=" + quoteJS(k) + ")"
			case optional:
				line += " = None"
			}
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			lines = []string{"    pass"}
		}
		blocks[idx] = "class " + own + "(BaseModel):\n" + strings.Join(lines, "\n")
		return own
	}
	py = func(s *shape, hint string) string {
		var parts []string
		if s.obj != nil {
			if len(s.obj.keys) > 0 {
				parts = append(parts, model(hint, s.obj))
			} else {
				needAny = true
				parts = append(parts, "dict[str, Any]")
			}
		}
		if s.arr != nil {
			el := "Any"
			if s.arr.items != nil {
				el = py(s.arr.items, hint+"Item")
			} else {
				needAny = true
			}
			parts = append(parts, "list["+el+"]")
		}
		if s.prims[pString] {
			parts = append(parts, "str")
		}
		if s.prims[pNumber] {
			parts = append(parts, "float")
		} else if s.prims[pInteger] {
			parts = append(parts, "int")
		}
		if s.prims[pBoolean] {
			parts = append(parts, "bool")
		}
		if s.prims[pNull] {
			parts = append(parts, "None")
		}
		if len(parts) == 0 {
			needAny = true
			return "Any"
		}
		return strings.Join(parts, " | ")
	}

	var rootName string
	if isRootObject(root) {
		rootName = model(name, root.obj)
	} else {
		rootName = n.unique(name)
		body := py(root, name)
		blocks = append([]string{rootName + " = " + body}, blocks...)
	}
	for i, j := 0, len(blocks)-1; i < j; i, j = i+1, j-1 {
		blocks[i], blocks[j] = blocks[j], blocks[i]
	}

	var b strings.Builder
	b.WriteString("# " + note + "\n")
	if needAny {
		b.WriteString("from typing import Any\n\n")
	}
	if needField {
		b.WriteString("from pydantic import BaseModel, Field\n")
	} else {
		b.WriteString("from pydantic import BaseModel\n")
	}
	for _, block := range blocks {
		b.WriteString("\n\n\n") // two blank lines between top-level definitions, as PEP 8 asks
		b.WriteString(block)
	}
	b.WriteString("\n")
	return b.String()
}
