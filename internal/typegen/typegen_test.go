package typegen

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// cases reads testdata/index.tsv: one line per case, "id<TAB>type name".
func cases(t *testing.T) [][2]string {
	t.Helper()
	f, err := os.Open("testdata/index.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out [][2]string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		id, name, ok := strings.Cut(sc.Text(), "\t")
		if ok {
			out = append(out, [2]string{id, name})
		}
	}
	return out
}

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestMatchesTheUIGenerator checks the TypeScript and JSON Schema output
// against golden files captured from web/src/lib/typegen.ts before it was
// replaced by this package, so "Copy as type" did not change.
func TestMatchesTheUIGenerator(t *testing.T) {
	for _, c := range cases(t) {
		id, name := c[0], c[1]
		body := read(t, id+".json")
		for lang, ext := range map[Lang]string{TypeScript: ".ts.golden", JSONSchema: ".schema.golden"} {
			got, err := Generate(lang, name, body)
			if err != nil {
				t.Errorf("%s %s: %v", id, lang, err)
				continue
			}
			if want := string(read(t, id+ext)); got != want {
				t.Errorf("%s %s differs from the UI's output.\n--- want\n%s\n--- got\n%s", id, lang, want, got)
			}
		}
	}
}

// TestEveryLanguageOnEveryCase makes sure no sample breaks an emitter, that
// Go output is gofmt-clean (toGo already runs gofmt, so a failure is an
// error) and that Python output parses.
func TestEveryLanguageOnEveryCase(t *testing.T) {
	python, _ := exec.LookPath("python3")
	for _, c := range cases(t) {
		id, name := c[0], c[1]
		body := read(t, id+".json")
		for _, lang := range Langs() {
			got, err := Generate(lang, name, body)
			if err != nil {
				t.Errorf("%s %s: %v", id, lang, err)
				continue
			}
			if !strings.Contains(got, "best effort") {
				t.Errorf("%s %s: missing the best-effort note", id, lang)
			}
			if lang == Pydantic && python != "" {
				cmd := exec.Command(python, "-I", "-c", "import ast,sys; ast.parse(sys.stdin.read())")
				cmd.Stdin = strings.NewReader(got)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Errorf("%s pydantic does not parse: %s\n%s", id, out, got)
				}
			}
		}
	}
}

