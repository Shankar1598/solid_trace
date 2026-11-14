package main_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/storage"
)

// The download of one Agent session directory, for a resume on another
// machine: a tar archive of the highest version of each path, as the
// server stored it (after Redaction), to a token with sessions:read for
// that Agent session.

func agentDownloadPath(sessionID string) string {
	return "/api/agent/v1/agent-sessions/" + sessionID + "/download"
}

// downloadedFile is one file of a download: its bytes and what the
// archive says of it.
type downloadedFile struct {
	bytes   string
	records map[string]string
}

// download is the answer of the download call.
type download struct {
	status  int
	header  http.Header
	raw     string
	files   map[string]downloadedFile
	order   []string
	tarErr  error
	bodyLen int
}

func downloadSession(t *testing.T, env *TestEnv, secret, sessionID string) download {
	t.Helper()
	req, _ := http.NewRequest("GET", agentDownloadPath(sessionID), nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := env.App.Test(req, 60000)
	if err != nil {
		t.Fatalf("Download of %s: %v", sessionID, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	d := download{status: resp.StatusCode, header: resp.Header, bodyLen: len(raw)}
	if resp.StatusCode != 200 {
		d.raw = string(raw)
		return d
	}
	d.files = map[string]downloadedFile{}
	archive := tar.NewReader(bytes.NewReader(raw))
	for {
		h, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			d.tarErr = err
			return d
		}
		content, err := io.ReadAll(archive)
		if err != nil {
			d.tarErr = err
			return d
		}
		d.files[h.Name] = downloadedFile{bytes: string(content), records: h.PAXRecords}
		d.order = append(d.order, h.Name)
	}
	return d
}

func expectDownloadStatus(t *testing.T, d download, status int) {
	t.Helper()
	if d.status != status {
		t.Fatalf("Expected the download to answer %d, got %d: %s", status, d.status, d.raw)
	}
	if d.tarErr != nil {
		t.Fatalf("Expected a whole tar archive, got %v", d.tarErr)
	}
}

func TestAgentSessionDownload(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	runSecret, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization,
		scopes: []string{"sessions:write", "sessions:read"}, sessionIDs: []string{runSession}})
	run := func(u unitUpload) unitUpload {
		u.runID, u.project = "run-0193", "shop"
		return u
	}
	secret := defaultSecrets()["Claude OAuth access token"]

	t.Run("the download is the redacted upload of the highest version of each path, at its relative path", func(t *testing.T) {
		first := []byte(textLine("Fix issue 42") + textLine("The token is "+secret))
		second := lines("Then run the tests")
		subagent := lines("A subagent's prompt")
		toolResult := []byte("a tool's long output, with " + secret + " in it")
		for _, u := range []unitUpload{
			{path: "session.jsonl", body: first},
			{path: "session.jsonl", offset: len(first), body: second},
			{path: "subagents/agent-a1b2.jsonl", body: subagent},
			{path: "tool-results/toolu_01.txt", contentHash: "sha256:one", body: []byte("the first output")},
			{path: "tool-results/toolu_01.txt", version: 2, contentHash: "sha256:two", body: toolResult},
		} {
			expectStatus(t, uploadUnit(t, env, runSecret, runSession, run(u)), 200)
		}
		env.ProcessAgentFiles(t)

		d := downloadSession(t, env, runSecret, runSession)
		expectDownloadStatus(t, d, 200)
		want := map[string]string{
			"session.jsonl":              strings.Replace(string(first), secret, stars(secret), 1) + string(second),
			"subagents/agent-a1b2.jsonl": string(subagent),
			"tool-results/toolu_01.txt":  strings.Replace(string(toolResult), secret, stars(secret), 1),
		}
		if len(d.files) != len(want) {
			t.Errorf("Expected the files %v, got %v", want, d.order)
		}
		for path, text := range want {
			if got, ok := d.files[path]; !ok || got.bytes != text {
				t.Errorf("Expected %s to be %q, got %q", path, text, got.bytes)
			}
		}
		if got := d.files["tool-results/toolu_01.txt"].records["SOLIDTRACE.version"]; got != "2" {
			t.Errorf("Expected the tool result's version 2, got %q", got)
		}
		if got := d.files["session.jsonl"].records["SOLIDTRACE.flags"]; got != "" {
			t.Errorf("Expected no flags on a whole file, got %q", got)
		}
		if got := d.header.Get("SolidTrace-Agent-Session-Flags"); got != "" {
			t.Errorf("Expected no flags on the Agent session, got %q", got)
		}
		if got := d.header.Get("Content-Type"); got != "application/x-tar" {
			t.Errorf("Expected a tar archive, got %q", got)
		}
	})

	t.Run("only a token with sessions:read that lists the Agent session downloads it", func(t *testing.T) {
		expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: lines("A laptop's prompt")}), 200)
		otherRun, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization,
			scopes: []string{"sessions:write", "sessions:read"}, sessionIDs: []string{otherSession}})
		expectStatus(t, uploadUnit(t, env, otherRun, otherSession, run(unitUpload{path: "session.jsonl", body: lines("Another Run")})), 200)
		env.ProcessAgentFiles(t)

		writeOnly, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization,
			scopes: []string{"sessions:write"}, sessionIDs: []string{runSession}})
		// Console never makes sessions:read with no list; a hand edit can.
		noList, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization, scopes: []string{"sessions:read"}})
		mcp, _ := o.rows.apiToken(apiTokenRow{user: o.user})
		access, _, _ := o.rows.accessToken(accessTokenRow{user: o.user, organization: o.organization,
			scopes: []string{"sessions:write", "sessions:read"}, resource: "http://console.test/factory",
			expiresAt: o.rows.now.Add(time.Hour)})
		expired := o.rows.now.Add(-time.Minute)
		expiredRun, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization,
			scopes: []string{"sessions:read"}, sessionIDs: []string{runSession}, expiresAt: &expired})

		for _, c := range []struct {
			name, secret, session string
			status                int
		}{
			{"a token for another Agent session", otherRun, runSession, 403},
			{"a token for another Agent session, the other way", runSecret, laptopSession, 403},
			{"a token without sessions:read", writeOnly, runSession, 403},
			{"the person's own sessions:write token", o.secret, laptopSession, 403},
			{"a sessions:read token with no list", noList, laptopSession, 403},
			{"an API token with no scope", mcp, runSession, 403},
			{"an OAuth access token", access, runSession, 403},
			{"an expired token", expiredRun, runSession, 401},
			{"no token", "", runSession, 401},
		} {
			d := downloadSession(t, env, c.secret, c.session)
			if d.status != c.status || d.files != nil {
				t.Errorf("%s: expected %d and no files, got %d: %s", c.name, c.status, d.status, d.raw)
			}
		}
		// The token's own Agent session still downloads, in either case of
		// its id.
		expectDownloadStatus(t, downloadSession(t, env, otherRun, strings.ToUpper(otherSession)), 200)
		if d := downloadSession(t, env, otherRun, otherSession); d.files["session.jsonl"].bytes != string(lines("Another Run")) {
			t.Errorf("Expected the other Run's Agent session, got %v", d.order)
		}
	})

	t.Run("a token of another Organization does not reach the Agent session, with the same id in its list", func(t *testing.T) {
		globex := o.rows.organization("Globex")
		o.rows.agentProject(globex, "github.com/globex/shop")
		o.rows.member(globex, o.otherUser)
		globexRun, _ := o.rows.apiToken(apiTokenRow{user: o.otherUser, organization: globex,
			scopes: []string{"sessions:read"}, sessionIDs: []string{runSession}})
		if d := downloadSession(t, env, globexRun, runSession); d.status != 404 || d.files != nil {
			t.Errorf("Expected 404 and no files, got %d: %s", d.status, d.raw)
		}
		initech := o.rows.organization("Initech")
		o.rows.member(initech, o.user)
		offRun, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: initech,
			scopes: []string{"sessions:read"}, sessionIDs: []string{runSession}})
		if d := downloadSession(t, env, offRun, runSession); d.status != 403 || d.files != nil {
			t.Errorf("Expected 403 with Agent sessions off, got %d: %s", d.status, d.raw)
		}
	})

	t.Run("an Agent session in the list that the server does not have is 404", func(t *testing.T) {
		unknown := "5f0c1d2e-3a4b-4c5d-8e6f-7a8b9c0d1e2f"
		token, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization,
			scopes: []string{"sessions:read"}, sessionIDs: []string{unknown}})
		if d := downloadSession(t, env, token, unknown); d.status != 404 {
			t.Errorf("Expected 404, got %d: %s", d.status, d.raw)
		}
		if d := downloadSession(t, env, token, "not-a-uuid"); d.status != 400 {
			t.Errorf("Expected 400 for an id that is not a UUID, got %d: %s", d.status, d.raw)
		}
	})
}

