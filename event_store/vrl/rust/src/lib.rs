//! The VRL runtime behind EventStore's `vrl` Go package. Go calls the
//! `st_vrl_*` functions over cgo; see `../vrl.h` for the contract.
//!
//! Every export catches panics: one must never unwind into Go.

use std::any::Any;
use std::panic::{self, AssertUnwindSafe};
use std::sync::Once;

use vrl::compiler::{self, Function, Program, TimeZone};
use vrl::diagnostic::{Diagnostic, Formatter, Severity};

/// Functions left out by name: they have no cap on what they write, so a small
/// Log could make them exhaust memory.
const LEFT_OUT: &[&str] = &["decode_gzip", "decode_zlib", "decode_zstd", "decode_snappy"];

/// A byte buffer Rust allocated and Go hands back to `st_vrl_buf_free`.
#[repr(C)]
pub struct Buf {
    ptr: *mut u8,
    len: usize,
}

impl Buf {
    fn from_vec(bytes: Vec<u8>) -> Buf {
        let boxed = bytes.into_boxed_slice();
        let len = boxed.len();
        Buf { ptr: Box::into_raw(boxed) as *mut u8, len }
    }
}

/// A compiled program and what it runs with.
pub struct Compiled {
    program: Program,
    timezone: TimeZone,
}

fn functions() -> Vec<Box<dyn Function>> {
    let mut functions = vrl::stdlib::all();
    functions.retain(|f| !LEFT_OUT.contains(&f.identifier()));
    functions
}

/// Keeps panics off stderr: each one is reported as a failure instead.
fn quiet_panics() {
    static ONCE: Once = Once::new();
    ONCE.call_once(|| panic::set_hook(Box::new(|_| {})));
}

fn panic_text(payload: Box<dyn Any + Send>) -> String {
    let message = payload
        .downcast_ref::<&str>()
        .map(|s| s.to_string())
        .or_else(|| payload.downcast_ref::<String>().cloned())
        .unwrap_or_else(|| "unknown".to_string());
    format!("panic: {message}")
}

unsafe fn bytes<'a>(ptr: *const u8, len: usize) -> &'a [u8] {
    if len == 0 {
        &[]
    } else {
        unsafe { std::slice::from_raw_parts(ptr, len) }
    }
}

/// Compiles `source`. Returns the program, or null when it has errors. Writes
/// its diagnostics (errors, or the program's warnings) to `diagnostics` as a
/// msgpack array of maps.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn st_vrl_compile(
    source: *const u8,
    len: usize,
    diagnostics: *mut Buf,
) -> *mut Compiled {
    quiet_panics();
    let source = String::from_utf8_lossy(unsafe { bytes(source, len) }).into_owned();
    let (compiled, list) = match panic::catch_unwind(|| compile(&source)) {
        Ok(result) => result,
        Err(payload) => (
            None,
            vec![Diagnostic {
                severity: Severity::Bug,
                code: 0,
                message: panic_text(payload),
                labels: vec![],
                notes: vec![],
            }],
        ),
    };
    let encoded = panic::catch_unwind(AssertUnwindSafe(|| encode_diagnostics(&source, &list)))
        .unwrap_or_else(|_| vec![0x90]); // an empty msgpack array
    unsafe { *diagnostics = Buf::from_vec(encoded) };
    match compiled {
        Some(compiled) => Box::into_raw(Box::new(compiled)),
        None => std::ptr::null_mut(),
    }
}

fn compile(source: &str) -> (Option<Compiled>, Vec<Diagnostic>) {
    match compiler::compile(source, &functions()) {
        Ok(result) => {
            let compiled = Compiled {
                program: result.program,
                timezone: TimeZone::parse("UTC").expect("UTC is a time zone"),
            };
            (Some(compiled), result.warnings.into_iter().collect())
        }
        Err(list) => (None, list.into_iter().collect()),
    }
}

fn encode_diagnostics(source: &str, list: &[Diagnostic]) -> Vec<u8> {
    let mut out = Vec::new();
    let w = &mut out;
    rmp::encode::write_array_len(w, list.len() as u32).unwrap();
    for d in list {
        let severity = match d.severity {
            Severity::Bug => "bug",
            Severity::Error => "error",
            Severity::Warning => "warning",
            Severity::Note => "note",
        };
        let formatted = Formatter::new(source, d.clone()).to_string();
        rmp::encode::write_map_len(w, 6).unwrap();
        rmp::encode::write_str(w, "severity").unwrap();
        rmp::encode::write_str(w, severity).unwrap();
        rmp::encode::write_str(w, "code").unwrap();
        rmp::encode::write_i64(w, d.code as i64).unwrap();
        rmp::encode::write_str(w, "message").unwrap();
        rmp::encode::write_str(w, &d.message).unwrap();
        rmp::encode::write_str(w, "labels").unwrap();
        rmp::encode::write_array_len(w, d.labels.len() as u32).unwrap();
        for label in &d.labels {
            rmp::encode::write_map_len(w, 4).unwrap();
            rmp::encode::write_str(w, "message").unwrap();
            rmp::encode::write_str(w, &label.message).unwrap();
            rmp::encode::write_str(w, "primary").unwrap();
            rmp::encode::write_bool(w, label.primary).unwrap();
            rmp::encode::write_str(w, "start").unwrap();
            rmp::encode::write_i64(w, label.span.start() as i64).unwrap();
            rmp::encode::write_str(w, "end").unwrap();
            rmp::encode::write_i64(w, label.span.end() as i64).unwrap();
        }
        rmp::encode::write_str(w, "notes").unwrap();
        rmp::encode::write_array_len(w, d.notes.len() as u32).unwrap();
        for note in &d.notes {
            rmp::encode::write_str(w, &note.to_string()).unwrap();
        }
        rmp::encode::write_str(w, "formatted").unwrap();
        rmp::encode::write_str(w, formatted.trim()).unwrap();
    }
    out
}

/// Frees a program `st_vrl_compile` returned. Null is ignored.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn st_vrl_program_free(compiled: *mut Compiled) {
    if !compiled.is_null() {
        let _ = panic::catch_unwind(AssertUnwindSafe(|| drop(unsafe { Box::from_raw(compiled) })));
    }
}

/// Frees a buffer Rust returned.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn st_vrl_buf_free(buf: Buf) {
    if !buf.ptr.is_null() {
        drop(unsafe { Box::from_raw(std::ptr::slice_from_raw_parts_mut(buf.ptr, buf.len)) });
    }
}
