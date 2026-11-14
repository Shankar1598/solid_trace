package agentprocessing

import (
	"encoding/json"
	"fmt"
	"maps"
	"time"
)

// parseState is what the parse keeps of a Project from one range to the
// next: for each .jsonl file, by Agent session, version and path, its last
// reply and the tokens that its Logs have had. Pebble keeps it with the
// parse cursor, so a range parsed again after a crash reads the same state
// and makes the same Logs. The JSON names are the stored format.
type parseState struct {
	Replies map[string]openReply `json:"replies,omitempty"`
}

type openReply struct {
	MessageID  string    `json:"message_id"`
	Input      int64     `json:"input"`
	Output     int64     `json:"output"`
	CacheRead  int64     `json:"cache_read"`
	CacheWrite int64     `json:"cache_write"`
	Cost       float64   `json:"cost"`
	HasCost    bool      `json:"has_cost"`
	StoredAt   time.Time `json:"stored_at"`
}

// replyKeptFor is how long the state keeps the last reply of a file after
// the server stored its unit. The lines of one reply come seconds apart.
const replyKeptFor = 24 * time.Hour

func decodeParseState(encoded []byte) (parseState, error) {
	var s parseState
	if len(encoded) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(encoded, &s); err != nil {
		return s, fmt.Errorf("decode the parse state: %w", err)
	}
	return s, nil
}

// encode is the stored state, nil when it is empty.
func (s parseState) encode() ([]byte, error) {
	if len(s.Replies) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("encode the parse state: %w", err)
	}
	return encoded, nil
}

func (s parseState) clone() parseState {
	return parseState{Replies: maps.Clone(s.Replies)}
}

// addReply adds to the Log of the last line in the range of a reply the
// tokens and the cost that the reply's Logs did not have yet, and keeps
// the reply as the file's last reply. A reply that has had all its tokens
// gets none on this line.
func (s *parseState) addReply(object map[string]json.RawMessage, file string, f lineFacts, storedAt time.Time) {
	t := f.usage.tokens()
	cost, hasCost := estimatedCost(f.model, t)
	reply := openReply{
		MessageID: f.messageID, Input: t.input, Output: t.output, CacheRead: t.cacheRead,
		CacheWrite: t.cacheWrite5m + t.cacheWrite1h, Cost: cost, HasCost: hasCost, StoredAt: storedAt.UTC(),
	}
	if s.Replies == nil {
		s.Replies = map[string]openReply{}
	}
	before, seen := s.Replies[file]
	s.Replies[file] = reply
	if !seen || before.MessageID != f.messageID {
		addTokens(object, t, cost, hasCost)
		return
	}
	more := tokens{
		input: reply.Input - before.Input, output: reply.Output - before.Output,
		cacheRead: reply.CacheRead - before.CacheRead, cacheWrite5m: reply.CacheWrite - before.CacheWrite,
	}
	moreCost := reply.Cost - before.Cost
	if more == (tokens{}) && moreCost == 0 {
		return
	}
	addTokens(object, more, moreCost, hasCost)
}

// dropOld drops the replies of files whose last unit the server stored
// more than replyKeptFor before the newest one in the state.
func (s *parseState) dropOld() {
	var newest time.Time
	for _, r := range s.Replies {
		if r.StoredAt.After(newest) {
			newest = r.StoredAt
		}
	}
	for file, r := range s.Replies {
		if newest.Sub(r.StoredAt) > replyKeptFor {
			delete(s.Replies, file)
		}
	}
}
