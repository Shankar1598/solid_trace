package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/event_store/storage"
	"github.com/valyala/fasthttp"
)

// The upload of an Agent session's files by offset, and the offsets call:
// what the server has of each path. The solidtrace CLI and the wrapper ask
// what the server has, then send only the new lines.

const (
	laptopSession = "0b6c2f1e-6a51-4c8e-9a3d-0f6d1c2b3a41"
	runSession    = "7d1e5a90-2c4b-4f7a-8e61-5b3c9d0a2e17"
	otherSession  = "c3f0a9e2-1d47-4b6e-a5c8-92e7f1b04d6a"
)

func agentFilesPath(sessionID string) string {
	return "/api/agent/v1/agent-sessions/" + sessionID + "/files"
}

// unitUpload is one upload: the bytes of path from offset, of version 1
// unless set, in the repository github.com/acme/shop.
type unitUpload struct {
	path, folder, runID, project, harness, repository string
	version, offset                                   int
	body                                              []byte
	headers                                           map[string]string
}

func uploadUnit(t *testing.T, env *TestEnv, secret, sessionID string, u unitUpload) agentAnswer {
	t.Helper()
	query := url.Values{}
	query.Set("path", u.path)
	version := u.version
	if version == 0 {
		version = 1
	}
	query.Set("version", fmt.Sprint(version))
	query.Set("offset", fmt.Sprint(u.offset))
	query.Set("harness", cmpOr(u.harness, "claude-code"))
	query.Set("repository", cmpOr(u.repository, "github.com/acme/shop"))
	for name, value := range map[string]string{"folder": u.folder, "run_id": u.runID, "project": u.project} {
		if value != "" {
			query.Set(name, value)
		}
	}
	req, _ := http.NewRequest("POST", agentFilesPath(sessionID)+"?"+query.Encode(), bytes.NewReader(u.body))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+secret)
	for name, value := range u.headers {
		req.Header.Set(name, value)
	}
	resp, err := env.App.Test(req, 30000)
	if errors.Is(err, fasthttp.ErrBodyTooLarge) {
		// Refused before the handler ran, as sendEncoded explains.
		return agentAnswer{status: http.StatusRequestEntityTooLarge}
	}
	if err != nil {
		t.Fatalf("Upload of %s at %d: %v", u.path, u.offset, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	answer := agentAnswer{status: resp.StatusCode, raw: string(raw)}
	_ = json.Unmarshal(raw, &answer.body)
	return answer
}

func cmpOr(value, otherwise string) string {
	if value == "" {
		return otherwise
	}
	return value
}

// sessionFiles is the offsets call's answer.
type sessionFiles struct {
	AgentSession *struct {
		ID     string `json:"id"`
		Person struct {
			UserID int64 `json:"user_id"`
		} `json:"person"`
		Harness    string `json:"harness"`
		Repository string `json:"repository"`
		Folder     string `json:"folder"`
		RunID      string `json:"run_id"`
		Project    string `json:"project"`
	} `json:"agent_session"`
	Files []struct {
		Path      string `json:"path"`
		Version   int    `json:"version"`
		EndOffset int    `json:"end_offset"`
	} `json:"files"`
}

func offsetsOf(t *testing.T, env *TestEnv, secret, sessionID string) (agentAnswer, sessionFiles) {
	t.Helper()
	answer := callAgentAPI(t, env.App, agentFilesPath(sessionID), map[string]string{"Authorization": "Bearer " + secret})
	var files sessionFiles
	if answer.status == 200 {
		if err := json.Unmarshal([]byte(answer.raw), &files); err != nil {
			t.Fatalf("Offsets answer %s: %v", answer.raw, err)
		}
	}
	return answer, files
}

// endOffsets is each path's "version end_offset".
func endOffsets(files sessionFiles) map[string]string {
	ends := map[string]string{}
	for _, f := range files.Files {
		ends[f.Path] = fmt.Sprintf("%d %d", f.Version, f.EndOffset)
	}
	return ends
}

// storedBytes is what agent_session_files holds of a path's version, in
// offset order, once Agent file processing has caught up.
func storedBytes(t *testing.T, env *TestEnv, projectID int64, sessionID, path string) string {
	t.Helper()
	env.ProcessAgentFiles(t)
	var stored string
	if err := env.ClickHouse.Conn().QueryRow(context.Background(), `
		SELECT arrayStringConcat(arrayMap(x -> x.2, arraySort(groupArray((offset, bytes)))))
		FROM agent_session_files WHERE project_id = ? AND session_id = ? AND path = ? AND version = 1`,
		uint32(projectID), sessionID, path).Scan(&stored); err != nil {
		t.Fatalf("Read the stored bytes of %s: %v", path, err)
	}
	return stored
}

func lines(texts ...string) []byte {
	var b strings.Builder
	for _, text := range texts {
		fmt.Fprintf(&b, `{"type":"user","message":%q}`+"\n", text)
	}
	return []byte(b.String())
}

// agentOrganization is an Organization with Agent sessions on, a member and
// the member's sessions:write token.
type agentOrganization struct {
	rows          *consoleRows
	organization  int64
	project       int64
	user          int64
	secret        string
	otherUser     int64
	otherSecret   string
	noAgentSecret string
}

func newAgentOrganization(t *testing.T, env *TestEnv) agentOrganization {
	t.Helper()
	rows := newConsoleRows(t, env)
	o := agentOrganization{rows: rows, organization: rows.organization("Acme")}
	o.project = rows.agentProject(o.organization, "github.com/acme/shop")
	o.user, o.otherUser = rows.user(), rows.user()
	rows.member(o.organization, o.user)
	rows.member(o.organization, o.otherUser)
	write := []string{"sessions:write"}
	o.secret, _ = rows.apiToken(apiTokenRow{user: o.user, organization: o.organization, scopes: write})
	o.otherSecret, _ = rows.apiToken(apiTokenRow{user: o.otherUser, organization: o.organization, scopes: write})
	off := rows.organization("No agents")
	rows.member(off, o.user)
	o.noAgentSecret, _ = rows.apiToken(apiTokenRow{user: o.user, organization: off, scopes: write})
	return o
}

func TestAgentSessionUploadByOffset(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	first := lines("Add a cart page", "Use the shop's colours")
	second := lines("Now the checkout")
	subagent := lines("Find the cart code")

	t.Run("lines at offset 0 then at the next offset are stored, and the offsets call returns each end offset", func(t *testing.T) {
		if _, files := offsetsOf(t, env, o.secret, laptopSession); files.AgentSession != nil || len(files.Files) != 0 {
			t.Fatalf("Expected nothing of an Agent session with no upload, got %+v", files)
		}
		answer := uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", folder: "web/cart", body: first})
		expectStatus(t, answer, 200)
		if answer.body["end_offset"] != float64(len(first)) || answer.body["stored"] != true {
			t.Errorf("Expected the end offset %d and stored, got %s", len(first), answer.raw)
		}
		expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", folder: "web/cart", offset: len(first), body: second}), 200)
		expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "subagents/agent-a1.jsonl", folder: "web/cart", body: subagent}), 200)

		answer, files := offsetsOf(t, env, o.secret, laptopSession)
		expectStatus(t, answer, 200)
		want := map[string]string{
			"session.jsonl":            fmt.Sprintf("1 %d", len(first)+len(second)),
			"subagents/agent-a1.jsonl": fmt.Sprintf("1 %d", len(subagent)),
		}
		if got := endOffsets(files); !mapsEqual(got, want) {
			t.Errorf("Expected end offsets %v, got %v", want, got)
		}
		if got := storedBytes(t, env, o.project, laptopSession, "session.jsonl"); got != string(first)+string(second) {
			t.Errorf("Expected the two units stored in order, got %q", got)
		}
	})

	t.Run("the Agent session has the token's user as its person, and the upload's repository and folder", func(t *testing.T) {
		_, files := offsetsOf(t, env, o.secret, laptopSession)
		s := files.AgentSession
		if s == nil || s.ID != laptopSession || s.Person.UserID != o.user || s.Harness != "claude-code" ||
			s.Repository != "github.com/acme/shop" || s.Folder != "web/cart" || s.RunID != "" || s.Project != "" {
			t.Errorf("Unexpected Agent session %+v", s)
		}
	})

	t.Run("a repeat of the same offset stores nothing new", func(t *testing.T) {
		for _, u := range []unitUpload{
			{path: "session.jsonl", body: first},
			{path: "session.jsonl", offset: len(first), body: second},
		} {
			answer := uploadUnit(t, env, o.secret, laptopSession, u)
			expectStatus(t, answer, 200)
			if answer.body["stored"] != false || answer.body["end_offset"] != float64(len(first)+len(second)) {
				t.Errorf("Expected a repeat at %d to store nothing, got %s", u.offset, answer.raw)
			}
		}
		if got := storedBytes(t, env, o.project, laptopSession, "session.jsonl"); got != string(first)+string(second) {
			t.Errorf("Expected each byte stored once after repeats, got %q", got)
		}
	})

	t.Run("a gap or an overlap against the stored end offset is refused", func(t *testing.T) {
		end := len(first) + len(second)
		for _, c := range []struct {
			says string
			u    unitUpload
		}{
			{"A gap", unitUpload{path: "session.jsonl", offset: end + 5, body: second}},
			{"An overlap", unitUpload{path: "session.jsonl", offset: len(first), body: append(bytes.Clone(second), second...)}},
			{"The server has no version 1", unitUpload{path: "tool-results/new.jsonl", offset: 10, body: second}},
		} {
			answer := uploadUnit(t, env, o.secret, laptopSession, c.u)
			expectStatus(t, answer, 409)
			if msg, _ := answer.body["error"].(string); !strings.HasPrefix(msg, c.says) {
				t.Errorf("Expected %s at %d to say %q, got %s", c.u.path, c.u.offset, c.says, answer.raw)
			}
		}
		answer := uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: end + 5, body: second})
		if answer.body["end_offset"] != float64(end) {
			t.Errorf("Expected the refusal to give the end offset %d, got %s", end, answer.raw)
		}
		if got := storedBytes(t, env, o.project, laptopSession, "session.jsonl"); got != string(first)+string(second) {
			t.Errorf("Expected a refused upload to store nothing, got %q", got)
		}
	})

	t.Run("a unit of a .jsonl file is whole lines", func(t *testing.T) {
		answer := uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "other.jsonl", body: []byte(`{"type":"user"`)})
		expectStatus(t, answer, 400)
		for _, u := range []unitUpload{
			{path: "../escape.jsonl", body: first},
			{path: "/root.jsonl", body: first},
			{path: "session.jsonl", repository: "acme/shop", body: first},
			{path: "session.jsonl", runID: "run-1", project: "shop", body: first},
		} {
			expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, u), 400)
		}
	})

	t.Run("a 200 means stored: after a restart the offsets call still returns them", func(t *testing.T) {
		third := lines("Ship it")
		end := len(first) + len(second)
		expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: end, body: third}), 200)
		env.Reopen(t)
		_, files := offsetsOf(t, env, o.secret, laptopSession)
		if got := endOffsets(files)["session.jsonl"]; got != fmt.Sprintf("1 %d", end+len(third)) {
			t.Errorf("Expected the end offset %d after a restart, got %s", end+len(third), got)
		}
		if got := storedBytes(t, env, o.project, laptopSession, "session.jsonl"); got != string(first)+string(second)+string(third) {
			t.Errorf("Expected the unit stored before the restart in ClickHouse after it, got %q", got)
		}
	})

	t.Run("another person's token cannot read or add to the Agent session", func(t *testing.T) {
		answer, _ := offsetsOf(t, env, o.otherSecret, laptopSession)
		expectStatus(t, answer, 403)
		expectStatus(t, uploadUnit(t, env, o.otherSecret, laptopSession, unitUpload{path: "x.jsonl", body: first}), 403)
	})

	t.Run("an Organization with Agent sessions off stores nothing", func(t *testing.T) {
		answer := uploadUnit(t, env, o.noAgentSecret, otherSession, unitUpload{path: "session.jsonl", body: first})
		expectStatus(t, answer, 403)
		if msg, _ := answer.body["error"].(string); !strings.Contains(msg, "Agent sessions are off") {
			t.Errorf("Expected a clear body, got %s", answer.raw)
		}
	})
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestAgentSessionUploadOfARun(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	runSecret, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization,
		scopes: []string{"sessions:write", "sessions:read"}, sessionIDs: []string{runSession}})
	body := lines("Fix issue 42")
	run := unitUpload{path: "session.jsonl", runID: "run-0193", project: "shop", folder: "services/api", body: body}

	t.Run("an upload of a Run's token carries the Run id, the Project and the folder", func(t *testing.T) {
		expectStatus(t, uploadUnit(t, env, runSecret, runSession, run), 200)
		_, files := offsetsOf(t, env, runSecret, runSession)
		s := files.AgentSession
		if s == nil || s.RunID != "run-0193" || s.Project != "shop" || s.Folder != "services/api" || s.Person.UserID != o.user {
			t.Errorf("Unexpected Agent session of the Run %+v", s)
		}
		if got := storedBytes(t, env, o.project, runSession, "session.jsonl"); got != string(body) {
			t.Errorf("Expected the Run's unit stored, got %q", got)
		}
	})

	t.Run("a Run's token must send the Run and its Project", func(t *testing.T) {
		answer := uploadUnit(t, env, runSecret, runSession, unitUpload{path: "session.jsonl", offset: len(body), body: body})
		expectStatus(t, answer, 400)
	})

	t.Run("an Agent session id not in the limit is refused", func(t *testing.T) {
		answer := uploadUnit(t, env, runSecret, otherSession, run)
		expectStatus(t, answer, 403)
		if msg, _ := answer.body["error"].(string); !strings.Contains(msg, "limited to the Agent sessions of a Run") {
			t.Errorf("Expected a clear body, got %s", answer.raw)
		}
		answer, _ = offsetsOf(t, env, runSecret, otherSession)
		expectStatus(t, answer, 403)
	})
}