func TestZod(t *testing.T) {
	got, err := Generate(Zod, "Nested", read(t, "edge_nested.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := `// inferred from one request, best effort
import { z } from "zod";

export const NestedListItem = z.object({
  id: z.number(),
  tag: z.string().optional(),
  extra: z.boolean().optional(),
});

export const NestedAB = z.object({
  c: z.array(z.number()),
});

export const NestedA = z.object({
  b: NestedAB,
});

export const Nested = z.object({
  a: NestedA,
  list: z.array(NestedListItem),
  empty: z.record(z.unknown()),
  emptyArr: z.array(z.unknown()),
  mixed: z.array(z.union([z.string(), z.number(), z.null()])),
  nested: z.array(z.array(z.number())),
});

export type Nested = z.infer<typeof Nested>;
`
	if got != want {
		t.Fatalf("--- want\n%s\n--- got\n%s", want, got)
	}
	got, _ = Generate(Zod, "RootArray", read(t, "edge_root_array.json"))
	if !strings.Contains(got, "export const RootArray = z.array(RootArrayItem);") || !strings.Contains(got, "name: z.string().optional(),") {
		t.Fatalf("root array:\n%s", got)
	}
}

func TestGo(t *testing.T) {
	got, err := Generate(Go, "Nested", read(t, "edge_nested.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := `// Inferred from one request, best effort.
type Nested struct {
	A        NestedA          ` + "`json:\"a\"`" + `
	List     []NestedListItem ` + "`json:\"list\"`" + `
	Empty    map[string]any   ` + "`json:\"empty\"`" + `
	EmptyArr []any            ` + "`json:\"emptyArr\"`" + `
	Mixed    []any            ` + "`json:\"mixed\"`" + `
	Nested   [][]int64        ` + "`json:\"nested\"`" + `
}

type NestedA struct {
	B NestedAB ` + "`json:\"b\"`" + `
}

type NestedAB struct {
	C []int64 ` + "`json:\"c\"`" + `
}

type NestedListItem struct {
	ID    int64   ` + "`json:\"id\"`" + `
	Tag   *string ` + "`json:\"tag,omitempty\"`" + `
	Extra *bool   ` + "`json:\"extra,omitempty\"`" + `
}
`
	if got != want {
		t.Fatalf("--- want\n%s\n--- got\n%s", want, got)
	}

	got, _ = Generate(Go, "Names", read(t, "edge_names.json"))
	got = squash(got)
	for _, want := range []string{"FirstName string `json:\"first-name\"`", "F2nd int64 `json:\"2nd\"`", "ID string", "URL string", "WeirdKey bool `json:\"$weird key\"`", "Item NamesItem", "ItemItem NamesItemItem"} {
		if !strings.Contains(got, want) {
			t.Errorf("names: missing %q in\n%s", want, got)
		}
	}
	got, _ = Generate(Go, "Nulls", read(t, "edge_null_union.json"))
	got = squash(got)
	if !strings.Contains(got, "A any `json:\"a\"`") || !strings.Contains(got, "B []*NullsBItem") {
		t.Errorf("nulls:\n%s", got)
	}
	got, _ = Generate(Go, "Scalars", read(t, "edge_scalars.json"))
	got = squash(got)
	if !strings.Contains(got, "F float64") || !strings.Contains(got, "E int64") || !strings.Contains(got, "One int64") {
		t.Errorf("scalars:\n%s", got)
	}
	if got, _ := Generate(Go, "RootScalar", read(t, "edge_root_scalar.json")); !strings.Contains(got, "type RootScalar = string") {
		t.Errorf("root scalar:\n%s", got)
	}
	// Keys that collide after casing get numbered.
	got, _ = Generate(Go, "Clash", []byte(`{"user_id":1,"userId":2,"UserID":3}`))
	got = squash(got)
	for _, want := range []string{"UserID int64 `json:\"user_id\"`", "UserID2 int64 `json:\"userId\"`", "UserID3 int64 `json:\"UserID\"`"} {
		if !strings.Contains(got, want) {
			t.Errorf("clash: missing %q in\n%s", want, got)
		}
	}
}

func TestPydantic(t *testing.T) {
	got, err := Generate(Pydantic, "Names", read(t, "edge_names.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"from pydantic import BaseModel, Field",
		`    first_name: str = Field(alias="first-name")`,
		`    f_2nd: int = Field(alias="2nd")`,
		`    class_: str = Field(alias="class")`,
		`    from_: str = Field(alias="from")`,
		`    weird_key: bool = Field(alias="$weird key")`,
		"class NamesItem(BaseModel):\n    x: int",
		"    items: list[NamesItemsItem]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	// Children are defined before the class that uses them.
	if strings.Index(got, "class NamesItem(") > strings.Index(got, "class Names(") {
		t.Errorf("child after parent:\n%s", got)
	}
	got, _ = Generate(Pydantic, "Nested", read(t, "edge_nested.json"))
	for _, want := range []string{"from typing import Any", "    tag: str | None = None", "    empty: dict[str, Any]", "    mixed: list[str | int | None]", "    nested: list[list[int]]"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	got, _ = Generate(Pydantic, "Shadow", []byte(`{"dict":1,"model_x":2,"json":{"a":1}}`))
	for _, want := range []string{`dict_: int = Field(alias="dict")`, `model_x_: int = Field(alias="model_x")`, `json_: ShadowJson = Field(alias="json")`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if got, _ := Generate(Pydantic, "RootArray", read(t, "edge_root_array.json")); !strings.Contains(got, "RootArray = list[RootArrayItem]") {
		t.Errorf("root array:\n%s", got)
	}
}

func TestMergingBodiesMakesMissingFieldsOptional(t *testing.T) {
	got, err := Generate(TypeScript, "Order", []byte(`{"id":1,"note":"x"}`), []byte(`{"id":2,"paid":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "  id: number;\n  note?: string;\n  paid?: boolean;\n") {
		t.Fatalf("got\n%s", got)
	}
	if _, err := Generate(TypeScript, "X", []byte(`{"a":1}`), []byte(`not json`)); err == nil || !strings.Contains(err.Error(), "body 2 is not JSON") {
		t.Fatalf("err = %v", err)
	}
	if _, err := Generate(TypeScript, "X", []byte(`{"a":1} {"b":2}`)); err == nil {
		t.Fatal("two values in one body should fail")
	}
}

func TestNamesAndLangs(t *testing.T) {
	if TypeName("invoice.paid") != "InvoicePaidPayload" || TypeName("") != "WebhookPayload" || TypeName("2fa_ok") != "T2faOkPayload" {
		t.Fatal(TypeName("invoice.paid"), TypeName(""), TypeName("2fa_ok"))
	}
	if EventOf([]byte(`{"type":"invoice.paid"}`)) != "invoice.paid" || EventOf([]byte(`{"action":"opened","type":3}`)) != "opened" || EventOf([]byte(`[1]`)) != "" {
		t.Fatal("EventOf")
	}
	for in, want := range map[string]Lang{"ts": TypeScript, "TypeScript": TypeScript, "schema": JSONSchema, "golang": Go, "py": Pydantic, "zod": Zod} {
		if got, err := ParseLang(in); err != nil || got != want {
			t.Errorf("ParseLang(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseLang("rust"); err == nil {
		t.Error("rust should be unknown")
	}
	if _, err := Generate(TypeScript, "X"); err == nil {
		t.Error("no bodies should fail")
	}
}

// squash collapses gofmt's alignment so substrings can be matched with one space.
func squash(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
