package agentprocessing

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/storage"
)

// The title of an Agent session (agent_sessions.title): the last
// custom-title of the main transcript, else its last ai-title, else the
// start of its first person message. A subagent's transcript gives none.

var titleSession = uuid.MustParse("7d3e9a1c-4b2f-4c6d-8e0a-1f3b5d7c9e2a")

func person(text string) string {
	return `{"type":"user","uuid":"p","timestamp":"2026-11-20T10:00:00Z","message":{"role":"user","content":` + string(quote(text)) + `}}`
}

func aiTitle(title string) string {
	return `{"type":"ai-title","aiTitle":` + string(quote(title)) + `,"sessionId":"x"}`
}

func customTitle(title string) string {
	return `{"type":"custom-title","customTitle":` + string(quote(title)) + `,"sessionId":"x"}`
}

func unitOf(path string, offset uint64, lines ...string) storage.AgentFileUnit {
	return storage.AgentFileUnit{
		SessionID: titleSession, Path: path, Version: 1, Offset: offset,
		Bytes:    []byte(strings.Join(lines, "\n") + "\n"),
		StoredAt: time.Date(2026, 11, 20, 10, 5, 0, 0, time.UTC),
		Session:  storage.AgentSession{Harness: "claude-code", Repository: "github.com/acme/shop"},
	}
}

// titleOf parses the ranges in order, and gives the title of the file at
// path as ClickHouse merges its rows: the max of their titles.
func titleOf(t *testing.T, path string, ranges ...[]storage.AgentFileUnit) storage.AgentTitle {
	t.Helper()
	var title storage.AgentTitle
	var state parseState
	for _, units := range ranges {
		var days []storage.AgentSessionDay
		_, days, state = parseUnits(units, 30, state)
		for _, d := range days {
			if d.Path == path && d.Title.After(title) {
				title = d.Title
			}
		}
	}
	return title
}

