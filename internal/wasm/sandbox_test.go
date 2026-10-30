package wasm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fastschema/qjs"
	"github.com/rs/zerolog"
)

// Scripts must not see qjs's host modules (std, os): they read and write files.
func TestSandbox_NoHostModules(t *testing.T) {
	r := newTestRunner(t)
	in := TransformInput{Method: "POST", Path: "/", Headers: map[string][]string{}, Body: "{}"}
	out, err := r.Transform(context.Background(),
		`function transform(req) { req.body = [typeof std, typeof os, typeof print, typeof scriptArgs].join(","); return req; }`, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Body != "undefined,undefined,undefined,undefined" {
		t.Fatalf("host globals visible to scripts: %s", out.Body)
	}
}

// Even with os in hand (a bare runtime, without the prelude), the filesystem
// is an empty, read-only directory, not the server's working directory.
func TestSandbox_NoServerFiles(t *testing.T) {
	r := newTestRunner(t)
	rt, err := qjs.New(r.option)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	eval := func(code string) string {
		v, err := rt.Eval("fs.js", qjs.Code(code))
		if err != nil {
			t.Fatal(err)
		}
		defer v.Free()
		return v.String()
	}
	// The test's working directory is this package, which has files in it.
	if out := eval(`JSON.stringify(os.readdir(".")[0])`); out != `[".",".."]` {
		t.Fatalf("script can see files: %s", out)
	}
	if out := eval(`String(os.open("x.txt", os.O_WRONLY | os.O_CREAT, 420) < 0)`); out != "true" {
		t.Fatalf("script could create a file: %q", out)
	}
}

// Runtimes are reused, so nothing a script leaves behind may reach the next
// one: globals, changed built-ins, or the Math.random sequence.
func TestSandbox_NoStateBetweenRuns(t *testing.T) {
	r := newTestRunner(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		out, err := r.eval(ctx, "a.js", `
			var before = [typeof leaked, typeof globalThis.alsoLeaked, typeof Object.prototype.polluted, JSON.stringify.name].join(",");
			var leaked = 1; globalThis.alsoLeaked = 2; Object.prototype.polluted = 3; JSON.stringify = function hijacked() {};
			before`)
		if err != nil {
			t.Fatal(err)
		}
		if out != "undefined,undefined,undefined,stringify" {
			t.Fatalf("run %d saw state from an earlier run: %s", i, out)
		}
	}
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		out, err := r.eval(ctx, "r.js", `String(Math.random())`)
		if err != nil {
			t.Fatal(err)
		}
		if seen[out] {
			t.Fatalf("Math.random repeated %s across runs", out)
		}
		seen[out] = true
	}
}

// A run that times out or fills memory loses its runtime; the next run gets a
// working one.
func TestSandbox_RecoversAfterFailedRun(t *testing.T) {
	log := zerolog.Nop()
	r, err := New(context.Background(), Config{Timeout: 200 * time.Millisecond, PoolSize: 1, MemoryLimit: 32 << 20, MaxStackSize: 512 << 10}, log)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	ctx := context.Background()
	if _, err := r.eval(ctx, "loop.js", `for (;;) {}`); err == nil || !strings.Contains(err.Error(), "longer than") {
		t.Fatalf("infinite loop: err = %v", err)
	}
	if out, err := r.eval(ctx, "ok.js", `String(1 + 1)`); err != nil || out != "2" {
		t.Fatalf("after timeout: %q %v", out, err)
	}
	if _, err := r.eval(ctx, "m.js", `var a = []; for (;;) a.push("x".repeat(1000000))`); err == nil {
		t.Fatal("unbounded allocation succeeded")
	}
	if out, err := r.eval(ctx, "ok.js", `String(1 + 1)`); err != nil || out != "2" {
		t.Fatalf("after out of memory: %q %v", out, err)
	}
	if _, err := r.eval(ctx, "throw.js", `throw new Error("boom")`); err == nil {
		t.Fatal("throw: want error")
	}
	if out, err := r.eval(ctx, "ok.js", `String(1 + 1)`); err != nil || out != "2" {
		t.Fatalf("after throw: %q %v", out, err)
	}
}

// qjs doesn't enforce MemoryLimit; the capped wasm memory does.
func TestSandbox_MemoryCapped(t *testing.T) {
	r := newTestRunner(t) // 32 MB
	start := time.Now()
	_, err := r.eval(context.Background(), "m.js", `var a = []; for (var i = 0; i < 100; i++) a.push("x".repeat(1000000) + i); a.length`)
	if err == nil || !strings.Contains(err.Error(), "out of memory") {
		t.Fatalf("100 MB allocation: err = %v, want out of memory", err)
	}
	// The cap must trip before the 5 s script timeout, which the "out of
	// memory" check above already proves (a timeout reports differently).
	// This is only a hang guard: it takes under 1 s locally and 3 to 4 s on
	// a shared CI runner, so the bound is generous on purpose.
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("took %s to hit the memory cap", took)
	}
	// Ordinary work well under the cap still runs.
	out, err := r.eval(context.Background(), "ok.js", `var a = []; for (var i = 0; i < 10; i++) a.push("x".repeat(1000000)); String(a.length)`)
	if err != nil || out != "10" {
		t.Fatalf("10 MB allocation: %q %v", out, err)
	}
}

func TestCapMemory(t *testing.T) {
	// Minimal module: header + memory section with one memory, min 3, no max.
	mod := []byte("\x00asm\x01\x00\x00\x00\x05\x03\x01\x00\x03")
	got, err := capMemory(mod, 1024)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("\x00asm\x01\x00\x00\x00\x05\x05\x01\x01\x03\x80\x08")
	if string(got) != string(want) {
		t.Fatalf("got % x, want % x", got, want)
	}
	if _, err := capMemory(mod, 2); err == nil {
		t.Fatal("cap below the minimum should fail")
	}
}
