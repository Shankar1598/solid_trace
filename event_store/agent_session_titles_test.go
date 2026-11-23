package main_test

import (
	"context"
	"testing"
	"time"
)

// The title of an Agent session (agent_sessions.title) on the Agent
// sessions list and the Agent session page: the last /rename of the main
// transcript, else the last title that Claude Code made, else the start of
// the first person message. Each part of an upload adds a row, and the
// merge of the rows keeps the title that wins.

func aiTitleLine(title string) string {
	return `{"type":"ai-title","aiTitle":"` + title + `","sessionId":"x"}`
}

func customTitleLine(title string) string {
	return `{"type":"custom-title","customTitle":"` + title + `","sessionId":"x"}`
}

// listedTitles are the titles of the list, by Agent session id.
func listedTitles(t *testing.T, env *TestEnv, projectID int64) map[string]string {
	t.Helper()
	titles := map[string]string{}
	for _, s := range listAgentSessions(t, env, projectID, octoberRange).AgentSessions {
		titles[s.ID] = s.Title
	}
	return titles
}

func TestTheTitleOfAnAgentSessionUploadedInPartsIsItsLastRenameElseItsLastTitle(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)

	// part uploads the next lines of the main transcript of an Agent
	// session, after the parts before it, and parses them.
	ends := map[string]int{}
	part := func(session string, lines ...string) {
		t.Helper()
		body := jsonl(lines...)
		expectStatus(t, uploadUnit(t, env, o.secret, session, unitUpload{path: "session.jsonl", offset: ends[session], body: body}), 200)
		ends[session] += len(body)
		env.ProcessAgentFiles(t)
	}
	expectTitles := func(what string, want map[string]string) {
		t.Helper()
		listed := listedTitles(t, env, o.project)
		for session, title := range want {
			if listed[session] != title {
				t.Errorf("%s: expected the list to give %s the title %q, got %q", what, session, title, listed[session])
			}
			if got := readTimeline(t, env, o.project, session, map[string]any{}).AgentSession.Title; got != title {
				t.Errorf("%s: expected the Agent session page to give %s the title %q, got %q", what, session, title, got)
			}
		}
	}

	part(laptopSession, personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"), aiTitleLine("Cart page"))
	part(apiSession, personAt("b1", "2026-10-02T11:10:00Z", `Fix the API\nand its tests`))
	part(otherSession, personAt("c1", "2026-10-02T11:20:00Z", "Read the logs"))
	expectStatus(t, uploadUnit(t, env, o.secret, otherSession, unitUpload{path: "subagents/agent-a1.jsonl", body: jsonl(
		personAt("s1", "2026-10-02T11:20:05Z", "Find the errors"),
		customTitleLine("A subagent's name"),
		aiTitleLine("Finding the errors"),
	)}), 200)
	env.ProcessAgentFiles(t)
	expectTitles("the first parts", map[string]string{
		laptopSession: "Cart page",
		apiSession:    "Fix the API and its tests",
		otherSession:  "Read the logs",
	})

	part(laptopSession, replyAt("a1", "2026-10-02T11:00:05Z", "msg_1", "claude-opus-5-5", `{"type":"text","text":"Done."}`),
		aiTitleLine("Cart page and checkout"))
	part(apiSession, aiTitleLine("API fix"), personAt("b2", "2026-10-02T11:12:00Z", "And the docs"))
	expectTitles("a later ai-title, and an ai-title after the first message", map[string]string{
		laptopSession: "Cart page and checkout",
		apiSession:    "API fix",
	})

	part(laptopSession, customTitleLine("Cart work"))
	part(laptopSession, aiTitleLine("Cart, checkout and coupons"))
	expectTitles("a rename, then an ai-title", map[string]string{laptopSession: "Cart work"})

	part(laptopSession, customTitleLine("Checkout work"))
	expectTitles("a second rename", map[string]string{laptopSession: "Checkout work"})
}

func TestAnInstallFromBeforeTitlesGetsTheTitleColumnAtItsNextStart(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"), aiTitleLine("Cart page"),
	)}), 200)
	env.ProcessAgentFiles(t)

	// The database as an install from before titles has it: no title
	// column, and no record of the step that adds it.
	ctx := context.Background()
	for _, statement := range []string{
		`ALTER TABLE agent_sessions DROP COLUMN title`,
		`DELETE FROM schema_migrations WHERE version = 11202`,
	} {
		if err := env.ClickHouse.Conn().Exec(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	env.Reopen(t)

	expectStatus(t, uploadUnit(t, env, o.secret, apiSession, unitUpload{path: "session.jsonl", body: jsonl(
		personAt("b1", "2026-10-02T11:10:00Z", "Fix the API"), aiTitleLine("API fix"),
	)}), 200)
	env.ProcessAgentFiles(t)
	listed := listedTitles(t, env, o.project)
	if len(listed) != 2 || listed[laptopSession] != "" || listed[apiSession] != "API fix" {
		t.Errorf("Expected no title for the Agent session parsed before the start, and the new one's title, got %v", listed)
	}
}