func TestAgentSessionUploadBodyLimit(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	line := append(bytes.Repeat([]byte("a"), 1<<20-1), '\n')
	unit := bytes.Repeat(line, 32)
	answer := uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: unit})
	expectStatus(t, answer, 200)
	if answer.body["end_offset"] != float64(32<<20) {
		t.Errorf("Expected a unit of 32 MiB stored whole, got %s", answer.raw)
	}
	if got := storedBytes(t, env, o.project, laptopSession, "session.jsonl"); got != string(unit) {
		t.Errorf("Expected the 32 MiB unit stored byte for byte, got %d bytes", len(got))
	}

	tooLarge := bytes.Repeat(line, 34)
	answer = uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(unit), body: tooLarge})
	expectStatus(t, answer, 413)
	// Small as sent, but a unit over 32 MiB once decoded.
	answer = uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(unit),
		body: zstdCompressed(t, bytes.Repeat(line, 33)), headers: map[string]string{"Content-Encoding": "zstd"}})
	expectStatus(t, answer, 413)
	_, files := offsetsOf(t, env, o.secret, laptopSession)
	if got := endOffsets(files)["session.jsonl"]; got != fmt.Sprintf("1 %d", len(unit)) {
		t.Errorf("Expected a request over the limit to store nothing, got %s", got)
	}
}

