package main_test

import "testing"

// An uploader can learn the parent of an Agent session after its first
// upload, and the server keeps the first parent that it was told: a later
// upload that names another parent does not change it.
func TestTheFirstParentOfAnAgentSessionIsKept(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	first, second, third := lines("Add a cart page"), lines("Now the checkout"), lines("Ship it")

	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: first}), 200)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(first),
		parent: otherSession, body: second}), 200)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(first) + len(second),
		parent: runSession, body: third}), 200)

	_, files := offsetsOf(t, env, o.secret, laptopSession)
	if files.AgentSession == nil || files.AgentSession.ParentSessionID != otherSession {
		t.Errorf("Expected the first parent %s to stay, got %+v", otherSession, files.AgentSession)
	}
}
