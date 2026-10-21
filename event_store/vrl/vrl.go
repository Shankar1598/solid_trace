// Package vrl compiles VRL programs and runs them on Logs, through the Rust
// vrl crate built as a staticlib (rust/) and called over cgo. Logs cross as
// msgpack. Run ./build before go build or go test; see docs/adr/0012.
package vrl

/*
#cgo linux,amd64 LDFLAGS: ${SRCDIR}/lib/linux_amd64/libst_vrl.a -lgcc_s -lutil -lrt -lpthread -lm -ldl
#cgo linux,arm64 LDFLAGS: ${SRCDIR}/lib/linux_arm64/libst_vrl.a -lgcc_s -lutil -lrt -lpthread -lm -ldl
#include "vrl.h"
*/
import "C"

import (
	"sync"
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
