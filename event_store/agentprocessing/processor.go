// Package agentprocessing moves the units of Agent sessions' files that the
// agent ingest API stored in Pebble into ClickHouse (ADR 0020): Agent file
// processing inserts them into agent_session_files, the raw layer, and then
// the parse makes each line of a .jsonl file into a Log of the Agent
// Project, the derived layer.
package agentprocessing

import (
	"bytes"
	"fmt"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/storage"
)

// maxBytesPerInsert is roughly how many bytes of units one insert takes for
// a Project: whole batches, at least one.
const maxBytesPerInsert = 16 << 20

// Store is ClickHouse's raw table of Agent sessions as Agent file
// processing uses it. *storage.ClickHouse is the production adapter.
type Store interface {
	InsertAgentFiles(projectID uint32, insert storage.AgentFileInsert) error
}

// Processor is Agent file processing: one worker that reads each Project's
// Agent file batches from Pebble after its cursor and inserts them into
// ClickHouse, each once, as Span processing does its Spans:
//  1. it records in Pebble the range of batches it is about to insert;
//  2. it inserts their units, with a dedup token made from the range;
//  3. it moves the cursor past the range and tells the parse.
//
// After a crash or a failed insert, the recorded range is inserted again
// with the same token, and ClickHouse drops what it already has. A Project
// whose insert fails is retried with backoff while the others carry on.
// The parse deletes the batches once it has parsed them.
type Processor struct {
	*loop
	batches *storage.PebbleWriter
	store   Store
	stored  func(projectID uint32)
}

// New constructs Agent file processing. stored is called for a Project
// after each range of its batches is in ClickHouse: the parse's Notify.
func New(batches *storage.PebbleWriter, store Store, stored func(projectID uint32)) *Processor {
	p := &Processor{batches: batches, store: store, stored: stored}
	p.loop = newLoop("Agent file processing", p.storeUnits)
	return p
}

// Prepare runs before the worker starts, and again after an error. The agent
// ingest API may write before it, as Pebble keeps new batch ids above every
// cursor from its open. It keeps new batch ids above every Project's cursor, and
// marks every Project with batches as pending.
func (p *Processor) Prepare() error {
	if err := p.batches.KeepBatchIDsAboveAgentFileCursors(); err != nil {
		return fmt.Errorf("read agent file processing cursors: %w", err)
	}
	projects, err := p.batches.ProjectsWithAgentFileBatches()
	if err != nil {
		return fmt.Errorf("list projects with agent file batches: %w", err)
	}
	for _, projectID := range projects {
		p.Notify(projectID)
	}
	return nil
}

// storeUnits stores the units of a Project's batches after its cursor, up
// to about maxBytesPerInsert: or, while the range it last recorded is past
// the cursor, that range again. more reports whether more may be waiting.
func (p *Processor) storeUnits(projectID uint32) (more bool, err error) {
	cursor, err := p.batches.AgentFileCursor(projectID)
	if err != nil {
		return false, fmt.Errorf("read agent file processing cursor: %w", err)
	}
	recorded, err := p.batches.RecordedAgentFileInsert(projectID)
	if err != nil {
		return false, fmt.Errorf("read the recorded range of agent file batches: %w", err)
	}
	batches, err := p.batches.AgentFileBatchesAfter(projectID, cursor, maxBytesPerInsert)
	if err != nil {
		return false, fmt.Errorf("read agent file batches: %w", err)
	}
	batches, again := nextRange(batches, cursor, recorded)
	if len(batches) == 0 {
		return false, nil
	}
	last := batches[len(batches)-1].ID
	if !again {
		if err := p.batches.RecordAgentFileInsert(projectID, last); err != nil {
			return false, fmt.Errorf("record the range of agent file batches: %w", err)
		}
	}

	units := make([]storage.AgentFileUnit, len(batches))
	for i, batch := range batches {
		units[i] = batch.Unit
	}
	if err := p.store.InsertAgentFiles(projectID, storage.AgentFileInsert{After: cursor, UpTo: last, Units: units}); err != nil {
		return false, fmt.Errorf("store agent session files: %w", err)
	}
	if err := p.batches.SetAgentFileCursor(projectID, last); err != nil {
		return false, fmt.Errorf("move agent file processing cursor: %w", err)
	}
	if p.stored != nil {
		p.stored(projectID)
	}
	// Batches may be waiting past this range: the next pass finds out.
	return true, nil
}

// nextRange is the range of batches, read after cursor, to insert next.
// While the range last recorded is past the cursor, it may have been
// inserted, whole or in part: it goes again, the same batches under the
// same token, never a longer range, and again is true.
func nextRange(batches []storage.AgentFileBatch, cursor, recorded uuid.UUID) (_ []storage.AgentFileBatch, again bool) {
	if bytes.Compare(recorded[:], cursor[:]) <= 0 {
		return batches, false
	}
	return upTo(batches, recorded), true
}

// upTo is the batches up to and including the batch with id last.
func upTo(batches []storage.AgentFileBatch, last uuid.UUID) []storage.AgentFileBatch {
	for i, batch := range batches {
		if bytes.Compare(batch.ID[:], last[:]) > 0 {
			return batches[:i]
		}
	}
	return batches
}
