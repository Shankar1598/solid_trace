package main_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The Review records of an Agent Project (ADR 0024): the parse finds the
// record that open-pr or update-pr ends its final reply with, and the read
// API lists the newest record of each PR and gives one record whole, with
// the rounds of its PR.

const reviewSession = "2c4e6a8b-0d1f-4e3a-9b5c-7d9f1b3d5e7a"

type listedReviewRecord struct {
	Key                   string    `json:"key"`
	ID                    string    `json:"id"`
	SessionID             string    `json:"session_id"`
	Path                  string    `json:"path"`
	Version               uint32    `json:"version"`
	Offset                uint64    `json:"offset"`
	StoredAt              time.Time `json:"stored_at"`
	PR                    string    `json:"pr"`
	Round                 uint32    `json:"round"`
	Title                 string    `json:"title"`
	Findings              uint32    `json:"findings"`
	Fixed                 uint32    `json:"fixed"`
	Missed                uint32    `json:"missed"`
	ConfirmationsNotGiven uint32    `json:"confirmations_not_given"`
	Earlier               int       `json:"earlier"`
}

type reviewRecordsAnswer struct {
	ReviewRecords []listedReviewRecord `json:"review_records"`
	More          bool                 `json:"more"`
}

type reviewRecordAnswer struct {
	ReviewRecord listedReviewRecord `json:"review_record"`
	Record       string             `json:"record"`
	Rounds       []struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
		Offset    uint64 `json:"offset"`
		Round     uint32 `json:"round"`
	} `json:"rounds"`
}

// prRecord is a Review record of round of pr ("" for none) with findings
// findings: one in four found by a person and the others fixed by
// reviewers, each with a long scenario, so that a record of 200 findings
// is past the Log limit of 64 KiB.
func prRecord(pr string, round, findings int) string {
	var b strings.Builder
	prJSON := "null"
	if pr != "" {
		prJSON = fmt.Sprintf("%q", pr)
	}
	fmt.Fprintf(&b, `{"version":2,"sessions":[{"id":"s1","title":"Cart page, round %d"}],"pr":%s,"round":%d,"story":{"started_from":"Add a cart page."},"reviews":[],"findings":[`, round, prJSON, round)
	for i := range findings {
		if i > 0 {
			b.WriteByte(',')
		}
		kind, outcome := "reviewer", "fixed"
		if i%4 == 3 {
			kind, outcome = "person", "open"
		}
		fmt.Fprintf(&b, `{"id":"F%d","found_by":{"kind":%q},"title":"Finding %d & <more>","scenario":"%s","outcome":%q}`,
			i, kind, i, strings.Repeat("The total is wrong when a coupon is used… ", 8), outcome)
	}
	b.WriteString(`],"confirmations":[{"id":"1.C1","given":true},{"id":"1.C2","given":false}]}`)
	return b.String()
}

// finalReply is an assistant line that ends with the record's block.
func finalReply(record string) string {
	line, _ := json.Marshal(map[string]any{
		"type": "assistant", "uuid": "a9", "timestamp": "2026-10-02T11:30:00Z",
		"message": map[string]any{"id": "msg_9", "role": "assistant", "model": "claude-opus-5-5", "content": []map[string]string{
			{"type": "text", "text": "The PR is open.\n\n```solidtrace-pr-record\n" + record + "\n```"},
		}},
	})
	return string(line)
}

func listReviewRecords(t *testing.T, env *TestEnv, projectID int64) reviewRecordsAnswer {
	t.Helper()
	env.ProcessAgentFiles(t)
	status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/review-records", projectID), map[string]any{})
	if status != 200 {
		t.Fatalf("Expected 200, got %d: %s", status, raw)
	}
	var answer reviewRecordsAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode %s: %v", raw, err)
	}
	return answer
}

func showReviewRecord(t *testing.T, env *TestEnv, projectID int64, body map[string]any) (int, reviewRecordAnswer) {
	t.Helper()
	status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/review-records/record", projectID), body)
	var answer reviewRecordAnswer
	if status == 200 {
		if err := json.Unmarshal(raw, &answer); err != nil {
			t.Fatalf("Decode %s: %v", raw, err)
		}
	}
	return status, answer
}