// changingDownloads reads from ClickHouse, but its reads of the bytes fail
// after the first file, as when Retention drops a day during a download.
type changingDownloads struct {
	*storage.ClickHouse
}

func (d changingDownloads) ReadAgentDownload(ctx context.Context, projectID uint32, sessionID uuid.UUID, files []storage.AgentDownloadFile, write func(int, []byte) error) error {
	return d.ClickHouse.ReadAgentDownload(ctx, projectID, sessionID, files, func(file int, bytes []byte) error {
		if file > 0 {
			return storage.ErrAgentDownloadChanged
		}
		return write(file, bytes)
	})
}

func TestAgentSessionDownloadThatFailsAfterItsFirstByte(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	reader, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization, scopes: []string{"sessions:read"},
		sessionIDs: []string{laptopSession}})
	for _, path := range []string{"session.jsonl", "subagents/agent-a1.jsonl"} {
		expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: path, body: lines("A line of " + path)}), 200)
	}
	env.ProcessAgentFiles(t)
	app := newAgentAppWith(t, env, "dev", changingDownloads{env.ClickHouse})

	req, _ := http.NewRequest("GET", agentDownloadPath(laptopSession), nil)
	req.Header.Set("Authorization", "Bearer "+reader)
	resp, err := app.Test(req, 10000)
	if err != nil {
		return // The client sees the error.
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	archive := tar.NewReader(bytes.NewReader(raw))
	var names []string
	for {
		h, err := archive.Next()
		if errors.Is(err, io.EOF) {
			t.Fatalf("Expected an error, got a whole archive of %v", names)
		}
		if err != nil {
			return
		}
		names = append(names, h.Name)
		if _, err := io.ReadAll(archive); err != nil {
			return
		}
	}
}