// agentDaysKept lists the stored days of the raw layer of a Project.
func agentDaysKept(t *testing.T, env *TestEnv, projectID int64) []string {
	t.Helper()
	rows, err := env.ClickHouse.Conn().Query(context.Background(),
		"SELECT DISTINCT toString(day) AS d FROM agent_session_files WHERE project_id = ? ORDER BY d", uint32(projectID))
	if err != nil {
		t.Fatalf("Read the raw days: %v", err)
	}
	defer rows.Close()
	days := []string{}
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			t.Fatal(err)
		}
		days = append(days, day)
	}
	return days
}

// Retention, Project deletion and the disk limit drop the raw days of the
// Agent Project, as they drop Logs' days.
func TestTheRawDaysOfAnAgentProjectFollowItsRetentionItsDeletionAndTheDiskLimit(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	consoleExec(t, env, "UPDATE projects SET log_retention_days = 2 WHERE id = ?", o.project)
	// With no Project left, the cleanup deletes nothing.
	o.rows.project(o.organization)

	at := func(day int) time.Time { return time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC) }
	offset := 0
	for _, day := range []int{1, 3, 4} {
		env.Clock.Set(at(day))
		body := lines(fmt.Sprintf("October %d", day))
		expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: offset, body: body}), 200)
		offset += len(body)
		env.ProcessAgentFiles(t)
	}
	if got := agentDaysKept(t, env, o.project); !slices.Equal(got, []string{"2026-10-01", "2026-10-03", "2026-10-04"}) {
		t.Fatalf("Expected three raw days, got %v", got)
	}

	// The disk limit drops the oldest day first.
	use, total := agentDiskUse(t, env)
	if len(use) != 3 {
		t.Fatalf("Expected three days on local disk, got %v", use)
	}
	if err := env.ClickHouseLogs.EnforceDiskLimit(storage.LogDiskLimit{Bytes: total - 1}, []storage.DayFamily{storage.LogDays, storage.AgentDays}); err != nil {
		t.Fatalf("Enforcing the disk limit failed: %v", err)
	}
	if got := agentDaysKept(t, env, o.project); !slices.Equal(got, []string{"2026-10-03", "2026-10-04"}) {
		t.Errorf("Expected the disk limit to drop October 1st, got %v", got)
	}

	// Retention of 2 days on October 6th keeps October 4th.
	env.Clock.Set(at(6))
	env.DeleteLogsPastRetention(t)
	if got := agentDaysKept(t, env, o.project); !slices.Equal(got, []string{"2026-10-04"}) {
		t.Errorf("Expected Retention to keep only October 4th, got %v", got)
	}
	// What the server has stays, so the next upload continues the file.
	_, files := offsetsOf(t, env, o.secret, laptopSession)
	if got := endOffsets(files)["session.jsonl"]; got != fmt.Sprintf("1 %d", offset) {
		t.Errorf("Expected the end offset %d after Retention, got %s", offset, got)
	}

	consoleExec(t, env, "DELETE FROM projects WHERE id = ?", o.project)
	env.DeleteLogsPastRetention(t)
	if got := agentDaysKept(t, env, o.project); len(got) != 0 {
		t.Errorf("Expected the deleted Agent Project to have no raw days, got %v", got)
	}
}

// agentDiskUse is how much local disk each day of the raw layer takes.
func agentDiskUse(t *testing.T, env *TestEnv) (map[string]int64, int64) {
	t.Helper()
	rows, err := env.ClickHouse.Conn().Query(context.Background(), `
		SELECT partition_id, toInt64(sum(bytes_on_disk)) FROM system.parts
		WHERE database = currentDatabase() AND active AND table = 'agent_session_files'
		GROUP BY partition_id`)
	if err != nil {
		t.Fatalf("Read the raw layer's disk use: %v", err)
	}
	defer rows.Close()
	use := map[string]int64{}
	var total int64
	for rows.Next() {
		var id string
		var bytes int64
		if err := rows.Scan(&id, &bytes); err != nil {
			t.Fatal(err)
		}
		use[id] = bytes
		total += bytes
	}
	return use, total
}
