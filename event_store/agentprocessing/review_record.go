package agentprocessing

import (
	"encoding/json"
	"strings"

	"github.com/solidtrace/event_store/storage"
)

// reviewRecordFence opens the block with which the open-pr and update-pr
// skills ended their final reply before solidtrace review final wrote the
// Review record of the PR to a file. Sessions of that time keep their
// records.
const reviewRecordFence = "```solidtrace-pr-record"

// reviewRecordFolder is the folder of an Agent session's own folder where
// solidtrace review final writes the session's Review records
// (cli/command/review_final.go).
const reviewRecordFolder = "/solidtrace/review-records/"

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

// reviewRecordOfFile is the Review record of unit when it is a whole file
// <session id>/solidtrace/review-records/<name>.json of its own Agent
// session, with the place of the file at offset 0; nil for another unit, a
// cut file, or a file that is not a record. A subagent's file is under
// subagents/, so it is not in that folder.
func reviewRecordOfFile(unit storage.AgentFileUnit) *storage.ReviewRecord {
	name, ok := strings.CutPrefix(unit.Path, unit.SessionID.String()+reviewRecordFolder)
	if !ok || !strings.HasSuffix(name, ".json") || strings.Contains(name, "/") || unit.Size > uint64(len(unit.Bytes)) {
		return nil
	}
	r := reviewRecordOf(string(unit.Bytes))
	if r != nil {
		placeRecord(r, unit, 0)
	}
	return r
}

// placeRecord gives r the place of its record: offset in unit.
func placeRecord(r *storage.ReviewRecord, unit storage.AgentFileUnit, offset uint64) {
	r.ID, r.SessionID, r.Path, r.Version, r.Offset, r.StoredAt = placeID(unit, offset), unit.SessionID, unit.Path, unit.Version, offset, unit.StoredAt.UTC()
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
