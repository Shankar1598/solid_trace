package agentprocessing

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/event_store/storage"
)

// The Review record of a PR (ADR 0024): the open-pr and update-pr skills
// end their final reply with the record in a solidtrace-pr-record block.
// The parse keeps the last such block of an assistant line of the main
// transcript, when it is a record of version 2 in valid JSON, all of it,
// with the place of its line and the counts that the Review records page
// lists.

// reviewRecord is a record of version 2 with four findings, one by each
// kind of finder, and three confirmations: given, not given, and with no
// answer.
const reviewRecord = `{"version":2,"sessions":[{"id":"s1","title":"Cart page & <checkout>"}],"pr":"https://github.com/acme/shop/pull/7","round":2,` +
	`"story":{"started_from":"Add a cart page…"},"reviews":[],` +
	`"findings":[{"id":"1.F1","found_by":{"kind":"reviewer","name":"tests"},"outcome":"fixed"},` +
	`{"id":"P1","found_by":{"kind":"person"},"outcome":"fixed"},` +
	`{"id":"A1","found_by":{"kind":"agent","name":"verifier"},"outcome":"deferred"},` +
	`{"id":"X1","found_by":{"kind":"test"},"outcome":"open"}],` +
	`"confirmations":[{"id":"1.C1","given":true},{"id":"1.C2","given":false},{"id":"1.C3","given":null}]}`

func reply(texts ...string) string {
	content := make([]map[string]string, len(texts))
	for i, t := range texts {
		content[i] = map[string]string{"type": "text", "text": t}
	}
	line, _ := json.Marshal(map[string]any{
		"type": "assistant", "uuid": "a1", "timestamp": "2026-11-20T10:01:00Z",
		"message": map[string]any{"id": "msg_1", "role": "assistant", "model": "claude-opus-5-5", "content": content},
	})
	return string(line)
}

func finalReply(record string) string {
	return reply("The PR is https://github.com/acme/shop/pull/7.\n\n```solidtrace-pr-record\n" + record + "\n```\n")
}

// recordsOf parses the units as one range and gives their Review records.
func recordsOf(units ...storage.AgentFileUnit) []storage.ReviewRecord {
	_, _, records, _ := parseUnits(units, 30, parseState{})
	return records
}

func TestAFinalReplyGivesTheReviewRecordOfItsLine(t *testing.T) {
	lines := []string{person("Open the PR"), finalReply(reviewRecord)}
	unit := unitOf("session.jsonl", 4096, lines...)
	logs, _, records, _ := parseUnits([]storage.AgentFileUnit{unit}, 30, parseState{})
	if len(records) != 1 {
		t.Fatalf("Expected one Review record, got %+v", records)
	}
	r := records[0]
	want := storage.ReviewRecord{
		ID: logs[0][1].ID, SessionID: titleSession, Path: "session.jsonl", Version: 1, Offset: 4096 + uint64(len(lines[0])) + 1,
		StoredAt: time.Date(2026, 11, 20, 10, 5, 0, 0, time.UTC), PR: "https://github.com/acme/shop/pull/7", Round: 2,
		Title:   "Cart page & <checkout>",
		Summary: storage.ReviewSummary{Findings: 4, Fixed: 2, Missed: 3, NotGiven: 2},
		Record:  reviewRecord,
	}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("Expected %+v, got %+v", want, r)
	}
}

