//! The VRL runtime behind EventStore's `vrl` Go package. Go calls the
//! `st_vrl_*` functions over cgo; see `../vrl.h` for the contract.
//!
//! Every export catches panics: one must never unwind into Go.

use std::any::Any;
use std::panic::{self, AssertUnwindSafe};
use std::sync::Once;

use chrono::{DateTime, Utc};
use rmpv::ValueRef;
use vrl::compiler::runtime::{Runtime, Terminate};
use vrl::compiler::{self, ExpressionError, Function, Program, TargetValue, TimeZone};
use vrl::diagnostic::{Diagnostic, Formatter, Severity};
use vrl::prelude::{Bytes, NotNan};
use vrl::value::{KeyString, ObjectMap, Secrets, Value};

/// Functions left out by name: they have no cap on what they write, so a small
/// Log could make them exhaust memory.
const LEFT_OUT: &[&str] = &["decode_gzip", "decode_zlib", "decode_zstd", "decode_snappy"];

/// How deep a Log may nest. Deeper Logs fail rather than risk overflowing
/// the stack, which no panic handler can catch.
const MAX_DEPTH: usize = 64;

/// The msgpack timestamp extension type, as Go writes time.Time.
const TIMESTAMP: i8 = -1;

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

/// Runs a program on Logs. `logs` is a msgpack array of bin, each one Log's
/// msgpack map. Writes one outcome per Log to `outcomes`, as a msgpack
/// array: `[0, log]` kept, `[1]` dropped by abort, `[2, error]` failed.
/// Returns 0, or 1 with the error text in `outcomes` when the whole call
/// fails.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn st_vrl_run(
    compiled: *const Compiled,
    logs: *const u8,
    len: usize,
    outcomes: *mut Buf,
) -> i32 {
    quiet_panics();
    let compiled = unsafe { &*compiled };
    let logs = unsafe { bytes(logs, len) };
    let (status, out) = match panic::catch_unwind(AssertUnwindSafe(|| run(compiled, logs))) {
        Ok(Ok(out)) => (0, out),
        Ok(Err(error)) => (1, error.into_bytes()),
        Err(payload) => (1, panic_text(payload).into_bytes()),
    };
    unsafe { *outcomes = Buf::from_vec(out) };
    status
}

fn run(compiled: &Compiled, mut logs: &[u8]) -> Result<Vec<u8>, String> {
    let count = rmp::decode::read_array_len(&mut logs).map_err(|e| format!("reading the Logs: {e}"))?;
    let mut out = Vec::with_capacity(logs.len() + 16);
    rmp::encode::write_array_len(&mut out, count).unwrap();
    let mut runtime = Runtime::default();
    let mut scratch = Vec::new();
    for _ in 0..count {
        let len = rmp::decode::read_bin_len(&mut logs).map_err(|e| format!("reading the Logs: {e}"))? as usize;
        if len > logs.len() {
            return Err("reading the Logs: a Log is cut short".to_string());
        }
        let (log, rest) = logs.split_at(len);
        logs = rest;
        scratch.clear();
        // Each Log's outcome goes to scratch first, so a panic part way
        // through never leaves half an outcome in out.
        match panic::catch_unwind(AssertUnwindSafe(|| run_one(compiled, &mut runtime, log, &mut scratch))) {
            Ok(()) => out.extend_from_slice(&scratch),
            Err(payload) => {
                runtime = Runtime::default(); // it may hold half-set variables
                write_failed(&mut out, &panic_text(payload));
            }
        }
    }
    Ok(out)
}

fn run_one(compiled: &Compiled, runtime: &mut Runtime, log: &[u8], out: &mut Vec<u8>) {
    let value = match decode_log(log) {
        Ok(value) => value,
        Err(error) => return write_failed(out, &error),
    };
    let mut target = TargetValue {
        value,
        metadata: Value::Object(ObjectMap::new()),
        secrets: Secrets::default(),
    };
    let result = runtime.resolve(&mut target, &compiled.program, &compiled.timezone);
    runtime.clear();
    match result {
        Ok(_) => match &target.value {
            Value::Object(_) => {
                rmp::encode::write_array_len(out, 2).unwrap();
                rmp::encode::write_pfix(out, 0).unwrap();
                if let Err(error) = encode_value(out, &target.value, 0) {
                    out.clear();
                    write_failed(out, &error);
                }
            }
            other => write_failed(out, &format!(". is {}, not an object", with_article(other.kind_str()))),
        },
        Err(Terminate::Abort(ExpressionError::Abort { .. })) => {
            rmp::encode::write_array_len(out, 1).unwrap();
            rmp::encode::write_pfix(out, 1).unwrap();
        }
        Err(error) => write_failed(out, &error.to_string()),
    }
}

fn write_failed(out: &mut Vec<u8>, error: &str) {
    rmp::encode::write_array_len(out, 2).unwrap();
    rmp::encode::write_pfix(out, 2).unwrap();
    rmp::encode::write_str(out, error).unwrap();
}

