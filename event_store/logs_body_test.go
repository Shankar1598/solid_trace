package main_test

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"slices"
	"testing"

	"github.com/klauspost/compress/zstd"
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