func TestOnlyTheLastBlockOfAnAssistantLineOfTheMainTranscriptIsARecord(t *testing.T) {
	noPR := strings.Replace(reviewRecord, `"pr":"https://github.com/acme/shop/pull/7"`, `"pr":null`, 1)
	toolUse, _ := json.Marshal(map[string]any{
		"type": "assistant", "uuid": "a2", "timestamp": "2026-11-20T10:01:00Z",
		"message": map[string]any{"id": "msg_2", "role": "assistant", "model": "claude-opus-5-5", "content": []map[string]any{
			{"type": "tool_use", "id": "toolu_1", "name": "Bash", "input": map[string]string{"command": "cat <<'EOF'\n```solidtrace-pr-record\n" + reviewRecord + "\n```\nEOF"}},
		}},
	})
	toolResult, _ := json.Marshal(map[string]any{
		"type": "user", "uuid": "u2", "timestamp": "2026-11-20T10:01:01Z",
		"message": map[string]any{"role": "user", "content": []map[string]any{
			{"type": "tool_result", "tool_use_id": "toolu_1", "content": "```solidtrace-pr-record\n" + reviewRecord + "\n```"},
		}},
	})
	sidechain := strings.Replace(finalReply(reviewRecord), `"type":"assistant"`, `"type":"assistant","isSidechain":true`, 1)
	cases := []struct {
		name  string
		path  string
		lines []string
		want  *storage.ReviewSummary
		pr    string
	}{
		{"the skill's own text, with its example block, is a user line", "session.jsonl",
			[]string{person("End the reply with the record:\n\n```solidtrace-pr-record\n{\"version\": 2, ...}\n```")}, nil, ""},
		{"a person who pastes a whole record is not the agent's reply", "session.jsonl", []string{person("```solidtrace-pr-record\n" + reviewRecord + "\n```")}, nil, ""},
		{"the agent quotes the skill's example", "session.jsonl", []string{finalReply(`{"version": 2, ...}`)}, nil, ""},
		{"a record that is not valid JSON", "session.jsonl", []string{finalReply(reviewRecord[:len(reviewRecord)-1])}, nil, ""},
		{"a record of another version", "session.jsonl", []string{finalReply(strings.Replace(reviewRecord, `"version":2`, `"version":1`, 1))}, nil, ""},
		{"JSON that is not a record", "session.jsonl", []string{finalReply(`[2]`)}, nil, ""},
		{"a subagent's reply", "subagents/agent-a1.jsonl", []string{finalReply(reviewRecord)}, nil, ""},
		{"a sidechain reply", "session.jsonl", []string{sidechain}, nil, ""},
		{"a tool call and its result", "session.jsonl", []string{string(toolUse), string(toolResult)}, nil, ""},
		{"a fence in the middle of a line opens no block", "session.jsonl", []string{reply("See the ```solidtrace-pr-record\n" + reviewRecord + "\n```")}, nil, ""},
		{"the last block decides, also when an earlier one is a record", "session.jsonl",
			[]string{reply("```solidtrace-pr-record\n"+reviewRecord+"\n```\n", "Again:\n```solidtrace-pr-record\n{\"version\": 2, ...}\n```")}, nil, ""},
		{"the last block of the last text block that has one", "session.jsonl",
			[]string{reply("```solidtrace-pr-record\n{\"version\": 2, ...}\n```", "Done. The record:\n```solidtrace-pr-record\n"+reviewRecord+"\n```", "No PR link.")},
			&storage.ReviewSummary{Findings: 4, Fixed: 2, Missed: 3, NotGiven: 2}, "https://github.com/acme/shop/pull/7"},
		{"a block with no end, with a record with no PR", "session.jsonl", []string{reply("```solidtrace-pr-record\n" + noPR + "\n")},
			&storage.ReviewSummary{Findings: 4, Fixed: 2, Missed: 3, NotGiven: 2}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			records := recordsOf(unitOf(c.path, 0, c.lines...))
			if c.want == nil {
				if len(records) != 0 {
					t.Fatalf("Expected no Review record, got %+v", records)
				}
				return
			}
			if len(records) != 1 || records[0].Summary != *c.want || records[0].PR != c.pr {
				t.Fatalf("Expected one Review record with %+v and the PR %q, got %+v", *c.want, c.pr, records)
			}
		})
	}
}

func TestEachFinalReplyOfAnAgentSessionIsARecordAndAParseAgainGivesTheSameRows(t *testing.T) {
	second := strings.Replace(reviewRecord, `"round":2`, `"round":3`, 1)
	lines := []string{person("Open the PR"), finalReply(reviewRecord), person("Update the PR"), finalReply(second)}
	unit := unitOf("session.jsonl", 0, lines...)

	records := recordsOf(unit)
	if len(records) != 2 || records[0].Round != 2 || records[1].Round != 3 || records[0].Record != reviewRecord || records[1].Record != second {
		t.Fatalf("Expected the records of rounds 2 and 3, in the order of their lines, got %+v", records)
	}
	if records[0].Offset == records[1].Offset || records[0].ID == records[1].ID {
		t.Errorf("Expected each record to have the place of its own line, got %+v", records)
	}
	if again := recordsOf(unit); !reflect.DeepEqual(again, records) {
		t.Errorf("Expected a parse again to give the same rows, %+v, got %+v", records, again)
	}
}
