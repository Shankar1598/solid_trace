package main_test

import (
	"strings"
	"testing"
)

const refusedSession = "5e2a7c14-9b3d-4a68-b0f1-6c8d3e7a9b25"

// An upload names a path and a folder under the Agent session's root, and
// the Agent session. Each form that could point outside the root, or name
// the same file in two ways, is refused with 400 and nothing is stored.
func TestAnUploadWithAPathThatIsNotPlainIsRefused(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	body := lines("Add a cart page")

	for name, u := range map[string]unitUpload{
		"a .. part in the middle of the path":   {path: "tool-results/../session.jsonl"},
		"a . part in the middle of the path":    {path: "tool-results/./out.jsonl"},
		"an empty part of the path":             {path: "tool-results//out.jsonl"},
		"a trailing slash on the path":          {path: "tool-results/out.jsonl/"},
		"a backslash in the path":               {path: `tool-results\out.jsonl`},
		"a NUL byte in the path":                {path: "out\x00.jsonl"},
		"a path that is only ..":                {path: ".."},
		"a .. part in the folder":               {path: "session.jsonl", folder: "web/../../etc"},
		"a folder that starts with ..":          {path: "session.jsonl", folder: "../web"},
		"a . part in the folder":                {path: "session.jsonl", folder: "web/./cart"},
		"a backslash in the folder":             {path: "session.jsonl", folder: `web\cart`},
		"the Agent session as its own parent":   {path: "session.jsonl", parent: refusedSession},
		"a parent that is not an Agent session": {path: "session.jsonl", parent: "not-a-uuid"},
	} {
		t.Run(name, func(t *testing.T) {
			u.body = body
			expectStatus(t, uploadUnit(t, env, o.secret, refusedSession, u), 400)
		})
	}
	if _, files := offsetsOf(t, env, o.secret, refusedSession); files.AgentSession != nil || len(files.Files) != 0 {
		t.Errorf("Expected a refused upload to store nothing, got %+v", files)
	}
}

// The Agent session id is the 36-character UUID with hyphens, as the
// harness gives it. Another form of the same UUID (no hyphens, braces, a
// URN) would be a second name for it, so it is refused.
func TestAnAgentSessionIdThatIsNotInTheHarnessFormIsRefused(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	for name, id := range map[string]string{
		"no hyphens": strings.ReplaceAll(refusedSession, "-", ""),
		"braces":     "{" + refusedSession + "}",
		"a URN":      "urn:uuid:" + refusedSession,
	} {
		t.Run(name, func(t *testing.T) {
			expectStatus(t, uploadUnit(t, env, o.secret, id, unitUpload{path: "session.jsonl", body: lines("Add a cart page")}), 400)
			answer, _ := offsetsOf(t, env, o.secret, id)
			expectStatus(t, answer, 400)
		})
	}
}

const oneByteGapSession = "2d9f4b60-7a1c-4e35-8c92-3b5e0a6d7f18"

// The server stores a unit only at its end offset. A gap of one byte is a
// gap, and a file that is not .jsonl with another hash at the same version
// is not stored, even when its size is the same.
func TestAnUploadWithAGapOfOneByteOrAnotherHashAtTheSameVersionIsRefused(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	first, second := lines("Add a cart page"), lines("Now the checkout")

	expectStatus(t, uploadUnit(t, env, o.secret, oneByteGapSession, unitUpload{path: "session.jsonl", body: first}), 200)
	answer := uploadUnit(t, env, o.secret, oneByteGapSession, unitUpload{path: "session.jsonl", offset: len(first) + 1, body: second})
	expectStatus(t, answer, 409)
	if msg, _ := answer.body["error"].(string); !strings.HasPrefix(msg, "A gap") || answer.body["end_offset"] != float64(len(first)) {
		t.Errorf("Expected a gap at the end offset %d, got %s", len(first), answer.raw)
	}

	expectStatus(t, uploadUnit(t, env, o.secret, oneByteGapSession, unitUpload{path: "tool-results/r1.txt", contentHash: "sha256:aa", body: []byte("first output")}), 200)
	answer = uploadUnit(t, env, o.secret, oneByteGapSession, unitUpload{path: "tool-results/r1.txt", contentHash: "sha256:bb", body: []byte("other output")})
	expectStatus(t, answer, 409)
	if msg, _ := answer.body["error"].(string); !strings.HasPrefix(msg, "Another hash") {
		t.Errorf("Expected a refusal that names the other hash, got %s", answer.raw)
	}
	_, files := offsetsOf(t, env, o.secret, oneByteGapSession)
	if _, _, hash := fileOf(files, "tool-results/r1.txt"); hash != "sha256:aa" {
		t.Errorf("Expected the first hash to stay, got %q", hash)
	}
}
