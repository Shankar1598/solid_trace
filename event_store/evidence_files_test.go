package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solidtrace/event_store/storage"
	"github.com/valyala/fasthttp"
)

// The first bytes of a file of each type, padded to a size.
var (
	pngBytes  = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"
	jpegBytes = "\xff\xd8\xff\xe0\x00\x10JFIF\x00"
	gifBytes  = "GIF89a\x01\x00\x01\x00"
	webpBytes = "RIFF\x24\x00\x00\x00WEBPVP8 "
	mp4Bytes  = "\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2"
	webmBytes = "\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01\x42\xf7\x81\x01\x42\xf2\x81\x04\x42\xf3\x81\x08\x42\x82\x84webm\x42\x87\x81\x04"
)

func evidenceOf(head string, size int) []byte {
	data := make([]byte, size)
	copy(data, head)
	for i := len(head); i < size; i++ {
		data[i] = byte(i * 7)
	}
	return data
}

func evidenceUploadPath(sessionID string) string {
	return "/api/agent/v1/agent-sessions/" + sessionID + "/evidence"
}

func uploadEvidence(t *testing.T, env *TestEnv, secret, sessionID, caption string, body []byte, headers map[string]string) agentAnswer {
	t.Helper()
	path := evidenceUploadPath(sessionID)
	if caption != "" {
		path += "?caption=" + url.QueryEscape(caption)
	}
	req, _ := http.NewRequest("POST", path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+secret)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := env.App.Test(req, 30000)
	if errors.Is(err, fasthttp.ErrBodyTooLarge) {
		return agentAnswer{status: http.StatusRequestEntityTooLarge}
	}
	if err != nil {
		t.Fatalf("Upload an Evidence file: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	answer := agentAnswer{status: resp.StatusCode, raw: string(raw)}
	_ = json.Unmarshal(raw, &answer.body)
	return answer
}

// readEvidence is a read of the query API: its status, headers and body.
func readEvidence(t *testing.T, env *TestEnv, path string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest("GET", path, nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := env.QueryApp.Test(req, 30000)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, body
}

// storedEvidence counts the files in the evidence folder, outside tmp.
func storedEvidence(t *testing.T, env *TestEnv) int {
	t.Helper()
	n := 0
	root := filepath.Join(env.dir, "evidence", "evidence")
	err := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	})
	if err != nil {
		t.Fatalf("Walk the evidence folder: %v", err)
	}
	return n
}

func TestEvidenceFileUpload(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	o.rows.exec(`INSERT INTO instance_settings (id, console_url, created_at, updated_at) VALUES (1, 'https://console.example.com', ?, ?)`,
		railsTime(o.rows.now), railsTime(o.rows.now))
	var slug string
	if err := o.rows.db.QueryRow("SELECT slug FROM organizations WHERE id = ?", o.organization).Scan(&slug); err != nil {
		t.Fatal(err)
	}

	t.Run("each type of image and video is stored with the type of its bytes, and answers its Console URL", func(t *testing.T) {
		for _, c := range []struct{ head, contentType string }{
			{pngBytes, "image/png"}, {jpegBytes, "image/jpeg"}, {gifBytes, "image/gif"},
			{webpBytes, "image/webp"}, {mp4Bytes, "video/mp4"}, {webmBytes, "video/webm"},
		} {
			answer := uploadEvidence(t, env, o.secret, laptopSession, "The cart page", evidenceOf(c.head, 1000), nil)
			expectStatus(t, answer, 201)
			id, _ := answer.body["id"].(string)
			want := fmt.Sprintf("https://console.example.com/%s/evidence/%s", slug, id)
			if answer.body["content_type"] != c.contentType || answer.body["byte_size"] != float64(1000) || answer.body["url"] != want {
				t.Errorf("Expected %s of 1000 bytes at %s, got %s", c.contentType, want, answer.raw)
			}
		}
		if n := storedEvidence(t, env); n != 6 {
			t.Errorf("Expected 6 stored files, got %d", n)
		}
	})

	t.Run("a file that is not one of the types is refused with 415, whatever its Content-Type says", func(t *testing.T) {
		for name, c := range map[string]struct {
			body    string
			headers map[string]string
		}{
			"SVG":                      {`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`, nil},
			"HTML":                     {"<!doctype html><script>alert(1)</script>", nil},
			"HTML sent as a PNG":       {"<html><body>shot.png</body></html>", map[string]string{"Content-Type": "image/png"}},
			"Matroska":                 {"\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01\x42\x82\x88matroska", nil},
			"HEIC":                     {"\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic", nil},
			"a PNG with gzip encoding": {pngBytes, map[string]string{"Content-Encoding": "gzip"}},
		} {
			if answer := uploadEvidence(t, env, o.secret, laptopSession, "", evidenceOf(c.body, 500), c.headers); answer.status != 415 {
				t.Errorf("%s: expected 415, got %d: %s", name, answer.status, answer.raw)
			}
		}
	})

	t.Run("a file of 32 MiB is stored, and one byte more is refused with 413", func(t *testing.T) {
		expectStatus(t, uploadEvidence(t, env, o.secret, laptopSession, "", evidenceOf(pngBytes, storage.MaxEvidenceFileBytes), nil), 201)
		expectStatus(t, uploadEvidence(t, env, o.secret, laptopSession, "", evidenceOf(pngBytes, storage.MaxEvidenceFileBytes+1), nil), 413)
		expectStatus(t, uploadEvidence(t, env, o.secret, laptopSession, "", nil, nil), 400)
	})

	t.Run("the same file again to the same Agent session answers 200 with the same row, and stores nothing", func(t *testing.T) {
		shot := evidenceOf(pngBytes, 2048)
		first := uploadEvidence(t, env, o.secret, otherSession, "Before", shot, nil)
		expectStatus(t, first, 201)
		stored := storedEvidence(t, env)
		again := uploadEvidence(t, env, o.secret, otherSession, "After", shot, nil)
		expectStatus(t, again, 200)
		if again.body["id"] != first.body["id"] || again.body["url"] != first.body["url"] {
			t.Errorf("Expected the first upload's row %s, got %s", first.raw, again.raw)
		}
		if n := storedEvidence(t, env); n != stored {
			t.Errorf("Expected no new stored file, got %d after %d", n, stored)
		}
		_, row := readEvidence(t, env, "/evidence/"+first.body["id"].(string), nil)
		if !strings.Contains(string(row), `"caption":"Before"`) {
			t.Errorf("Expected the first upload's caption, got %s", row)
		}
		// Another Agent session gets its own row.
		other := uploadEvidence(t, env, o.secret, runSession, "", shot, nil)
		expectStatus(t, other, 201)
		if other.body["id"] == first.body["id"] {
			t.Errorf("Expected a new row for another Agent session, got %s", other.raw)
		}
	})

	t.Run("a caption over 200 characters is refused", func(t *testing.T) {
		expectStatus(t, uploadEvidence(t, env, o.secret, laptopSession, strings.Repeat("é", 201), evidenceOf(pngBytes, 100), nil), 400)
		expectStatus(t, uploadEvidence(t, env, o.secret, laptopSession, strings.Repeat("é", 200), evidenceOf(pngBytes, 101), nil), 201)
	})

	t.Run("a token without sessions:write, or of an Organization with Agent sessions off, is refused", func(t *testing.T) {
		readOnly, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization, scopes: []string{"sessions:read"}})
		expectStatus(t, uploadEvidence(t, env, readOnly, laptopSession, "", evidenceOf(pngBytes, 100), nil), 403)
		expectStatus(t, uploadEvidence(t, env, o.noAgentSecret, laptopSession, "", evidenceOf(pngBytes, 100), nil), 403)
		expectStatus(t, uploadEvidence(t, env, "", laptopSession, "", evidenceOf(pngBytes, 100), nil), 401)
		expectStatus(t, uploadEvidence(t, env, o.secret, "not-a-session", "", evidenceOf(pngBytes, 100), nil), 400)
	})

	t.Run("an Agent session that another person uploaded, or that is not of the token's Run, is refused", func(t *testing.T) {
		expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: lines("Add a cart page")}), 200)
		answer := uploadEvidence(t, env, o.otherSecret, laptopSession, "", evidenceOf(pngBytes, 300), nil)
		expectStatus(t, answer, 403)
		if !strings.Contains(answer.raw, "another person") {
			t.Errorf("Expected UploadAgentFile's refusal, got %s", answer.raw)
		}
		runToken, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization, scopes: []string{"sessions:write"},
			sessionIDs: []string{runSession}})
		expectStatus(t, uploadEvidence(t, env, runToken, laptopSession, "", evidenceOf(pngBytes, 300), nil), 403)
		expectStatus(t, uploadEvidence(t, env, runToken, runSession, "", evidenceOf(pngBytes, 301), nil), 201)
	})

	t.Run("a token of another Organization does not reach the Agent session's files, with the same id", func(t *testing.T) {
		shot := evidenceOf(pngBytes, 4096)
		acme := uploadEvidence(t, env, o.secret, laptopSession, "Acme's", shot, nil)
		expectStatus(t, acme, 201)
		globex := o.rows.organization("Globex")
		o.rows.agentProject(globex, "github.com/globex/shop")
		o.rows.member(globex, o.otherUser)
		globexSecret, _ := o.rows.apiToken(apiTokenRow{user: o.otherUser, organization: globex, scopes: []string{"sessions:write"}})
		answer := uploadEvidence(t, env, globexSecret, laptopSession, "Globex's", shot, nil)
		expectStatus(t, answer, 201)
		if answer.body["id"] == acme.body["id"] {
			t.Fatalf("Expected Globex's own row, got Acme's: %s", answer.raw)
		}
		_, row := readEvidence(t, env, "/evidence/"+answer.body["id"].(string), nil)
		if !strings.Contains(string(row), fmt.Sprintf(`"organization_id":%d,`, globex)) {
			t.Errorf("Expected the row in Globex, got %s", row)
		}
		_, row = readEvidence(t, env, "/evidence/"+acme.body["id"].(string), nil)
		if !strings.Contains(string(row), fmt.Sprintf(`"organization_id":%d,`, o.organization)) || !strings.Contains(string(row), `"caption":"Acme's"`) {
			t.Errorf("Expected Acme's row as it was, got %s", row)
		}
	})
}

