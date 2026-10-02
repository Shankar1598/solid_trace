package main_test

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/valyala/fasthttp"
)

// gzipped compresses body as a shipper's gzip encoder does.
func gzipped(t *testing.T, body []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	if _, err := w.Write(body); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	return out.Bytes()
}

// storedGzip wraps body in gzip without compressing it, so the gzip body is
// a little larger than body.
func storedGzip(t *testing.T, body []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w, _ := gzip.NewWriterLevel(&out, gzip.NoCompression)
	if _, err := w.Write(body); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	return out.Bytes()
}

// zstdCompressed compresses body as a shipper's zstd encoder does.
func zstdCompressed(t *testing.T, body []byte) []byte {
	t.Helper()
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd: %v", err)
	}
	defer encoder.Close()
	return encoder.EncodeAll(body, nil)
}

// sendEncoded posts a body with a Content-Encoding to an ingest path of
// Project 123, with its key.
func sendEncoded(t *testing.T, env *TestEnv, path, contentType, encoding string, body []byte) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", path+"?sentry_key=test_public_key", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	resp, err := env.App.Test(req, 10000)
	if errors.Is(err, fasthttp.ErrBodyTooLarge) {
		// The server refused a body over the ingest app's limit before its
		// handler ran. It answered 413 and closed the connection, which
		// app.Test reports as this error instead of the response.
		return &http.Response{StatusCode: http.StatusRequestEntityTooLarge, Body: http.NoBody}
	}
	if err != nil {
		t.Fatalf("POST %s failed: %v", path, err)
	}
	return resp
}

