package wasm

import (
	"bytes"
	"errors"
	"fmt"
	_ "unsafe" // for go:linkname

	_ "github.com/fastschema/qjs" // provides qjsWasm below
)

// qjsWasm is the QuickJS WebAssembly module embedded in fastschema/qjs. It
// isn't exported; the link fails at build time if qjs renames it.
//
//go:linkname qjsWasm github.com/fastschema/qjs.wasmBytes
var qjsWasm []byte

const wasmPageSize = 64 * 1024

// cappedQJSWasm returns the QuickJS module with its linear memory capped at
// limit bytes. qjs passes Option.MemoryLimit to QuickJS but it isn't enforced,
// so without a cap a script can grow the runtime to wasm32's 4 GB. wazero
// enforces a module's declared maximum: memory.grow past it fails, malloc
// returns NULL and QuickJS throws "out of memory".
func cappedQJSWasm(limit int) ([]byte, error) {
	if len(qjsWasm) == 0 {
		return nil, errors.New("qjs wasm module not found")
	}
	pages := uint32(limit / wasmPageSize)
	if pages == 0 {
		return nil, fmt.Errorf("memory limit %d is below one wasm page", limit)
	}
	return capMemory(qjsWasm, pages)
}

// capMemory rewrites a wasm module's memory section to declare maxPages as the
// maximum. It expects one defined (not imported) memory.
func capMemory(module []byte, maxPages uint32) ([]byte, error) {
	if len(module) < 8 || !bytes.Equal(module[:4], []byte("\x00asm")) {
		return nil, errors.New("not a wasm module")
	}
	i := 8
	for i < len(module) {
		id := module[i]
		size, n, err := readULEB(module[i+1:])
		if err != nil {
			return nil, err
		}
		body := i + 1 + n
		end := body + int(size)
		if end > len(module) {
			return nil, errors.New("truncated wasm section")
		}
		if id != 5 { // memory section
			i = end
			continue
		}

		count, n1, err := readULEB(module[body:end])
		if err != nil || count != 1 {
			return nil, fmt.Errorf("expected one memory, got %d", count)
		}
		p := body + n1
		flags := module[p]
		minPages, _, err := readULEB(module[p+1 : end])
		if err != nil {
			return nil, err
		}
		if flags&^1 != 0 {
			return nil, fmt.Errorf("unsupported memory flags %#x", flags)
		}
		if maxPages < minPages {
			return nil, fmt.Errorf("memory cap of %d pages is below the module's minimum of %d", maxPages, minPages)
		}

		var sec []byte
		sec = appendULEB(sec, 1)
		sec = append(sec, 0x01) // has maximum
		sec = appendULEB(sec, minPages)
		sec = appendULEB(sec, maxPages)

		out := make([]byte, 0, len(module)+8)
		out = append(out, module[:i]...)
		out = append(out, 5)
		out = appendULEB(out, uint32(len(sec)))
		out = append(out, sec...)
		out = append(out, module[end:]...)
		return out, nil
	}
	return nil, errors.New("wasm module has no memory section")
}

func readULEB(b []byte) (uint32, int, error) {
	var v uint32
	for i := 0; i < len(b) && i < 5; i++ {
		v |= uint32(b[i]&0x7f) << (7 * i)
		if b[i] < 0x80 {
			return v, i + 1, nil
		}
	}
	return 0, 0, errors.New("bad LEB128 value in wasm module")
}

func appendULEB(b []byte, v uint32) []byte {
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b = append(b, c|0x80)
			continue
		}
		return append(b, c)
	}
}
