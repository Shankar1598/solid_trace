package dispatcher

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Run events are Logs in the Organization's Agent Project (see the
// Attribute names in EventStore's storage/runs.go). The store writes each
// one to the outbox table in the same transaction as the state change
// that it records, and the sender loop delivers the outbox to SolidTrace
// with retries. So an event never blocks a state change, and a Run
// changes state while SolidTrace is down.

// Run event kinds. Each later kind is added where its state change is.
const (
	eventCreated          = "created"
	eventQueued           = "queued"
	eventExecutionStarted = "execution_started"
	eventExecutionEnded   = "execution_ended"
	eventReport           = "report"
)

// Trigger kinds of a Run (run.trigger).
const triggerPerson = "person"

// Actor kinds (actor.kind).
const actorPerson = "person"

// runEventsPath is SolidTrace's (EventStore's) route for run events. A
// token with run_events:write sends a JSON array of Logs.
const runEventsPath = "/api/agent/v1/run-events"

// maxTitle is the most runes of a Run's title.
const maxTitle = 100

// title is the Run's title: the first line of its prompt, at most
// maxTitle runes.
func title(prompt string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(prompt), "\n")
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) > maxTitle {
		line = string([]rune(line)[:maxTitle-1]) + "…"
	}
	return line
}

// recordEvent adds the next run event of the Run to the outbox, in tx: a
// snapshot of the Run's list fields after the change, and details, the
// Attributes of the kind. The actor is the person who started the Run,
// unless details name another (an intervention names its actor). An
// event of an Execution (with execution.id) also has the Run's Agent
// session id.
func recordEvent(tx *sql.Tx, at time.Time, runID, kind string, details map[string]any) error {
	var seq int64
	if err := tx.QueryRow(`UPDATE runs SET seq = seq + 1 WHERE id = ? RETURNING seq`, runID).Scan(&seq); err != nil {
		return fmt.Errorf("count the run event: %w", err)
	}
	var state, reason, project, repository, folder, baseBranch, trigger, prompt, actorID, actorName, sessionID string
	var executions int64
	err := tx.QueryRow(`SELECT state, reason, project, repository, folder, base_branch, trigger_kind, prompt,
		started_by_id, started_by_name, session_id, (SELECT COUNT(*) FROM executions e WHERE e.run_id = r.id)
		FROM runs r WHERE id = ?`, runID).Scan(&state, &reason, &project, &repository, &folder, &baseBranch, &trigger,
		&prompt, &actorID, &actorName, &sessionID, &executions)
	if err != nil {
		return fmt.Errorf("read the Run for its event: %w", err)
	}
	event := map[string]any{
		"time":            at.UTC().Format(time.RFC3339Nano),
		"level":           "info",
		"message":         "Run " + runID + ": " + kind,
		"run.id":          runID,
		"run.seq":         seq,
		"event.id":        fmt.Sprintf("%s-%d", runID, seq),
		"event.kind":      kind,
		"run.state":       state,
		"run.reason":      reason,
		"run.trigger":     trigger,
		"run.title":       title(prompt),
		"run.executions":  executions,
		"run.pr.url":      "",
		"run.pr.outcome":  "",
		"run.base_branch": baseBranch,
		"factory.project": project,
		"repository":      repository,
		"folder":          folder,
		"actor.kind":      actorPerson,
		"actor.id":        actorID,
		"actor.name":      actorName,
	}
	if _, ok := details["execution.id"]; ok && sessionID != "" {
		event["agent.session_id"] = sessionID
	}
	for k, v := range details {
		event[k] = v
	}
	return addToOutbox(tx, event)
}

