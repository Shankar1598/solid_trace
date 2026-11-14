package main_test

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// The versions of a path of an Agent session: a trimmed .jsonl file starts
// a new version from offset 0, a changed file that is not .jsonl is a new
// version, and a unit over 32 MiB is stored cut. Also the captured
// repositories check of an upload.

const (
	trimSession   = "5a0e7c43-9b1d-4e2f-8c6a-3d4b5e6f7a81"
	cutSession    = "9e2d4c61-7a3b-4f5e-b1c8-0a9d8e7f6c52"
	teamSession   = "2f8b6a14-3c5d-4e7f-9a0b-1c2d3e4f5a63"
	caseSession   = "6c4a2e80-1b3d-4f5a-8e7c-9d0b1a2c3e74"
	outerSession  = "d1e2f3a4-b5c6-4d7e-8f90-a1b2c3d4e585"
	runOutSession = "8a7b6c5d-4e3f-4a1b-9c8d-7e6f5a4b3c96"
)

// rowOf is one row of agent_session_files.
type rowOf struct {
	offset, endOffset, size int
	flags                   int
}

// storedRows is the rows of a version of a path, in offset order, once
// Agent file processing has caught up.
func storedRows(t *testing.T, env *TestEnv, projectID int64, sessionID, path string, version int) []rowOf {
	t.Helper()
	env.ProcessAgentFiles(t)
	rows, err := env.ClickHouse.Conn().Query(context.Background(), `
		SELECT toInt64(offset), toInt64(end_offset), toInt64(length(bytes)), toInt64(flags) FROM agent_session_files
		WHERE project_id = ? AND session_id = ? AND path = ? AND version = ? ORDER BY offset`,
		uint32(projectID), sessionID, path, uint32(version))
	if err != nil {
		t.Fatalf("Read the rows of %s: %v", path, err)
	}
	defer rows.Close()
	var list []rowOf
	for rows.Next() {
		var offset, end, size, flags int64
		if err := rows.Scan(&offset, &end, &size, &flags); err != nil {
			t.Fatal(err)
		}
		list = append(list, rowOf{int(offset), int(end), int(size), int(flags)})
	}
	return list
}

func fileOf(files sessionFiles, path string) (version, endOffset int, hash string) {
	for _, f := range files.Files {
		if f.Path == path {
			return f.Version, f.EndOffset, f.ContentHash
		}
	}
	return 0, 0, ""
}

func flagsOf(t *testing.T, env *TestEnv, secret, sessionID string) []string {
	t.Helper()
	answer, files := offsetsOf(t, env, secret, sessionID)
	expectStatus(t, answer, 200)
	if files.AgentSession == nil {
		t.Fatalf("Expected the Agent session %s, got %s", sessionID, answer.raw)
	}
	return files.AgentSession.Flags
}