fn with_article(kind: &str) -> String {
    match kind.as_bytes().first() {
        Some(b'a' | b'e' | b'i' | b'o' | b'u') => format!("an {kind}"),
        _ => format!("a {kind}"),
    }
}

fn decode_log(mut log: &[u8]) -> Result<Value, String> {
    let value = rmpv::decode::read_value_ref_with_max_depth(&mut log, MAX_DEPTH).map_err(|e| match e {
        rmpv::decode::Error::DepthLimitExceeded => format!("the Log nests more than {MAX_DEPTH} levels deep"),
        e => format!("reading the Log: {e}"),
    })?;
    to_value(value)
}

fn to_value(value: ValueRef) -> Result<Value, String> {
    Ok(match value {
        ValueRef::Nil => Value::Null,
        ValueRef::Boolean(b) => Value::Boolean(b),
        ValueRef::Integer(n) => match n.as_i64() {
            Some(n) => Value::Integer(n),
            None => return Err(format!("the Log holds {n}, too large for an integer")),
        },
        ValueRef::F32(f) => to_float(f as f64)?,
        ValueRef::F64(f) => to_float(f)?,
        ValueRef::String(s) => match s.as_str() {
            Some(s) => Value::from(s),
            None => Value::Bytes(Bytes::copy_from_slice(s.as_bytes())),
        },
        ValueRef::Binary(b) => Value::Bytes(Bytes::copy_from_slice(b)),
        ValueRef::Array(items) => Value::Array(items.into_iter().map(to_value).collect::<Result<_, _>>()?),
        ValueRef::Map(entries) => {
            let mut map = ObjectMap::new();
            for (key, value) in entries {
                let key = match &key {
                    ValueRef::String(s) => s.as_str(),
                    _ => None,
                };
                let key = key.ok_or("the Log has a key that isn't a string")?;
                map.insert(KeyString::from(key), to_value(value)?);
            }
            Value::Object(map)
        }
        ValueRef::Ext(TIMESTAMP, b) => Value::Timestamp(timestamp(b).ok_or("the Log holds a timestamp that can't be read")?),
        ValueRef::Ext(kind, _) => return Err(format!("the Log holds a msgpack extension of type {kind}")),
    })
}

fn to_float(f: f64) -> Result<Value, String> {
    NotNan::new(f).map(Value::Float).map_err(|_| "the Log holds NaN".to_string())
}

/// Reads msgpack's timestamp extension in its 4, 8 and 12 byte forms.
fn timestamp(b: &[u8]) -> Option<DateTime<Utc>> {
    let (secs, nanos) = match b.len() {
        4 => (u32::from_be_bytes(b.try_into().ok()?) as i64, 0),
        8 => {
            let x = u64::from_be_bytes(b.try_into().ok()?);
            ((x & 0x3_ffff_ffff) as i64, (x >> 34) as u32)
        }
        12 => (i64::from_be_bytes(b[4..].try_into().ok()?), u32::from_be_bytes(b[..4].try_into().ok()?)),
        _ => return None,
    };
    DateTime::from_timestamp(secs, nanos)
}

fn encode_value(out: &mut Vec<u8>, value: &Value, depth: usize) -> Result<(), String> {
    if depth > MAX_DEPTH {
        return Err(format!("the program nested . more than {MAX_DEPTH} levels deep"));
    }
    match value {
        Value::Null => rmp::encode::write_nil(out).unwrap(),
        Value::Boolean(b) => rmp::encode::write_bool(out, *b).unwrap(),
        // Always int64 and float64, which Go decodes as such.
        Value::Integer(n) => rmp::encode::write_i64(out, *n).unwrap(),
        Value::Float(f) => rmp::encode::write_f64(out, f.into_inner()).unwrap(),
        Value::String(s) => rmp::encode::write_str(out, s).unwrap(),
        Value::Bytes(b) => rmp::encode::write_str(out, &String::from_utf8_lossy(b)).unwrap(),
        Value::Regex(r) => rmp::encode::write_str(out, r.as_str()).unwrap(),
        Value::Timestamp(t) => {
            rmp::encode::write_ext_meta(out, 12, TIMESTAMP).unwrap();
            out.extend_from_slice(&t.timestamp_subsec_nanos().to_be_bytes());
            out.extend_from_slice(&t.timestamp().to_be_bytes());
        }
        Value::Object(map) => {
            rmp::encode::write_map_len(out, map.len() as u32).unwrap();
            for (key, value) in map {
                rmp::encode::write_str(out, key).unwrap();
                encode_value(out, value, depth + 1)?;
            }
        }
        Value::Array(items) => {
            rmp::encode::write_array_len(out, items.len() as u32).unwrap();
            for item in items {
                encode_value(out, item, depth + 1)?;
            }
        }
    }
    Ok(())
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