func TestTheTitleOfAnAgentSession(t *testing.T) {
	const main = "session.jsonl"
	cases := []struct {
		name  string
		lines []string
		want  string
	}{
		{"a custom-title wins over an ai-title", []string{person("Fix the cart"), customTitle("Cart fix"), aiTitle("Fix the cart page")}, "Cart fix"},
		{"a later ai-title wins over an earlier one", []string{person("Fix the cart"), aiTitle("Cart"), aiTitle("Cart page and checkout")}, "Cart page and checkout"},
		{"a later rename wins over an earlier one", []string{customTitle("First name"), aiTitle("Cart"), customTitle("Second name")}, "Second name"},
		{"a rename after an ai-title wins", []string{aiTitle("Cart"), customTitle("Factory build")}, "Factory build"},
		{"an ai-title after a rename does not win", []string{customTitle("Factory build"), aiTitle("Cart")}, "Factory build"},
		{"with no title, the first person message", []string{person("Fix the cart"), person("And the checkout")}, "Fix the cart"},
		{"the first person message on one line", []string{person("Fix the cart\n\nThe total is wrong   when a coupon is used")}, "Fix the cart The total is wrong when a coupon is used"},
		{"a slash command as the page shows it", []string{
			person("<command-message>review</command-message>\n<command-name>/review</command-name>\n<command-args>the cart PR</command-args>"),
		}, "/review the cart PR"},
		{"the output of a local command is not a person message", []string{
			person("<local-command-stdout>Kept model as Opus</local-command-stdout>"), person("Fix the cart"),
		}, "Fix the cart"},
		{"a message that is not from a person is not a title", []string{
			`{"type":"user","isMeta":true,"timestamp":"2026-11-20T10:00:00Z","message":{"role":"user","content":"Caveat: the messages below"}}`,
			person("Fix the cart"),
		}, "Fix the cart"},
		{"no title and no person message", []string{`{"type":"system","subtype":"x","content":"Started"}`}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := titleOf(t, main, []storage.AgentFileUnit{unitOf(main, 0, c.lines...)}).Text; got != c.want {
				t.Errorf("Expected the title %q, got %q", c.want, got)
			}
		})
	}

	t.Run("a long first person message is cut", func(t *testing.T) {
		got := titleOf(t, main, []storage.AgentFileUnit{unitOf(main, 0, person(strings.Repeat("word ", 40)))}).Text
		if want := strings.TrimSpace(strings.Repeat("word ", 16)) + "…"; got != want {
			t.Errorf("Expected the title %q, got %q", want, got)
		}
	})

	t.Run("the order goes on in a later range of the upload", func(t *testing.T) {
		first := unitOf(main, 0, person("Fix the cart"), customTitle("Cart fix"), aiTitle("Cart"))
		second := unitOf(main, uint64(len(first.Bytes)), aiTitle("Cart and checkout"), customTitle("Checkout"))
		if got := titleOf(t, main, []storage.AgentFileUnit{first}, []storage.AgentFileUnit{second}).Text; got != "Checkout" {
			t.Errorf("Expected the rename of the second range, got %q", got)
		}
		onlyAI := unitOf(main, uint64(len(first.Bytes)), aiTitle("Cart and checkout"))
		start := unitOf(main, 0, person("Fix the cart"), aiTitle("Cart"))
		onlyAI.Offset = uint64(len(start.Bytes))
		if got := titleOf(t, main, []storage.AgentFileUnit{start}, []storage.AgentFileUnit{onlyAI}).Text; got != "Cart and checkout" {
			t.Errorf("Expected the ai-title of the second range, got %q", got)
		}
		message := unitOf(main, uint64(len(start.Bytes)), person("And the checkout"))
		if got := titleOf(t, main, []storage.AgentFileUnit{unitOf(main, 0, person("Fix the cart"))}, []storage.AgentFileUnit{message}).Text; got != "Fix the cart" {
			t.Errorf("Expected the first person message of the first range, got %q", got)
		}
	})

	t.Run("a range without the start of the transcript gives no first person message", func(t *testing.T) {
		// As for an Agent session parsed before titles, which continues:
		// its first person message is in a range parsed before.
		later := unitOf(main, 4096, person("And the docs"))
		if got := titleOf(t, main, []storage.AgentFileUnit{later}); got != (storage.AgentTitle{}) {
			t.Errorf("Expected no title, got %+v", got)
		}
		later = unitOf(main, 4096, person("And the docs"), aiTitle("Docs"))
		if got := titleOf(t, main, []storage.AgentFileUnit{later}).Text; got != "Docs" {
			t.Errorf("Expected the ai-title, got %q", got)
		}
	})

	t.Run("a subagent's transcript gives no title", func(t *testing.T) {
		const subagent = "subagents/agent-a1.jsonl"
		units := []storage.AgentFileUnit{
			unitOf(main, 0, person("Fix the cart"), aiTitle("Cart")),
			unitOf(subagent, 0, person("Read the cart"), customTitle("Subagent name"), aiTitle("Reading the cart")),
		}
		if got := titleOf(t, main, units).Text; got != "Cart" {
			t.Errorf("Expected the main transcript's title, got %q", got)
		}
		if got := titleOf(t, subagent, units); got != (storage.AgentTitle{}) {
			t.Errorf("Expected no title for the subagent's transcript, got %+v", got)
		}
	})

	t.Run("a live Agent session uploaded in many small parts keeps its last ai-title", func(t *testing.T) {
		// As Claude Code writes a live Agent session: each turn is a person
		// message, then lines with no timestamp (last-prompt, ai-title, mode,
		// permission-mode), then the agent's reply. The hooks and the sweep
		// upload it in parts that start and end anywhere between lines: a
		// part can end on an ai-title, or hold only untimed lines. The parts
		// are on two stored days, and a subagent's parts come between them.
		var lines []string
		for turn := 1; turn <= 14; turn++ {
			at := time.Date(2026, 11, 22, 23, 40+turn, 0, 0, time.UTC).Format(time.RFC3339)
			lines = append(lines,
				`{"type":"user","uuid":"p","timestamp":"`+at+`","message":{"role":"user","content":"Turn `+fmt.Sprint(turn)+`"}}`,
				`{"type":"last-prompt","lastPrompt":"Turn `+fmt.Sprint(turn)+`","sessionId":"x"}`,
				aiTitle(fmt.Sprintf("Title %d", turn)),
				`{"type":"mode","mode":"default","sessionId":"x"}`,
				`{"type":"permission-mode","permissionMode":"default","sessionId":"x"}`,
				`{"type":"assistant","uuid":"a","timestamp":"`+at+`","message":{"id":"m`+fmt.Sprint(turn)+`","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Done."}],"usage":{"input_tokens":1,"output_tokens":1}}}`,
			)
		}
		const subagent = "subagents/agent-a1.jsonl"
		for _, size := range []int{1, 2, 3, 5, 7} {
			var ranges [][]storage.AgentFileUnit
			var offset, subOffset uint64
			for start, part := 0, 0; start < len(lines); start, part = start+size, part+1 {
				unit := unitOf(main, offset, lines[start:min(start+size, len(lines))]...)
				// The first parts on one day, the others on the next.
				unit.StoredAt = time.Date(2026, 11, 22, 23, 50, 0, 0, time.UTC).Add(time.Duration(part) * 3 * time.Minute)
				offset += uint64(len(unit.Bytes))
				sub := unitOf(subagent, subOffset, person("Read the cart"), aiTitle("Reading the cart"))
				sub.StoredAt = unit.StoredAt
				subOffset += uint64(len(sub.Bytes))
				ranges = append(ranges, []storage.AgentFileUnit{sub, unit})
			}
			if got := titleOf(t, main, ranges...).Text; got != "Title 14" {
				t.Errorf("Parts of %d lines, a range each: expected the last ai-title, got %q", size, got)
			}
			var one []storage.AgentFileUnit
			for _, r := range ranges {
				one = append(one, r...)
			}
			if got := titleOf(t, main, one).Text; got != "Title 14" {
				t.Errorf("Parts of %d lines in one range: expected the last ai-title, got %q", size, got)
			}
		}
	})

	t.Run("a new version of the transcript has the title of its own lines", func(t *testing.T) {
		old := unitOf(main, 0, person("Fix the cart"), aiTitle("Cart"))
		trimmed := unitOf(main, 0, person("Fix the cart"), aiTitle("Cart and checkout"))
		trimmed.Version = 2
		_, days, _ := parseUnits([]storage.AgentFileUnit{old, trimmed}, 30, parseState{})
		for _, d := range days {
			if d.Version == 2 && d.Title.Text != "Cart and checkout" {
				t.Errorf("Expected the title of version 2, got %+v", d.Title)
			}
		}
	})

	t.Run("the custom-title's text is the Log's message", func(t *testing.T) {
		logs, _, _ := parseUnits([]storage.AgentFileUnit{unitOf(main, 0, customTitle("Factory build"))}, 30, parseState{})
		if len(logs[0]) != 1 || logs[0][0].Message != "Factory build" {
			t.Errorf("Expected one Log with the message %q, got %+v", "Factory build", logs[0])
		}
	})
}