func TestAgentSessionPathVersions(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	before := lines("Add a cart page", "Use the shop's colours", "Now the checkout")
	after := lines("Summary of the cart work")

	t.Run("a trimmed .jsonl file starts version 2 at offset 0, and the Agent session is flagged trimmed", func(t *testing.T) {
		expectStatus(t, uploadUnit(t, env, o.secret, trimSession, unitUpload{path: "session.jsonl", body: before}), 200)
		if got := flagsOf(t, env, o.secret, trimSession); len(got) != 0 {
			t.Fatalf("Expected no flags before a trim, got %v", got)
		}
		answer := uploadUnit(t, env, o.secret, trimSession, unitUpload{path: "session.jsonl", version: 2, body: after})
		expectStatus(t, answer, 200)
		if answer.body["version"] != float64(2) || answer.body["end_offset"] != float64(len(after)) || answer.body["stored"] != true {
			t.Errorf("Expected version 2 stored up to %d, got %s", len(after), answer.raw)
		}
		if got := flagsOf(t, env, o.secret, trimSession); !slices.Equal(got, []string{"trimmed"}) {
			t.Errorf("Expected the Agent session flagged trimmed, got %v", got)
		}
		more := lines("And the tests")
		expectStatus(t, uploadUnit(t, env, o.secret, trimSession, unitUpload{path: "session.jsonl", version: 2, offset: len(after), body: more}), 200)
		if got := storedVersion(t, env, o.project, trimSession, "session.jsonl", 2); got != string(after)+string(more) {
			t.Errorf("Expected version 2 stored, got %q", got)
		}
		if got := storedVersion(t, env, o.project, trimSession, "session.jsonl", 1); got != string(before) {
			t.Errorf("Expected version 1 to stay, got %q", got)
		}
		answer = uploadUnit(t, env, o.secret, trimSession, unitUpload{path: "session.jsonl", offset: len(before), body: more})
		expectStatus(t, answer, 409)
	})

	t.Run("the offsets call returns the highest version of each path, with its end offset and hash", func(t *testing.T) {
		expectStatus(t, uploadUnit(t, env, o.secret, trimSession, unitUpload{path: "tool-results/r1.txt", contentHash: "sha256:aa", body: []byte("first output")}), 200)
		_, files := offsetsOf(t, env, o.secret, trimSession)
		want := map[string]string{
			"session.jsonl":       fmt.Sprintf("2 %d", len(after)+len(lines("And the tests"))),
			"tool-results/r1.txt": "1 12",
		}
		if got := endOffsets(files); !mapsEqual(got, want) {
			t.Errorf("Expected %v, got %v", want, got)
		}
		if _, _, hash := fileOf(files, "tool-results/r1.txt"); hash != "sha256:aa" {
			t.Errorf("Expected the hash sha256:aa, got %q", hash)
		}
	})

	t.Run("a file that is not .jsonl with the same hash stores nothing, and with a new hash is a new version", func(t *testing.T) {
		for _, version := range []int{1, 2} {
			answer := uploadUnit(t, env, o.secret, trimSession, unitUpload{path: "tool-results/r1.txt", version: version, contentHash: "sha256:aa", body: []byte("first output")})
			expectStatus(t, answer, 200)
			if answer.body["stored"] != false || answer.body["version"] != float64(1) {
				t.Errorf("Expected the same hash as version %d to store nothing, got %s", version, answer.raw)
			}
		}
		answer := uploadUnit(t, env, o.secret, trimSession, unitUpload{path: "tool-results/r1.txt", contentHash: "sha256:bb", body: []byte("changed output")})
		expectStatus(t, answer, 409)

		answer = uploadUnit(t, env, o.secret, trimSession, unitUpload{path: "tool-results/r1.txt", version: 2, contentHash: "sha256:bb", body: []byte("changed output")})
		expectStatus(t, answer, 200)
		if answer.body["stored"] != true || answer.body["version"] != float64(2) {
			t.Errorf("Expected a new hash stored as version 2, got %s", answer.raw)
		}
		_, files := offsetsOf(t, env, o.secret, trimSession)
		if version, end, hash := fileOf(files, "tool-results/r1.txt"); version != 2 || end != 14 || hash != "sha256:bb" {
			t.Errorf("Expected version 2 up to 14 with sha256:bb, got %d %d %q", version, end, hash)
		}
		if got := storedVersion(t, env, o.project, trimSession, "tool-results/r1.txt", 2); got != "changed output" {
			t.Errorf("Expected version 2 stored, got %q", got)
		}
		if got := storedVersion(t, env, o.project, trimSession, "tool-results/r1.txt", 1); got != "first output" {
			t.Errorf("Expected version 1 to stay, got %q", got)
		}
		if got := flagsOf(t, env, o.secret, trimSession); !slices.Equal(got, []string{"trimmed"}) {
			t.Errorf("Expected a changed file not to flag the Agent session, got %v", got)
		}
	})

	t.Run("a file that is not .jsonl is whole, with a content hash", func(t *testing.T) {
		for _, u := range []unitUpload{
			{path: "tool-results/r2.txt", body: []byte("output")},
			{path: "tool-results/r2.txt", offset: 6, contentHash: "sha256:cc", body: []byte("output")},
		} {
			expectStatus(t, uploadUnit(t, env, o.secret, trimSession, u), 400)
		}
	})

	t.Run("a repository that is not in the list is refused with a clear body", func(t *testing.T) {
		answer := uploadUnit(t, env, o.secret, outerSession, unitUpload{path: "session.jsonl", repository: "github.com/acme/billing", body: before})
		expectStatus(t, answer, 403)
		if msg, _ := answer.body["error"].(string); !strings.Contains(msg, "github.com/acme/billing") || !strings.Contains(msg, "captured repositories") {
			t.Errorf("Expected a body that names the repository and the list, got %s", answer.raw)
		}
		if answer, _ := offsetsOf(t, env, o.secret, outerSession); !strings.Contains(answer.raw, `"agent_session":null`) {
			t.Errorf("Expected nothing stored, got %s", answer.raw)
		}
	})

	t.Run("a repository matched by a host/workspace/* entry, or with its host in other letter case, is accepted", func(t *testing.T) {
		consoleExec(t, env, "UPDATE projects SET captured_repositories = ? WHERE id = ?",
			`["github.com/acme/shop","bitbucket.org/acme-team/*"]`, o.project)
		expectStatus(t, uploadUnit(t, env, o.secret, teamSession, unitUpload{path: "session.jsonl", repository: "bitbucket.org/acme-team/api", body: before}), 200)
		expectStatus(t, uploadUnit(t, env, o.secret, caseSession, unitUpload{path: "session.jsonl", repository: "GitHub.com/acme/shop", body: before}), 200)
		expectStatus(t, uploadUnit(t, env, o.secret, outerSession, unitUpload{path: "session.jsonl", repository: "bitbucket.org/acme-other/api", body: before}), 403)
	})

	t.Run("a token limited to a Run's Agent sessions is not checked against the list", func(t *testing.T) {
		runSecret, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization,
			scopes: []string{"sessions:write"}, sessionIDs: []string{runOutSession}})
		expectStatus(t, uploadUnit(t, env, runSecret, runOutSession, unitUpload{path: "session.jsonl", repository: "github.com/acme/billing",
			runID: "run-0194", project: "billing", body: before}), 200)
	})
}

