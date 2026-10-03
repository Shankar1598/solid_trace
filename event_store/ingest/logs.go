package ingest

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/ingest/normalise"
	"github.com/solidtrace/event_store/models"
)

// LogCounts tells a sender what became of the Logs it sent.
type LogCounts struct {
	// Accepted Logs are stored.
	Accepted int `json:"accepted"`
	// Dropped Logs are not, and never will be: an element or line that isn't
	// a JSON object, or a Log over 1 MB even after the per-Log limits.
	Dropped int `json:"dropped"`
	// Truncated Logs are accepted ones that a per-Log limit cut. Each carries
	// solidtrace.truncated = true.
	Truncated int `json:"truncated"`
}

// IngestLogs ingests the body of the JSON Log route: a JSON array of objects,
// one object, or NDJSON. Each object is one Log. An element or line that isn't
// a JSON object is dropped and counted. The rest are normalised, against the
// receipt time and the Project's Retention, and stored as one Log batch, and
// Log processing is woken.
//
// A body that isn't one JSON value is read as NDJSON whatever its content
// type, so a sender that labels NDJSON as JSON still gets its Logs stored.
func (s *Service) IngestLogs(projectID uint32, body Body, ndjson bool) (LogCounts, error) {
	if !s.enterGate() {
		return LogCounts{}, ErrOverloaded
	}
	defer s.leaveGate()

	raw, err := body()
	if err != nil {
		return LogCounts{}, err
	}
	objects, dropped := logObjects(raw, ndjson)
	return s.storeLogs(projectID, objects, dropped, normalise.JSON)
}

// storeLogs normalises one request's Logs with normaliseLog, against the
// receipt time and the Project's Retention, stores the ones kept as one Log
// batch, and wakes Log processing. dropped counts what the request held that
// wasn't a Log at all. The caller is in the gate.
func (s *Service) storeLogs(
	projectID uint32,
	objects []map[string]json.RawMessage,
	dropped int,
	normaliseLog func(map[string]json.RawMessage, normalise.Receipt) (models.Log, normalise.Outcome),
) (LogCounts, error) {
	retentionDays, err := s.retention.LogRetentionDays(projectID)
	if err != nil {
		return LogCounts{}, fmt.Errorf("read retention: %w", err)
	}
	receipt := normalise.Receipt{At: s.clock.Now().UTC(), RetentionDays: retentionDays}

	counts := LogCounts{Dropped: dropped}
	logs := make([]models.Log, 0, len(objects))
	for _, object := range objects {
		log, outcome := normaliseLog(object, receipt)
		switch outcome {
		case normalise.Dropped:
			counts.Dropped++
			continue
		case normalise.Truncated:
			counts.Truncated++
		}
		id, err := uuid.NewV7()
		if err != nil {
			return LogCounts{}, fmt.Errorf("generate log id: %w", err)
		}
		log.ID = id
		logs = append(logs, log)
	}

	counts.Accepted = len(logs)
	if len(logs) == 0 {
		return counts, nil
	}
	// A Pebble write is never retried: the sender gets an error instead.
	if err := s.logs.WriteLogBatch(projectID, logs, receipt.At); err != nil {
		return LogCounts{}, fmt.Errorf("write log batch: %w", err)
	}
	s.notifyLogs(projectID)
	return counts, nil
}

// sentryLogRecords returns the log records held by the payloads of an
// envelope's log items, and counts the records that aren't JSON objects. A
// payload must be a JSON object with an "items" array of records; one that
// isn't holds no records that can be read.
// See https://develop.sentry.dev/sdk/telemetry/logs/#log-envelope-item-payload
func sentryLogRecords(payloads [][]byte) (records []map[string]json.RawMessage, dropped int) {
	for _, payload := range payloads {
		var container struct {
			Items []json.RawMessage `json:"items"`
		}
		if json.Unmarshal(payload, &container) != nil {
			continue
		}
		for _, item := range container.Items {
			if record, ok := jsonObject(item); ok {
				records = append(records, record)
			} else {
				dropped++
			}
		}
	}
	return records, dropped
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
