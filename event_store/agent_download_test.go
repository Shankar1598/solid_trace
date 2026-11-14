package main_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
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
	})
}
