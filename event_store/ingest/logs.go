package ingest

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
)

// LogCounts tells a sender what became of the Logs it sent.
type LogCounts struct {
	// Accepted Logs are stored.
	Accepted int `json:"accepted"`
	// Dropped Logs are not, and never will be: an element or line that isn't
	// a JSON object.
	Dropped int `json:"dropped"`
}

// IngestLogs ingests the body of the JSON Log route: a JSON array of objects,
// one object, or NDJSON. Each object is one Log. An element or line that isn't
// a JSON object is dropped and counted. The rest are normalised and stored as
// one Log batch, and Log processing is woken.
//
// A body that isn't one JSON value is read as NDJSON whatever its content
// type, so a sender that labels NDJSON as JSON still gets its Logs stored.
func (s *Service) IngestLogs(projectID uint32, body []byte, ndjson bool) (LogCounts, error) {
	if !s.enterGate() {
		return LogCounts{}, ErrOverloaded
	}
	defer s.leaveGate()

	objects, dropped := logObjects(body, ndjson)
	receivedAt := s.clock.Now().UTC()
	logs := make([]models.Log, 0, len(objects))
	for _, object := range objects {
		log := normaliseLog(object, receivedAt)
		id, err := uuid.NewV7()
		if err != nil {
			return LogCounts{}, fmt.Errorf("generate log id: %w", err)
		}
		log.ID = id
		logs = append(logs, log)
	}

	counts := LogCounts{Accepted: len(logs), Dropped: dropped}
	if len(logs) == 0 {
		return counts, nil
	}
	// A Pebble write is never retried: the sender gets an error instead.
	if err := s.logs.WriteLogBatch(projectID, logs); err != nil {
		return LogCounts{}, fmt.Errorf("write log batch: %w", err)
	}
	s.notifyLogs(projectID)
	return counts, nil
}

// logObjects splits a body into the JSON objects it holds, and counts the
// elements or lines that aren't one.
func logObjects(body []byte, ndjson bool) (objects []map[string]json.RawMessage, dropped int) {
	keep := func(raw []byte) {
		if object, ok := jsonObject(raw); ok {
			objects = append(objects, object)
		} else {
			dropped++
		}
	}

	if trimmed := bytes.TrimSpace(body); !ndjson && json.Valid(trimmed) {
		if trimmed[0] != '[' {
			keep(trimmed)
			return objects, dropped
		}
		var elements []json.RawMessage
		if err := json.Unmarshal(trimmed, &elements); err != nil {
			// Unreachable: valid JSON that starts with '[' is an array.
			return nil, 1
		}
		for _, element := range elements {
			keep(element)
		}
		return objects, dropped
	}

	for line := range bytes.Lines(body) {
		if line = bytes.TrimSpace(line); len(line) > 0 {
			keep(line)
		}
	}
	return objects, dropped
}

// jsonObject parses raw as a JSON object, with its values left as JSON text.
func jsonObject(raw []byte) (map[string]json.RawMessage, bool) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		// A JSON null unmarshals to a nil map without an error.
		return nil, false
	}
	return object, true
}