const (
	pendingSession ="1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c01"
	retainSession  = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c02"
)

func TestAgentSessionDownloadOfWhatIsNotWhole(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	reader, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization, scopes: []string{"sessions:read"},
		sessionIDs: []string{pendingSession, cutSession, trimSession, retainSession}})
	upload := func(sessionID string, u unitUpload) {
		t.Helper()
		expectStatus(t, uploadUnit(t, env, o.secret, sessionID, u), 200)
	}

	t.Run("a unit that is stored but not yet in ClickHouse makes the download wait", func(t *testing.T) {
		first := lines("The first prompt")
		upload(pendingSession, unitUpload{path: "session.jsonl", body: first})
		env.ProcessAgentFiles(t)
		upload(pendingSession, unitUpload{path: "session.jsonl", offset: len(first), body: lines("The last prompt")})

		d := downloadSession(t, env, reader, pendingSession)
		if d.status != 503 || d.header.Get("Retry-After") != "1" || d.files != nil {
			t.Errorf("Expected 503 with Retry-After 1, got %d %q: %s", d.status, d.header.Get("Retry-After"), d.raw)
		}
		env.ProcessAgentFiles(t)
		d = downloadSession(t, env, reader, pendingSession)
		expectDownloadStatus(t, d, 200)
		if got, want := d.files["session.jsonl"].bytes, string(first)+string(lines("The last prompt")); got != want {
			t.Errorf("Expected %q, got %q", want, got)
		}
	})

	t.Run("a cut unit is marked cut with the range that has no bytes", func(t *testing.T) {
		const mib = 1 << 20
		longLine := append(bytes.Repeat([]byte("a"), 40*mib), '\n')
		next := lines("After the long line")
		upload(cutSession, unitUpload{path: "session.jsonl", size: len(longLine), body: longLine[:32*mib]})
		upload(cutSession, unitUpload{path: "session.jsonl", offset: len(longLine), body: next})
		env.ProcessAgentFiles(t)

		d := downloadSession(t, env, reader, cutSession)
		expectDownloadStatus(t, d, 200)
		f := d.files["session.jsonl"]
		if f.bytes != string(longLine[:32*mib])+string(next) {
			t.Errorf("Expected the stored 32 MiB and the next line, got %d bytes", len(f.bytes))
		}
		want := map[string]string{
			"SOLIDTRACE.version":      "1",
			"SOLIDTRACE.start_offset": "0",
			"SOLIDTRACE.end_offset":   fmt.Sprint(len(longLine) + len(next)),
			"SOLIDTRACE.flags":        "cut",
			"SOLIDTRACE.missing":      fmt.Sprintf("%d-%d", 32*mib, len(longLine)),
		}
		for key, value := range want {
			if f.records[key] != value {
				t.Errorf("Expected %s %q, got %q", key, value, f.records[key])
			}
		}
		if got := d.header.Get("SolidTrace-Agent-Session-Flags"); got != "cut" {
			t.Errorf("Expected the Agent session flagged cut, got %q", got)
		}
	})

	t.Run("a trimmed .jsonl file comes back as its new version", func(t *testing.T) {
		upload(trimSession, unitUpload{path: "session.jsonl", body: lines("A long first version", "of two lines")})
		short := lines("Short")
		upload(trimSession, unitUpload{path: "session.jsonl", version: 2, body: short})
		env.ProcessAgentFiles(t)

		d := downloadSession(t, env, reader, trimSession)
		expectDownloadStatus(t, d, 200)
		if f := d.files["session.jsonl"]; f.bytes != string(short) || f.records["SOLIDTRACE.version"] != "2" {
			t.Errorf("Expected version 2 %q, got version %s %q", short, f.records["SOLIDTRACE.version"], f.bytes)
		}
		if got := d.header.Get("SolidTrace-Agent-Session-Flags"); got != "trimmed" {
			t.Errorf("Expected the Agent session flagged trimmed, got %q", got)
		}
	})

	t.Run("after Retention drops the first days, a file is partial from the offset where it starts", func(t *testing.T) {
		consoleExec(t, env, "UPDATE projects SET log_retention_days = 2 WHERE id = ?", o.project)
		// With no Project left, the cleanup deletes nothing.
		o.rows.project(o.organization)
		at := func(day int) time.Time { return time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC) }
		env.Clock.Set(at(1))
		subagent := lines("A subagent of October 1st")
		upload(retainSession, unitUpload{path: "subagents/agent-a1.jsonl", body: subagent})
		offset := 0
		var kept []byte
		for _, day := range []int{1, 3, 4} {
			env.Clock.Set(at(day))
			kept = lines(fmt.Sprintf("October %d", day))
			upload(retainSession, unitUpload{path: "session.jsonl", offset: offset, body: kept})
			offset += len(kept)
			env.ProcessAgentFiles(t)
		}
		env.Clock.Set(at(6))
		env.DeleteLogsPastRetention(t)

		d := downloadSession(t, env, reader, retainSession)
		expectDownloadStatus(t, d, 200)
		f := d.files["session.jsonl"]
		start := offset - len(kept)
		if f.bytes != string(kept) || f.records["SOLIDTRACE.flags"] != "partial" ||
			f.records["SOLIDTRACE.start_offset"] != fmt.Sprint(start) || f.records["SOLIDTRACE.end_offset"] != fmt.Sprint(offset) {
			t.Errorf("Expected October 4th, partial from %d to %d, got %q %v", start, offset, f.bytes, f.records)
		}
		// A file that Retention dropped whole is there with no bytes,
		// partial at its end.
		s, ok := d.files["subagents/agent-a1.jsonl"]
		if !ok || s.bytes != "" || s.records["SOLIDTRACE.flags"] != "partial" ||
			s.records["SOLIDTRACE.start_offset"] != fmt.Sprint(len(subagent)) {
			t.Errorf("Expected the dropped subagent file empty and partial at %d, got %v %+v", len(subagent), ok, s)
		}
		if !slices.Equal(d.order, []string{"session.jsonl", "subagents/agent-a1.jsonl"}) {
			t.Errorf("Expected the files in path order, got %v", d.order)
		}
		if got := d.header.Get("SolidTrace-Agent-Session-Flags"); got != "partial" {
			t.Errorf("Expected the Agent session flagged partial, got %q", got)
		}
	})
}
