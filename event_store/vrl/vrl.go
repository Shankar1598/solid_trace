// Package vrl compiles VRL programs and runs them on Logs, through the Rust
// vrl crate built as a staticlib (rust/) and called over cgo. Logs cross as
// msgpack. Run ./build before go build or go test. See
// docs/adr/0012-log-transform-runs-vrl-through-cgo.md.
package vrl

/*
#cgo linux,amd64 LDFLAGS: ${SRCDIR}/lib/linux_amd64/libst_vrl.a -lgcc_s -lutil -lrt -lpthread -lm -ldl -Wl,-z,noexecstack
#cgo linux,arm64 LDFLAGS: ${SRCDIR}/lib/linux_arm64/libst_vrl.a -lgcc_s -lutil -lrt -lpthread -lm -ldl -Wl,-z,noexecstack
#include "vrl.h"
*/
import "C"

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/vmihailenco/msgpack/v5"
)

// Diagnostic is one of VRL's compile errors or warnings.
type Diagnostic struct {
	Severity  string   `msgpack:"severity"` // "error", "warning", "note" or "bug"
	Code      int      `msgpack:"code"`     // E105 is 105
	Message   string   `msgpack:"message"`
	Labels    []Label  `msgpack:"labels"`
	Notes     []string `msgpack:"notes"`
	Formatted string   `msgpack:"formatted"` // VRL's own text, quoting the source
}

// Label points at the part of the source a Diagnostic is about. Start and
// End are byte offsets into the source.
type Label struct {
	Message string `msgpack:"message"`
	Primary bool   `msgpack:"primary"`
	Start   int    `msgpack:"start"`
	End     int    `msgpack:"end"`
}

// Program is a compiled VRL program. It is safe for concurrent use, and
// must be freed with Free.
type Program struct {
	mu  sync.RWMutex
	ptr unsafe.Pointer
}

// Compile compiles source. It returns nil and the errors when the program
// doesn't compile; otherwise the program and any warnings.
func Compile(source string) (*Program, []Diagnostic) {
	var out C.st_vrl_buf
	ptr := C.st_vrl_compile((*C.uint8_t)(unsafe.Pointer(unsafe.StringData(source))), C.size_t(len(source)), &out)
	defer C.st_vrl_buf_free(out)
	var diagnostics []Diagnostic
	if err := msgpack.Unmarshal(view(out), &diagnostics); err != nil {
		diagnostics = append(diagnostics, Diagnostic{Severity: "bug", Message: err.Error()})
	}
	if ptr == nil {
		return nil, diagnostics
	}
	return &Program{ptr: ptr}, diagnostics
}

// Outcome is what a program did with one Log: returned it, dropped it with
// abort, or failed on it.
type Outcome struct {
	Log     map[string]any // the Log the program returned
	Dropped bool           // the program called abort
	Error   string         // why the program failed on the Log
}

// The outcomes' kinds, as Rust writes them.
const (
	kept    = 0
	dropped = 1
	failed  = 2
)

// Run runs the program on logs in one call, and returns one Outcome per
// Log. A Log's values are strings, int64s, float64s, bools, nils,
// time.Times, []any and map[string]any; a returned Log holds the same, with
// times in UTC. It returns an error only when the call as a whole fails.
func (p *Program) Run(logs []map[string]any) ([]Outcome, error) {
	input, err := encodeLogs(logs)
	if err != nil {
		return nil, err
	}
	var out C.st_vrl_buf
	status, err := p.run(input, &out)
	if err != nil {
		return nil, err
	}
	defer C.st_vrl_buf_free(out)
	if status != 0 {
		return nil, fmt.Errorf("vrl: %s", view(out))
	}
	return decodeOutcomes(view(out), len(logs))
}

func (p *Program) run(input []byte, out *C.st_vrl_buf) (C.int32_t, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.ptr == nil {
		return 0, errors.New("vrl: the program was freed")
	}
	return C.st_vrl_run(p.ptr, (*C.uint8_t)(unsafe.SliceData(input)), C.size_t(len(input)), out), nil
}

// encodeLogs writes logs as a msgpack array of bin, one per Log, so Rust
// can read each Log apart and fail one without losing its place.
func encodeLogs(logs []map[string]any) ([]byte, error) {
	var all, one bytes.Buffer
	outer, inner := msgpack.NewEncoder(&all), msgpack.NewEncoder(&one)
	if err := outer.EncodeArrayLen(len(logs)); err != nil {
		return nil, err
	}
	for _, log := range logs {
		one.Reset()
		if err := inner.Encode(log); err != nil {
			return nil, fmt.Errorf("vrl: encoding a Log: %w", err)
		}
		if err := outer.EncodeBytes(one.Bytes()); err != nil {
			return nil, err
		}
	}
	return all.Bytes(), nil
}

func decodeOutcomes(data []byte, want int) ([]Outcome, error) {
	dec := msgpack.NewDecoder(bytes.NewReader(data))
	dec.UseLooseInterfaceDecoding(true)
	n, err := dec.DecodeArrayLen()
	if err != nil || n != want {
		return nil, fmt.Errorf("vrl: %d outcomes for %d Logs: %v", n, want, err)
	}
	outcomes := make([]Outcome, n)
	for i := range outcomes {
		if _, err := dec.DecodeArrayLen(); err != nil {
			return nil, fmt.Errorf("vrl: reading outcome %d: %w", i, err)
		}
		kind, err := dec.DecodeInt()
		if err != nil {
			return nil, fmt.Errorf("vrl: reading outcome %d: %w", i, err)
		}
		switch kind {
		case kept:
			outcomes[i].Log, err = dec.DecodeMap()
			inUTC(outcomes[i].Log)
		case dropped:
			outcomes[i].Dropped = true
		case failed:
			outcomes[i].Error, err = dec.DecodeString()
		default:
			err = fmt.Errorf("unknown kind %d", kind)
		}
		if err != nil {
			return nil, fmt.Errorf("vrl: reading outcome %d: %w", i, err)
		}
	}
	return outcomes, nil
}

// inUTC puts the times in v in UTC, where msgpack decodes them in Local.
func inUTC(v any) any {
	switch v := v.(type) {
	case time.Time:
		return v.UTC()
	case map[string]any:
		for k, x := range v {
			switch x.(type) {
			case time.Time, map[string]any, []any:
				v[k] = inUTC(x)
			}
		}
	case []any:
		for i, x := range v {
			v[i] = inUTC(x)
		}
	}
	return v
}

// Free frees the program. It waits for runs in flight; later runs fail.
func (p *Program) Free() {
	p.mu.Lock()
	defer p.mu.Unlock()
	C.st_vrl_program_free(p.ptr)
	p.ptr = nil
}

// view gives the bytes of a buffer Rust returned, valid until it is freed.
func view(buf C.st_vrl_buf) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(buf.ptr)), int(buf.len))
}