func addToOutbox(tx *sql.Tx, log map[string]any) error {
	body, err := json.Marshal(log)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO outbox (body) VALUES (?)`, body)
	return err
}

// commit commits tx, and wakes the sender: tx can hold run events.
func (s *store) commit(tx *sql.Tx) error {
	if err := tx.Commit(); err != nil {
		return err
	}
	s.wakeSender()
	return nil
}

func (s *store) wakeSender() {
	select {
	case s.sendWake <- struct{}{}:
	default:
	}
}

// addWarning adds one of the Dispatcher's warning Logs to the outbox. It
// has no run.id, so EventStore stores it with file dispatcher.
func (s *store) addWarning(at time.Time, message string, attrs map[string]any) error {
	log := map[string]any{"time": at.UTC().Format(time.RFC3339Nano), "level": "warn", "message": message}
	for k, v := range attrs {
		log[k] = v
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := addToOutbox(tx, log); err != nil {
		return err
	}
	return s.commit(tx)
}

// warn writes a warning to the Dispatcher's log and sends it to the Agent
// Project as a Log with file dispatcher. args are key and value pairs, as
// for slog; each key is the Log's Attribute name.
func (d *Dispatcher) warn(message string, args ...string) {
	attrs := map[string]any{}
	logArgs := make([]any, 0, len(args))
	for i := 0; i+1 < len(args); i += 2 {
		attrs[args[i]] = args[i+1]
		logArgs = append(logArgs, args[i], args[i+1])
	}
	d.log.Warn(message, logArgs...)
	if err := d.store.addWarning(d.now(), message, attrs); err != nil {
		d.log.Error("could not keep a warning for SolidTrace", "error", err)
	}
}

// warnArgs are the Attributes of a warning about p's trigger for the
// Project slug.
func (p person) warnArgs(slug string) []string {
	return []string{"organization", p.organization.slug, "factory.project", slug,
		"actor.kind", actorPerson, "actor.id", p.id, "actor.name", p.name}
}

// The outbox's batches: at most this many Logs and bytes in one request
// (EventStore takes up to 10 MiB).
const (
	maxBatchLogs  = 500
	maxBatchBytes = 1 << 20
)

// The sender's waits after a failed send: from minEventRetry, two times
// longer after each failure, up to Options.EventRetry (maxEventRetry by
// default).
const (
	minEventRetry = 250 * time.Millisecond
	maxEventRetry = 30 * time.Second
)

// outboxBatch is the oldest Logs of the outbox, as one JSON array, and the
// id of the last one. It is empty when the outbox is.
func (s *store) outboxBatch() (body []byte, last int64, err error) {
	rows, err := s.db.Query(`SELECT id, body FROM outbox ORDER BY id LIMIT ?`, maxBatchLogs)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var buf bytes.Buffer
	for rows.Next() {
		var id int64
		var log []byte
		if err := rows.Scan(&id, &log); err != nil {
			return nil, 0, err
		}
		if buf.Len() > 0 && buf.Len()+len(log) > maxBatchBytes {
			break
		}
		if buf.Len() == 0 {
			buf.WriteByte('[')
		} else {
			buf.WriteByte(',')
		}
		buf.Write(log)
		last = id
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if buf.Len() == 0 {
		return nil, 0, nil
	}
	buf.WriteByte(']')
	return buf.Bytes(), last, nil
}

// sent removes the Logs up to last from the outbox.
func (s *store) sent(last int64) error {
	_, err := s.db.Exec(`DELETE FROM outbox WHERE id <= ?`, last)
	return err
}

// sendLoop delivers the outbox to SolidTrace, oldest first, so each Run's
// events arrive in run.seq order. A failure leaves the outbox as it is,
// and the loop tries again later. SolidTrace's ingest is at least once: a
// batch that arrived with no answer arrives again, and readers drop the
// repeats.
func (d *Dispatcher) sendLoop(ctx context.Context) {
	var wait time.Duration
	failing := false
	for {
		err := d.sendOutbox(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			if failing {
				d.log.Info("the run events are sent to SolidTrace again")
			}
			failing, wait = false, 0
			select {
			case <-ctx.Done():
				return
			case <-d.store.sendWake:
			}
			continue
		}
		if !failing {
			d.log.Warn("could not send the run events to SolidTrace: the Dispatcher keeps them and tries again",
				"error", err)
		}
		failing = true
		wait = min(max(2*wait, minEventRetry), d.eventRetry)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// sendOutbox sends the outbox in batches until it is empty.
func (d *Dispatcher) sendOutbox(ctx context.Context) error {
	for {
		body, last, err := d.store.outboxBatch()
		if err != nil || body == nil {
			return err
		}
		if err := d.sendBatch(ctx, body); err != nil {
			return err
		}
		if err := d.store.sent(last); err != nil {
			return err
		}
	}
}

func (d *Dispatcher) sendBatch(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.config.SolidTraceURL+runEventsPath,
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.config.SolidTraceToken)
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("SolidTrace answered %d: %s", resp.StatusCode, strings.TrimSpace(string(answer)))
	}
	var counts struct {
		Dropped int `json:"dropped"`
	}
	if json.Unmarshal(answer, &counts) == nil && counts.Dropped > 0 {
		d.log.Warn("SolidTrace dropped run events", "dropped", counts.Dropped)
	}
	return nil
}
