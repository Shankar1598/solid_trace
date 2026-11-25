package agentprocessing

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/solidtrace/event_store/storage"
)

// reviewRecordFence opens the block with which the open-pr and update-pr
// skills ended their final reply before they sent the Review record of the
// PR to solidtrace review final. Sessions of that time keep their records.
const reviewRecordFence = "```solidtrace-pr-record"

// reviewFinalCall is the first line of a Bash command that runs only
// solidtrace review final, by its name or by a path, with its stdin from a
// heredoc whose word is in quotes, so that the shell does not change the
// body (cli/plugin-skills/skills/open-pr/SKILL.md).
var reviewFinalCall = regexp.MustCompile(`^[ \t]*(?:[\w.~/-]*/)?solidtrace[ \t]+review[ \t]+final[ \t]*<<[ \t]*(['"])(\w+)(['"])[ \t]*$`)

// reviewRecordVersion is the version of the record (cli/review.Record) that
// the parse reads.
const reviewRecordVersion = 2

// reviewRecordJSON is the part of a Review record that its summary reads.
type reviewRecordJSON struct {
	Version  int     `json:"version"`
	PR       *string `json:"pr"`
	Round    uint32  `json:"round"`
	Sessions []struct {
		Title string `json:"title"`
	} `json:"sessions"`
	Findings []struct {
		Outcome string `json:"outcome"`
		FoundBy struct {
			Kind string `json:"kind"`
		} `json:"found_by"`
	} `json:"findings"`
	Confirmations []struct {
		Given *bool `json:"given"`
	} `json:"confirmations"`
}

// missedBy are the found_by kinds of a finding that the reviews missed
// (open-pr's "Missed by the review, found by the person", "by an agent",
// "by a test run").
var missedBy = map[string]bool{"person": true, "agent": true, "test": true}

// reviewRecordOf is the Review record in body, with no place yet; nil
// when body is not a record of version 2 in valid JSON.
func reviewRecordOf(body string) *storage.ReviewRecord {
	var parsed reviewRecordJSON
	if json.Unmarshal([]byte(body), &parsed) != nil || parsed.Version != reviewRecordVersion {
		return nil
	}
	r := &storage.ReviewRecord{Round: parsed.Round, Record: body}
	if parsed.PR != nil {
		r.PR = *parsed.PR
	}
	if len(parsed.Sessions) > 0 {
		r.Title = parsed.Sessions[0].Title
	}
	r.Summary.Findings = uint32(len(parsed.Findings))
	for _, f := range parsed.Findings {
		if f.Outcome == "fixed" {
			r.Summary.Fixed++
		}
		if missedBy[f.FoundBy.Kind] {
			r.Summary.Missed++
		}
	}
	for _, c := range parsed.Confirmations {
		if c.Given == nil || !*c.Given {
			r.Summary.NotGiven++
		}
	}
	return r
}

// lastReviewRecordBlock is the text in the last block of text that a line
// with only the fence opens, up to the line that closes it or the end of
// the text, trimmed.
func lastReviewRecordBlock(text string) (string, bool) {
	for end := len(text); ; {
		start := strings.LastIndex(text[:end], reviewRecordFence)
		if start < 0 {
			return "", false
		}
		end = start
		if start > 0 && text[start-1] != '\n' {
			continue
		}
		rest := text[start+len(reviewRecordFence):]
		info, body, found := strings.Cut(rest, "\n")
		if !found || strings.TrimSpace(info) != "" {
			continue
		}
		body, _, _ = strings.Cut(body, "\n```")
		return strings.TrimSpace(body), true
	}
}

// reviewFinalBody is the heredoc of input, the input of a Bash tool use,
// when its command is one call of solidtrace review final and nothing
// more: blank or comment lines, the line of reviewFinalCall, the body, the
// line with only the heredoc's word, and blank lines.
func reviewFinalBody(input json.RawMessage) (string, bool) {
	var bash struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(input, &bash) != nil {
		return "", false
	}
	lines := strings.Split(bash.Command, "\n")
	for len(lines) > 0 && blankOrComment(lines[0]) {
		lines = lines[1:]
	}
	if len(lines) == 0 {
		return "", false
	}
	m := reviewFinalCall.FindStringSubmatch(lines[0])
	if m == nil {
		return "", false
	}
	openQuote, word, closeQuote := m[1], m[2], m[3]
	if openQuote != closeQuote {
		return "", false
	}
	body := lines[1:]
	for i, l := range body {
		if l == word {
			for _, after := range body[i+1:] {
				if strings.TrimSpace(after) != "" {
					return "", false
				}
			}
			return strings.TrimSpace(strings.Join(body[:i], "\n")), true
		}
	}
	return "", false
}

// blankOrComment is true for a line that bash runs as nothing.
func blankOrComment(line string) bool {
	line = strings.TrimSpace(line)
	return line == "" || strings.HasPrefix(line, "#")
}
