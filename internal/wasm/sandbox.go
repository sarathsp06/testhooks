package wasm

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/fastschema/qjs"
)

// A sandbox is a QuickJS runtime that is reused between scripts. Creating a
// runtime costs about 3 ms (a new wasm instance plus QuickJS and prelude
// setup), many times the cost of a typical script. A QuickJS runtime keeps all
// of its state in the wasm instance's linear memory, so copying the memory
// back from a snapshot taken right after setup returns the runtime to exactly
// that state: the next script sees nothing the previous one did.
type sandbox struct {
	rt  *qjs.Runtime
	ctx *evalContext
	// snapshot is linear memory from address 1 (0 is the null page, which
	// qjs's Mem won't address) right after setup.
	snapshot []byte
	// size is the memory size the snapshot was taken at. A script that grew
	// memory leaves a runtime that is thrown away rather than reset.
	size uint32
}

// randomSetup replaces Math.random with a generator seeded before each run.
// QuickJS keeps its own generator's state in linear memory, so after a reset
// every run would see the same sequence of numbers.
const randomSetup = `
(function () {
  var a = 0, b = 0, c = 0, d = 1;
  // sfc32: fast, small state, plenty for Math.random.
  function next() {
    var t = (((a + b) | 0) + d) | 0;
    d = (d + 1) | 0;
    a = b ^ (b >>> 9);
    b = (c + (c << 3)) | 0;
    c = (c << 21) | (c >>> 11);
    c = (c + t) | 0;
    return t >>> 0;
  }
  Math.random = function random() {
    return ((next() >>> 5) * 67108864 + (next() >>> 6)) / 9007199254740992;
  };
  Object.defineProperty(globalThis, '__th_seed', {
    enumerable: false,
    value: function (s0, s1, s2, s3) {
      a = s0; b = s1; c = s2; d = s3;
      for (var i = 0; i < 12; i++) next();
    }
  });
})();
`

// seedProgram returns a call that seeds Math.random for one run.
func seedProgram() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("__th_seed(%d, %d, %d, %d)",
		binary.LittleEndian.Uint32(b[0:]), binary.LittleEndian.Uint32(b[4:]),
		binary.LittleEndian.Uint32(b[8:]), binary.LittleEndian.Uint32(b[12:]))
}

// sandbox returns an idle runtime, or sets up a new one. The caller must hold
// a slot (acquire) and hand the runtime back with recycle.
func (r *Runner) sandbox() (*sandbox, error) {
	select {
	case sb := <-r.idle:
		return sb, nil
	default:
	}

	ectx := &evalContext{Context: context.Background()}
	ectx.set(context.Background())
	opt := r.option
	opt.Context = ectx
	opt.CloseOnContextDone = true
	rt, err := qjs.New(opt)
	if err != nil {
		return nil, err
	}
	sb := &sandbox{rt: rt, ctx: ectx}

	val, err := evalSetup(rt, prelude+"\n"+randomSetup)
	if err != nil {
		sb.close()
		return nil, fmt.Errorf("load prelude: %w", err)
	}
	val.Free()

	mem := rt.Mem()
	sb.size = mem.Size()
	view, err := mem.Read(1, uint64(sb.size-1))
	if err != nil {
		sb.close()
		return nil, fmt.Errorf("snapshot runtime: %w", err)
	}
	sb.snapshot = append([]byte(nil), view...)
	return sb, nil
}

// evalSetup runs trusted setup code, turning qjs's panics into errors.
func evalSetup(rt *qjs.Runtime, code string) (val *qjs.Value, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%v", p)
		}
	}()
	return rt.Eval("prelude.js", qjs.Code(code))
}

// recycle resets a runtime to its snapshot and returns it to the idle pool,
// or closes it when it can't be reused.
func (r *Runner) recycle(sb *sandbox, reusable bool) {
	if reusable && r.ready.Load() && sb.reset() {
		select {
		case r.idle <- sb:
			return
		default:
		}
	}
	sb.close()
}

// reset copies the snapshot back into linear memory. It reports false when
// the memory has grown since the snapshot, which a copy can't undo.
func (sb *sandbox) reset() (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	mem := sb.rt.Mem()
	if mem.Size() != sb.size {
		return false
	}
	view, err := mem.Read(1, uint64(sb.size-1))
	if err != nil {
		return false
	}
	copy(view, sb.snapshot)
	return true
}

// close frees the runtime. After a timeout the module is closed and qjs
// panics on any further call, including Close; there's nothing left to free
// then.
func (sb *sandbox) close() {
	defer func() { _ = recover() }()
	sb.rt.Close()
}

// evalContext is the context a sandbox's runtime was created with. qjs fixes
// a runtime's context at creation, and wazero closes the module when the
// context passed to a call is done, so each run points it at that run's
// context: the run's deadline applies to the reused runtime.
type evalContext struct {
	context.Context // only for Value
	cur             atomic.Pointer[context.Context]
}

func (c *evalContext) set(ctx context.Context) { c.cur.Store(&ctx) }

func (c *evalContext) current() context.Context { return *c.cur.Load() }

func (c *evalContext) Deadline() (time.Time, bool) { return c.current().Deadline() }

func (c *evalContext) Done() <-chan struct{} { return c.current().Done() }

func (c *evalContext) Err() error { return c.current().Err() }
