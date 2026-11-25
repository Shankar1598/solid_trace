package agentprocessing

import (
	"encoding/json"
	"strings"

	"github.com/solidtrace/event_store/storage"
)

// reviewRecordFence opens the block with which the open-pr and update-pr
// skills end their final reply: the Review record of the PR, as one line
// of JSON (cli/plugin-skills/skills/open-pr/SKILL.md).
const reviewRecordFence = "```solidtrace-pr-record"

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

// reviewRecordOf is the Review record of the last solidtrace-pr-record
// block in texts, the text blocks of an assistant line in order, with no
// place yet; nil when there is no block, or when the last one is not a
// record of version 2 in valid JSON.
func reviewRecordOf(texts []string) *storage.ReviewRecord {
	for i := len(texts) - 1; i >= 0; i-- {
		body, ok := lastReviewRecordBlock(texts[i])
		if !ok {
			continue
		}
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
	return nil
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
