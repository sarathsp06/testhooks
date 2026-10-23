package wasm

import (
	"context"
	"strings"
	"testing"
	"time"
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

// Even with os in hand (the raw eval skips the prelude), the filesystem is an
// empty, read-only directory, not the server's working directory.
func TestSandbox_NoServerFiles(t *testing.T) {
	r := newTestRunner(t)
	// The test's working directory is this package, which has files in it.
	out, err := r.eval(context.Background(), "fs.js", `JSON.stringify(os.readdir(".")[0])`)
	if err != nil {
		t.Fatal(err)
	}
	if out != `[".",".."]` {
		t.Fatalf("script can see files: %s", out)
	}
	out, err = r.eval(context.Background(), "w.js", `String(os.open("x.txt", os.O_WRONLY | os.O_CREAT, 420) < 0)`)
	if err != nil || out != "true" {
		t.Fatalf("script could create a file: %q %v", out, err)
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