func TestGzipZstdAndIdentityLogBodiesAreStored(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	bodies := []struct {
		encoding    string
		contentType string
		body        []byte
	}{
		{"gzip", "application/x-ndjson", gzipped(t, []byte("{\"message\":\"gzip one\",\"time\":\"2026-10-02T11:00:01Z\"}\n{\"message\":\"gzip two\",\"time\":\"2026-10-02T11:00:02Z\"}\n"))},
		{"zstd", "application/json", zstdCompressed(t, []byte(`[{"message":"zstd","time":"2026-10-02T11:00:03Z"}]`))},
		{"identity", "application/json", []byte(`{"message":"identity","time":"2026-10-02T11:00:04Z"}`)},
	}
	for _, b := range bodies {
		resp := sendEncoded(t, env, "/api/123/logs", b.contentType, b.encoding, b.body)
		if counts := decodeLogCounts(t, resp); counts.Accepted == 0 || counts.Dropped != 0 {
			t.Errorf("%s: expected every Log accepted, got %+v", b.encoding, counts)
		}
	}
	env.ProcessLogs(t)

	if got, want := messages(latestLogs(t, env, 123, 10)), []string{"identity", "zstd", "gzip two", "gzip one"}; !slices.Equal(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
}

// ndjsonOfSize is NDJSON Logs with the given message, at least n bytes long.
func ndjsonOfSize(message string, n int) []byte {
	line := []byte(fmt.Sprintf(`{"message":%q}`+"\n", message))
	return bytes.Repeat(line, n/len(line)+1)
}

func TestLogBodiesOverEitherCapGet413AndNothingIsStored(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	const sentCap, decodedCap = 10 << 20, 50 << 20
	overDecodedCap := ndjsonOfSize("over the decoded cap", decodedCap+1)
	bodies := []struct {
		name     string
		encoding string
		body     []byte
	}{
		{"identity over 10 MB", "", ndjsonOfSize("identity over 10 MB", sentCap+1)},
		{"gzip over 10 MB as sent", "gzip", storedGzip(t, ndjsonOfSize("gzip over 10 MB", sentCap))},
		{"gzip over 50 MB decoded", "gzip", gzipped(t, overDecodedCap)},
		{"zstd over 50 MB decoded", "zstd", zstdCompressed(t, overDecodedCap)},
	}
	for _, b := range bodies {
		t.Run(b.name, func(t *testing.T) {
			resp := sendEncoded(t, env, "/api/123/logs", "application/x-ndjson", b.encoding, b.body)
			if resp.StatusCode != 413 {
				raw, _ := io.ReadAll(resp.Body)
				t.Errorf("Expected 413, got %d: %.200s", resp.StatusCode, raw)
			}
		})
	}

	env.ProcessLogs(t)
	if logs := latestLogs(t, env, 123, 10); len(logs) != 0 {
		t.Errorf("Expected no Logs stored, got %d", len(logs))
	}
}

func TestOnlyTheLogRouteTakesABodyOver4MB(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// 6 MB as sent: over the Event routes' cap of 4 MB, under the Log route's
	// 10 MB.
	payload := []byte(`{"message":"` + strings.Repeat("x", 6<<20) + `"}`)

	resp := sendEncoded(t, env, "/api/123/logs", "application/json", "", payload)
	if counts := decodeLogCounts(t, resp); counts != (logCounts{Accepted: 1, Truncated: 1}) {
		t.Errorf("Expected the Log accepted and truncated, got %+v", counts)
	}

	envelope := append([]byte(`{}`+"\n"+`{"type":"event"}`+"\n"), payload...)
	for path, body := range map[string][]byte{"/api/123/store": payload, "/api/123/envelope": envelope} {
		resp := sendEncoded(t, env, path, "application/json", "", body)
		if resp.StatusCode != 413 {
			raw, _ := io.ReadAll(resp.Body)
			t.Errorf("%s: expected 413, got %d: %.200s", path, resp.StatusCode, raw)
		}
	}
	if uuids := env.EventWriter.UUIDs(); len(uuids) != 0 {
		t.Errorf("Expected no Event stored, got %d", len(uuids))
	}
}

// brotliCompressed compresses body as sentry-python does when the brotli
// module is installed.
func brotliCompressed(t *testing.T, body []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w := brotli.NewWriterLevel(&out, 4)
	if _, err := w.Write(body); err != nil {
		t.Fatalf("brotli: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("brotli: %v", err)
	}
	return out.Bytes()
}

// deflated compresses body in HTTP's deflate coding, the zlib format.
func deflated(t *testing.T, body []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zlib.NewWriter(&out)
	if _, err := w.Write(body); err != nil {
		t.Fatalf("deflate: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("deflate: %v", err)
	}
	return out.Bytes()
}

// logEnvelope is an envelope holding one log item with one record.
func logEnvelope(body string) []byte {
	record := fmt.Sprintf(`{"timestamp":1790942340.25,"level":"info","body":%q,"attributes":{}}`, body)
	return []byte(`{"sent_at":"2026-10-02T12:00:00Z"}` + sentryItem(logItemHeader(1), `{"items":[`+record+`]}`))
}

func TestEnvelopesInEachEncodingSentrySDKsUseAreStored(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	encode := map[string]func(*testing.T, []byte) []byte{
		"gzip":    gzipped,          // sentry-ruby, sentry-javascript on Node, and most others
		"br":      brotliCompressed, // sentry-python with brotli installed
		"deflate": deflated,
		"zstd":    zstdCompressed,
	}
	for encoding, compress := range encode {
		resp := sendEncoded(t, env, "/api/123/envelope", "application/x-sentry-envelope", encoding, compress(t, logEnvelope(encoding)))
		if resp.StatusCode != 200 {
			raw, _ := io.ReadAll(resp.Body)
			t.Errorf("%s: expected 200, got %d: %s", encoding, resp.StatusCode, raw)
		}
	}
	// sentry-ruby sends an empty Content-Encoding when it doesn't compress.
	req, _ := http.NewRequest("POST", "/api/123/envelope?sentry_key=test_public_key", bytes.NewReader(logEnvelope("uncompressed")))
	req.Header["Content-Encoding"] = []string{""}
	if resp, err := env.App.Test(req, 2000); err != nil || resp.StatusCode != 200 {
		t.Errorf("uncompressed: expected 200, got %v %v", resp, err)
	}
	env.ProcessLogs(t)

	got := messages(latestLogs(t, env, 123, 10))
	slices.Sort(got)
	if want := []string{"br", "deflate", "gzip", "uncompressed", "zstd"}; !slices.Equal(got, want) {
		t.Errorf("Expected Logs %v, got %v", want, got)
	}

	// An Event sent to the store route compressed, as before.
	if resp := sendEncoded(t, env, "/api/123/store", "application/json", "gzip", gzipped(t, []byte(divideByZero))); resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("store: expected 200, got %d: %s", resp.StatusCode, raw)
	}
	if uuids := env.EventWriter.UUIDs(); len(uuids) != 1 {
		t.Errorf("Expected the Event stored, got %d", len(uuids))
	}
}

func TestEventRouteBodiesOver50MBDecodedGet413AndNothingIsStored(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Log records past 50 MB in one log item.
	record := `{"timestamp":1790942340.25,"level":"info","body":"over the decoded cap","attributes":{}}`
	records := strings.Repeat(record+",", (50<<20)/len(record)+1)
	envelope := []byte(`{}` + sentryItem(logItemHeader(1), `{"items":[`+records+record+`]}`))
	event := []byte(`{"message":"` + strings.Repeat("x", 50<<20) + `"}`)

	for path, body := range map[string][]byte{"/api/123/envelope": envelope, "/api/123/store": event} {
		resp := sendEncoded(t, env, path, "application/json", "gzip", gzipped(t, body))
		if resp.StatusCode != 413 {
			raw, _ := io.ReadAll(resp.Body)
			t.Errorf("%s: expected 413, got %d: %.200s", path, resp.StatusCode, raw)
		}
	}

	env.ProcessLogs(t)
	if logs := latestLogs(t, env, 123, 10); len(logs) != 0 {
		t.Errorf("Expected no Logs stored, got %d", len(logs))
	}
	if uuids := env.EventWriter.UUIDs(); len(uuids) != 0 {
		t.Errorf("Expected no Event stored, got %d", len(uuids))
	}
}

func TestBodiesInAnUnknownOrBrokenEncodingAreRefusedAndNothingIsStored(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	logs := []byte(`{"message":"refused"}`)
	cases := []struct {
		name     string
		encoding string
		body     []byte
		want     int
	}{
		{"an encoding EventStore doesn't decode", "snappy", logs, 415},
		{"two encodings", "gzip, zstd", zstdCompressed(t, gzipped(t, logs)), 415},
		{"gzip that isn't", "gzip", logs, 400},
		{"zstd cut short", "zstd", zstdCompressed(t, logs)[:10], 400},
	}
	for _, c := range cases {
		for _, path := range []string{"/api/123/logs", "/api/123/envelope", "/api/123/store"} {
			t.Run(c.name+" "+path, func(t *testing.T) {
				resp := sendEncoded(t, env, path, "application/json", c.encoding, c.body)
				if resp.StatusCode != c.want {
					raw, _ := io.ReadAll(resp.Body)
					t.Errorf("Expected %d, got %d: %s", c.want, resp.StatusCode, raw)
				}
			})
		}
	}

	env.ProcessLogs(t)
	if logs := latestLogs(t, env, 123, 10); len(logs) != 0 {
		t.Errorf("Expected no Logs stored, got %d", len(logs))
	}
	if uuids := env.EventWriter.UUIDs(); len(uuids) != 0 {
		t.Errorf("Expected no Event stored, got %d", len(uuids))
	}
}