func TestTheReviewRecordsListTheNewestRecordOfEachPRAndGiveEachRecordWhole(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	const pr = "https://github.com/acme/shop/pull/7"

	first, second, noPR := prRecord(pr, 1, 200), prRecord(pr, 2, 8), prRecord("", 1, 4)
	if len(first) <= 64<<10 {
		t.Fatalf("Expected a record past the Log limit, got %d bytes", len(first))
	}
	// Round 1 in one Agent session; round 2, and a record with no PR, in
	// another, a day later.
	opening := jsonl(transcript[0], finalReply(first))
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: opening}), 200)
	env.ProcessAgentFiles(t)
	env.Clock.Set(testNow.Add(24 * time.Hour))
	update := jsonl(transcript[0], finalReply(second), finalReply(noPR))
	expectStatus(t, uploadUnit(t, env, o.secret, reviewSession, unitUpload{path: "session.jsonl", body: update}), 200)

	answer := listReviewRecords(t, env, o.project)
	if len(answer.ReviewRecords) != 2 || answer.More {
		t.Fatalf("Expected one row for the PR and one for the record with no PR, got %+v", answer)
	}
	// The same store time: the later line comes first.
	none, newest := answer.ReviewRecords[0], answer.ReviewRecords[1]
	if newest.Key != pr || newest.PR != pr || newest.Round != 2 || newest.SessionID != reviewSession || newest.Earlier != 1 ||
		newest.Title != "Cart page, round 2" || newest.Findings != 8 || newest.Fixed != 6 || newest.Missed != 2 || newest.ConfirmationsNotGiven != 1 {
		t.Errorf("Expected round 2 of %s with 8 findings, 6 fixed, 2 missed, 1 confirmation not given and 1 earlier, got %+v", pr, newest)
	}
	if none.Key != reviewSession || none.PR != "" || none.Earlier != 0 || none.Findings != 4 {
		t.Errorf("Expected the record with no PR under its Agent session's id, got %+v", none)
	}

	status, shown := showReviewRecord(t, env, o.project, map[string]any{"key": pr})
	if status != 200 || shown.ReviewRecord.ID != newest.ID || shown.Record != second {
		t.Fatalf("Expected the key to give round 2 whole, got %d %+v", status, shown.ReviewRecord)
	}
	if len(shown.Rounds) != 2 || shown.Rounds[0].Round != 2 || shown.Rounds[1].Round != 1 || shown.Rounds[1].SessionID != laptopSession {
		t.Fatalf("Expected rounds 2 and 1, newest first, got %+v", shown.Rounds)
	}
	status, earlier := showReviewRecord(t, env, o.project, map[string]any{"id": shown.Rounds[1].ID})
	if status != 200 || earlier.Record != first || earlier.ReviewRecord.Round != 1 || earlier.ReviewRecord.Findings != 200 || len(earlier.Rounds) != 2 {
		t.Errorf("Expected the id of round 1 to give it whole, %d bytes, with its PR's rounds, got %d: %d bytes, %+v",
			len(first), status, len(earlier.Record), earlier.ReviewRecord)
	}

	for name, body := range map[string]map[string]any{
		"a key that no record has": {"key": "https://github.com/acme/shop/pull/8"},
		"an id that no record has": {"id": "6f1e3a5c-2b4d-4f6a-8c0e-3d5b7f9a1c4e"},
		"an id that is not one":    {"id": "round-1"},
	} {
		if status, _ := showReviewRecord(t, env, o.project, body); status != 404 {
			t.Errorf("%s: expected 404, got %d", name, status)
		}
	}
	if status, _ := showReviewRecord(t, env, o.project, map[string]any{"key": pr, "id": newest.ID}); status != 400 {
		t.Errorf("Expected 400 for a key and an id, got %d", status)
	}
	// Another Project's reads find nothing.
	if other := listReviewRecords(t, env, o.project+1000); len(other.ReviewRecords) != 0 {
		t.Errorf("Expected no Review records in another Project, got %+v", other)
	}
}

func TestAReviewRecordParsedAgainAfterACrashIsListedOnce(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl",
		body: jsonl(transcript[0], finalReply(prRecord("https://github.com/acme/shop/pull/7", 1, 4)))}), 200)
	if err := env.AgentFileProcessor.ProcessUntilCaughtUp(); err != nil {
		t.Fatal(err)
	}
	// The parse inserts, and EventStore stops before it moves its cursor.
	crashed := make(chan struct{})
	env.FlakyLogs.failAfterInsert(connectionLost, func() {
		env.FlakyLogs.goDown()
		close(crashed)
	})
	done := make(chan error, 1)
	go func() { done <- env.AgentParser.ProcessUntilCaughtUp() }()
	select {
	case <-crashed:
	case <-time.After(10 * time.Second):
		t.Fatalf("Expected the parse to insert")
	}
	env.Restart(t)
	<-done
	env.FlakyLogs.recover()

	answer := listReviewRecords(t, env, o.project)
	if len(answer.ReviewRecords) != 1 || answer.ReviewRecords[0].Earlier != 0 {
		t.Fatalf("Expected the record once, got %+v", answer.ReviewRecords)
	}
	status, shown := showReviewRecord(t, env, o.project, map[string]any{"key": "https://github.com/acme/shop/pull/7"})
	if status != 200 || len(shown.Rounds) != 1 {
		t.Errorf("Expected one round, got %d %+v", status, shown.Rounds)
	}
}

// Retention and the Agent Project's deletion drop a Review record with the
// day of its Agent session.
func TestTheReviewRecordsFollowTheRetentionAndTheDeletionOfTheAgentProject(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	consoleExec(t, env, "UPDATE projects SET log_retention_days = 2 WHERE id = ?", o.project)
	// With no Project left, the cleanup deletes nothing.
	o.rows.project(o.organization)

	at := func(day int) time.Time { return time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC) }
	for i, day := range []int{1, 4} {
		env.Clock.Set(at(day))
		pr := fmt.Sprintf("https://github.com/acme/shop/pull/%d", day)
		session := []string{laptopSession, reviewSession}[i]
		expectStatus(t, uploadUnit(t, env, o.secret, session, unitUpload{path: "session.jsonl", body: jsonl(finalReply(prRecord(pr, 1, 4)))}), 200)
		env.ProcessAgentFiles(t)
	}
	keys := func() []string {
		var keys []string
		for _, r := range listReviewRecords(t, env, o.project).ReviewRecords {
			keys = append(keys, r.Key)
		}
		return keys
	}
	if got := keys(); len(got) != 2 {
		t.Fatalf("Expected the records of October 1 and 4, got %v", got)
	}

	// Retention of 2 days on October 6th keeps October 4th.
	env.Clock.Set(at(6))
	env.DeleteLogsPastRetention(t)
	if got := keys(); len(got) != 1 || got[0] != "https://github.com/acme/shop/pull/4" {
		t.Errorf("Expected only the record of October 4 after Retention, got %v", got)
	}

	consoleExec(t, env, "DELETE FROM projects WHERE id = ?", o.project)
	env.DeleteLogsPastRetention(t)
	if got := keys(); len(got) != 0 {
		t.Errorf("Expected no record after the Agent Project's deletion, got %v", got)
	}
}
