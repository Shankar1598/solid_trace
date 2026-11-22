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
