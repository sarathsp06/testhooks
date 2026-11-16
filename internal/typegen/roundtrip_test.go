package typegen

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// extraCases are shapes the golden files don't cover: names that collide with
// a language's own words, leading underscores, an optional event field, an
// empty object root.
var extraCases = map[string]string{
	"underscore_id":  `{"_id":"abc","__v":1,"_meta":{"a":1}}`,
	"typeish_keys":   `[{"list":[1],"str":"s","int":1,"float":1.5,"bool":true,"dict":{"a":1},"type":"x"},{"list":[2],"str":"t","int":2,"float":2.5,"bool":false,"dict":{"a":2}}]`,
	"empty_root":     `{}`,
	"optional_event": `[{"type":"a.b","id":1},{"id":2}]`,
	"go_keywords":    `{"func":1,"type":"t","package":"p","range":[1],"map":{"k":1},"chan":null}`,
	"py_shadow":      `{"model_config":1,"copy":2,"schema":3,"None":4,"self":5,"id":"x"}`,
	"deep_arrays":    `{"m":[[[{"a":[null,1]}]],[]]}`,
}

// roundTripCases are every golden case plus the extras, with the JSON value a
// generated type must accept: the merged bodies as a JSON array when the case
// is an array of bodies, else the one body.
func roundTripCases(t *testing.T) map[string][][]byte {
	t.Helper()
	out := map[string][][]byte{}
	for _, c := range cases(t) {
		out[c[0]] = [][]byte{read(t, c[0]+".json")}
	}
	for id, body := range extraCases {
		var arr []json.RawMessage
		if strings.HasPrefix(body, "[{") && json.Unmarshal([]byte(body), &arr) == nil && id != "edge_root_array" {
			var bodies [][]byte
			for _, b := range arr {
				bodies = append(bodies, b)
			}
			out[id] = bodies
		} else {
			out[id] = [][]byte{[]byte(body)}
		}
	}
	return out
}

// TestGoOutputTypeChecks puts a package clause in front of the generated Go
// and type-checks it with go/types, so every struct, tag and pointer is legal.
func TestGoOutputTypeChecks(t *testing.T) {
	fset := token.NewFileSet()
	for id, bodies := range roundTripCases(t) {
		src, err := Generate(Go, "Payload", bodies...)
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		f, err := parser.ParseFile(fset, id+".go", "package p\n\n"+src, 0)
		if err != nil {
			t.Errorf("%s: parse: %v\n%s", id, err, src)
			continue
		}
		conf := types.Config{Error: func(err error) { t.Errorf("%s: %v\n%s", id, err, src) }}
		_, _ = conf.Check("p", fset, []*ast.File{f}, nil)
	}
}

// TestPydanticOutputValidatesTheBody imports each generated module with the
// Pydantic in TYPEGEN_PYTHON (a python3 that has pydantic) and validates the
// source bodies with it. Skipped when no such python is available.
func TestPydanticOutputValidatesTheBody(t *testing.T) {
	python := os.Getenv("TYPEGEN_PYTHON")
	if python == "" {
		python, _ = exec.LookPath("python3")
	}
	if python == "" || exec.Command(python, "-c", "import pydantic").Run() != nil {
		t.Skip("no python3 with pydantic; set TYPEGEN_PYTHON to run this")
	}
	dir := t.TempDir()
	for id, bodies := range roundTripCases(t) {
		src, err := Generate(Pydantic, "Payload", bodies...)
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		mod := filepath.Join(dir, "m_"+id+".py")
		if err := os.WriteFile(mod, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(rawList(bodies))
		script := `import json, sys, importlib.util
spec = importlib.util.spec_from_file_location("m", sys.argv[1]); m = importlib.util.module_from_spec(spec); sys.modules["m"] = m; spec.loader.exec_module(m)
from pydantic import TypeAdapter
ta = TypeAdapter(m.Payload)
for body in json.loads(sys.argv[2]):
    ta.validate_python(body)
`
		cmd := exec.Command(python, "-I", "-c", script, mod, string(data))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s: pydantic rejects its own body: %s\n%s", id, out, src)
		}
	}
}

// TestZodOutputParsesTheBody type-checks each generated Zod file with tsc and
// parses the source bodies with it, using the zod and typescript found in
// TYPEGEN_NODE_MODULES. Skipped when that is not set.
func TestZodOutputParsesTheBody(t *testing.T) {
	mods := os.Getenv("TYPEGEN_NODE_MODULES")
	node, _ := exec.LookPath("node")
	if mods == "" || node == "" {
		t.Skip("set TYPEGEN_NODE_MODULES to a node_modules with zod and typescript to run this")
	}
	dir := t.TempDir()
	if err := os.Symlink(mods, filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	var files []string
	all := roundTripCases(t)
	for id, bodies := range all {
		src, err := Generate(Zod, "Payload", bodies...)
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		data, _ := json.Marshal(rawList(bodies))
		src += "\nfor (const body of " + string(data) + ") Payload.parse(body);\n"
		f := filepath.Join(dir, "z_"+id+".ts")
		if err := os.WriteFile(f, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	tsc := filepath.Join(mods, ".bin", "tsc")
	args := append([]string{"--noEmit", "--strict", "--target", "es2022", "--module", "nodenext", "--moduleResolution", "nodenext", "--skipLibCheck"}, files...)
	if out, err := exec.Command(tsc, args...).CombinedOutput(); err != nil {
		t.Errorf("tsc: %s", out)
	}
	for _, f := range files {
		if out, err := exec.Command(node, "--experimental-strip-types", f).CombinedOutput(); err != nil {
			src, _ := os.ReadFile(f)
			t.Errorf("%s: zod rejects its own body: %s\n%s", filepath.Base(f), out, src)
		}
	}
}

func rawList(bodies [][]byte) []json.RawMessage {
	out := make([]json.RawMessage, len(bodies))
	for i, b := range bodies {
		out[i] = json.RawMessage(b)
	}
	return out
}