func TestEvidenceFileReads(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	video := evidenceOf(webmBytes, 1000)
	answer := uploadEvidence(t, env, o.secret, laptopSession, "The checkout", video, nil)
	expectStatus(t, answer, 201)
	id := answer.body["id"].(string)

	t.Run("the row", func(t *testing.T) {
		resp, body := readEvidence(t, env, "/evidence/"+id, nil)
		var row map[string]any
		if err := json.Unmarshal(body, &row); err != nil || resp.StatusCode != 200 {
			t.Fatalf("Expected 200 and JSON, got %d: %s", resp.StatusCode, body)
		}
		for name, want := range map[string]any{
			"id": id, "organization_id": float64(o.organization), "agent_session_id": laptopSession, "caption": "The checkout",
			"content_type": "video/webm", "byte_size": float64(1000), "uploaded_by_user_id": float64(o.user), "deleted_at": nil,
		} {
			if row[name] != want {
				t.Errorf("Expected %s %v, got %v", name, want, row[name])
			}
		}
	})

	t.Run("the bytes, whole and by range", func(t *testing.T) {
		for _, c := range []struct {
			rangeHeader  string
			status       int
			contentRange string
			want         []byte
		}{
			{"", 200, "", video},
			{"bytes=100-199", 206, "bytes 100-199/1000", video[100:200]},
			{"bytes=990-", 206, "bytes 990-999/1000", video[990:]},
			{"bytes=-10", 206, "bytes 990-999/1000", video[990:]},
			{"bytes=900-5000", 206, "bytes 900-999/1000", video[900:]},
			{"bytes=0-0", 206, "bytes 0-0/1000", video[:1]},
			{"bytes=0-1,5-9", 200, "", video},
			{"items=0-9", 200, "", video},
			{"bytes=1000-", 416, "bytes */1000", nil},
		} {
			headers := map[string]string{}
			if c.rangeHeader != "" {
				headers["Range"] = c.rangeHeader
			}
			resp, body := readEvidence(t, env, "/evidence/"+id+"/content", headers)
			if resp.StatusCode != c.status || resp.Header.Get("Content-Range") != c.contentRange {
				t.Errorf("%q: expected %d with Content-Range %q, got %d with %q", c.rangeHeader, c.status, c.contentRange,
					resp.StatusCode, resp.Header.Get("Content-Range"))
				continue
			}
			if c.want != nil && (!bytes.Equal(body, c.want) || resp.Header.Get("Content-Type") != "video/webm" ||
				resp.Header.Get("Accept-Ranges") != "bytes") {
				t.Errorf("%q: expected %d bytes of video/webm, got %d bytes of %q", c.rangeHeader, len(c.want), len(body),
					resp.Header.Get("Content-Type"))
			}
		}
	})

	t.Run("an id with no row is 404, and a deleted file's bytes are 410", func(t *testing.T) {
		for _, path := range []string{"/evidence/5f0c1d2e-3a4b-4c5d-8e6f-7a8b9c0d1e2f", "/evidence/not-an-id/content"} {
			if resp, body := readEvidence(t, env, path, nil); resp.StatusCode != 404 {
				t.Errorf("%s: expected 404, got %d: %s", path, resp.StatusCode, body)
			}
		}
		o.rows.exec("UPDATE evidence_files SET deleted_at = ? WHERE id = ?", railsTime(o.rows.now), id)
		if resp, body := readEvidence(t, env, "/evidence/"+id+"/content", nil); resp.StatusCode != 410 {
			t.Errorf("Expected 410, got %d: %s", resp.StatusCode, body)
		}
		if _, body := readEvidence(t, env, "/evidence/"+id, nil); strings.Contains(string(body), `"deleted_at":null`) {
			t.Errorf("Expected the row's deleted_at, got %s", body)
		}
	})
}
