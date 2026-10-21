// The VRL runtime's C interface, built from rust/ by ./build. Every buffer
// Rust returns is freed with st_vrl_buf_free, and every program with
// st_vrl_program_free. No function unwinds a panic into the caller.

#include <stddef.h>
#include <stdint.h>

typedef struct {
	uint8_t *ptr;
	size_t len;
} st_vrl_buf;

// Compiles the program source[0:len]. Returns null when it has errors.
// Writes its diagnostics to *diagnostics as a msgpack array of maps.
void *st_vrl_compile(const uint8_t *source, size_t len, st_vrl_buf *diagnostics);

// Runs a program on logs[0:len], a msgpack array of bin, each one Log's
// msgpack map. Writes one outcome per Log to *outcomes as a msgpack array:
// [0, log] kept, [1] dropped by abort, [2, error] failed. Returns 0, or 1
// with the error text in *outcomes when the whole call fails.
int32_t st_vrl_run(const void *program, const uint8_t *logs, size_t len, st_vrl_buf *outcomes);

void st_vrl_program_free(void *program);

void st_vrl_buf_free(st_vrl_buf buf);
