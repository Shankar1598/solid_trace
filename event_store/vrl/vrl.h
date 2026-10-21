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

void st_vrl_program_free(void *program);

void st_vrl_buf_free(st_vrl_buf buf);