func TestAgentSessionUnitsOver32MiB(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	const mib = 1 << 20
	longLine := append(bytes.Repeat([]byte("a"), 40*mib), '\n')

	t.Run("a line over 32 MiB comes as its first 32 MiB with its full size, and is stored cut and flagged", func(t *testing.T) {
		answer := uploadUnit(t, env, o.secret, cutSession, unitUpload{path: "session.jsonl", size: len(longLine), body: longLine[:32*mib]})
		expectStatus(t, answer, 200)
		if answer.body["end_offset"] != float64(len(longLine)) || answer.body["stored"] != true {
			t.Errorf("Expected the end offset %d, got %s", len(longLine), answer.raw)
		}
		if got := flagsOf(t, env, o.secret, cutSession); !slices.Equal(got, []string{"cut"}) {
			t.Errorf("Expected the Agent session flagged cut, got %v", got)
		}
		next := lines("After the long line")
		expectStatus(t, uploadUnit(t, env, o.secret, cutSession, unitUpload{path: "session.jsonl", offset: len(longLine), body: next}), 200)

		rows := storedRows(t, env, o.project, cutSession, "session.jsonl", 1)
		if len(rows) < 2 {
			t.Fatalf("Expected the cut unit and the next one, got %v", rows)
		}
		cutRows, last := rows[:len(rows)-1], rows[len(rows)-1]
		stored := 0
		for _, r := range cutRows {
			stored += r.size
			if r.flags&1 == 0 {
				t.Errorf("Expected each row of the cut unit marked cut, got %+v", r)
			}
		}
		if stored != 32*mib {
			t.Errorf("Expected 32 MiB stored of the long line, got %d", stored)
		}
		if end := cutRows[len(cutRows)-1].endOffset; end != len(longLine) {
			t.Errorf("Expected the cut unit's last row to end at %d, its end on disk, got %d", len(longLine), end)
		}
		if last.offset != len(longLine) || last.flags != 0 || last.size != len(next) {
			t.Errorf("Expected the next unit stored whole after the cut, got %+v", last)
		}
	})

	t.Run("a file that is not .jsonl over 32 MiB is stored cut", func(t *testing.T) {
		answer := uploadUnit(t, env, o.secret, cutSession, unitUpload{path: "tool-results/big.txt", contentHash: "sha256:dd",
			size: 33 * mib, body: longLine[:32*mib]})
		expectStatus(t, answer, 200)
		if answer.body["end_offset"] != float64(33*mib) {
			t.Errorf("Expected the end offset %d, got %s", 33*mib, answer.raw)
		}
	})

	t.Run("a body over 32 MiB once decoded is stored cut, not refused", func(t *testing.T) {
		line := append(bytes.Repeat([]byte("b"), mib-1), '\n')
		unit := bytes.Repeat(line, 33)
		answer := uploadUnit(t, env, o.secret, cutSession, unitUpload{path: "subagents/agent-a1.jsonl",
			body: zstdCompressed(t, unit), headers: map[string]string{"Content-Encoding": "zstd"}})
		expectStatus(t, answer, 200)
		if answer.body["end_offset"] != float64(len(unit)) {
			t.Errorf("Expected the end offset %d, got %s", len(unit), answer.raw)
		}
		if got := storedVersion(t, env, o.project, cutSession, "subagents/agent-a1.jsonl", 1); got != string(unit[:32*mib]) {
			t.Errorf("Expected the first 32 MiB stored, got %d bytes", len(got))
		}
	})

	t.Run("a .jsonl body over 32 MiB once decoded with no newline at its end is refused", func(t *testing.T) {
		unit := bytes.Repeat([]byte("c"), 33*mib)
		answer := uploadUnit(t, env, o.secret, cutSession, unitUpload{path: "subagents/agent-a2.jsonl",
			body: zstdCompressed(t, unit), headers: map[string]string{"Content-Encoding": "zstd"}})
		expectStatus(t, answer, 400)
		answer = uploadUnit(t, env, o.secret, cutSession, unitUpload{path: "subagents/agent-a2.jsonl", size: len(unit),
			body: zstdCompressed(t, unit), headers: map[string]string{"Content-Encoding": "zstd"}})
		expectStatus(t, answer, 400)
	})

	t.Run("a size that does not fit the body is refused", func(t *testing.T) {
		for _, u := range []unitUpload{
			{path: "other.jsonl", size: 10, body: lines("Longer than ten bytes")},
			{path: "other.jsonl", size: 64 * mib, body: lines("Less than 32 MiB")},
		} {
			expectStatus(t, uploadUnit(t, env, o.secret, cutSession, u), 400)
		}
	})
}
