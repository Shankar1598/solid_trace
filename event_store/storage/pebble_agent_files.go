package storage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/google/uuid"
	"github.com/vmihailenco/msgpack/v5"
)

// The agent ingest API stores each unit of an Agent session's file that it
// accepts as one Agent file batch in Pebble, as Log ingest stores a Log
// batch, and Agent file processing inserts the batches into
// agent_session_files about one time a second. In the same Pebble write it
// keeps the state of the file (its highest version and its end offset) and
// of the Agent session (who and where it is), which the upload checks and
// the offsets call answers at once, before an insert.

// AgentSession is who and where an Agent session is, as its first upload
// gave it. The msgpack tags are the stored format: do not rename them.
type AgentSession struct {
	// UserID is the person: the user of the token that uploaded it.
	UserID int64 `msgpack:"user_id"`
	// Harness is the coding agent, as claude-code.
	Harness string `msgpack:"harness"`
	// Repository is the matched remote, as host/workspace/name.
	Repository string `msgpack:"repository"`
	// Folder is the path inside the repository, "" for its root.
	Folder string `msgpack:"folder"`
	// RunID and Project are set for the Agent session of a Run: its id
	// and its Project in the factory.
	RunID   string `msgpack:"run_id"`
	Project string `msgpack:"project"`
	// Flags are the AgentFlag bits of each unit stored of the Agent
	// session: it has a cut unit, or a trimmed .jsonl file.
	Flags uint8 `msgpack:"flags"`
}

// The flags of a unit (the flags column of agent_session_files) and of an
// Agent session (each flag of one of its units). The values are stored:
// do not change them.
const (
	// AgentFlagCut is a unit over agent ingest's 32 MiB limit, stored
	// cut: its bytes are its first 32 MiB, and its last row's end offset
	// is its end on disk. This is the marker of the cut.
	AgentFlagCut uint8 = 1
	// AgentFlagTrimmed is the first unit of a new version of a .jsonl
	// file: the file became shorter than what the server had of it.
	AgentFlagTrimmed uint8 = 2
)

// AgentFlagNames are the names of the flags set in flags, as the API
// gives them.
func AgentFlagNames(flags uint8) []string {
	names := []string{}
	if flags&AgentFlagCut != 0 {
		names = append(names, "cut")
	}
	if flags&AgentFlagTrimmed != 0 {
		names = append(names, "trimmed")
	}
	return names
}

// AgentFileUnit is one unit of a file of an Agent session: for a .jsonl
// file, whole lines from Offset. The msgpack tags are the stored format.
type AgentFileUnit struct {
	SessionID   uuid.UUID    `msgpack:"session_id"`
	Path        string       `msgpack:"path"`
	Version     uint32       `msgpack:"version"`
	Offset      uint64       `msgpack:"offset"`
	ContentHash string       `msgpack:"content_hash"`
	Bytes       []byte       `msgpack:"bytes"`
	StoredAt    time.Time    `msgpack:"stored_at"`
	Session     AgentSession `msgpack:"session"`
	// Size is the size of the unit on disk when it is more than Bytes (a
	// cut unit), else 0.
	Size uint64 `msgpack:"size,omitempty"`
	// Flags are the unit's AgentFlag bits.
	Flags uint8 `msgpack:"flags,omitempty"`
}

// End is the offset after the unit on disk.
func (u AgentFileUnit) End() uint64 {
	if u.Size > uint64(len(u.Bytes)) {
		return u.Offset + u.Size
	}
	return u.Offset + uint64(len(u.Bytes))
}

// AgentFileState is what the server has of one path of an Agent session:
// its highest version, with the end offset and hash of what is stored of
// it. The msgpack tags are the stored format.
type AgentFileState struct {
	Path        string `msgpack:"-"`
	Version     uint32 `msgpack:"version"`
	EndOffset   uint64 `msgpack:"end_offset"`
	ContentHash string `msgpack:"content_hash"`
	// Batch is the Agent file batch of the last unit stored of the path,
	// uuid.Nil in a state from before it was kept. Once Agent file
	// processing's cursor is at it, the path's units are in ClickHouse.
	Batch uuid.UUID `msgpack:"batch"`
}

// AgentFileBatch is one stored unit, under its batch id.
type AgentFileBatch struct {
	ID   uuid.UUID
	Unit AgentFileUnit
}

// AgentUploadConflict is an upload that does not continue what is stored:
// State is what the server has of the path.
type AgentUploadConflict struct {
	Reason string
	State  AgentFileState
}

func (c *AgentUploadConflict) Error() string { return c.Reason }

// ErrAgentSessionOfAnotherPerson is an upload, or a read of the offsets,
// from a person to an Agent session that another person uploaded.
var ErrAgentSessionOfAnotherPerson = errors.New("the Agent session is another person's")

// AgentUpload is a unit to store. AnyPerson lets the upload add to an
// Agent session of another person: a token limited to a Run's Agent
// sessions may, as different people continue a Run.
type AgentUpload struct {
	Unit      AgentFileUnit
	AnyPerson bool
}

// Keyspaces, each the reserved Project id 0 and a prefix, apart from those
// of Log and Transaction batches:
//   - "agentunits/" + Project + batch id: the Agent file batches;
//   - "agentfile/" + Project + session id + path: an AgentFileState;
//   - "agentsession/" + Project + session id: an AgentSession;
//   - "agentcursor/" and "agentinsert/" + Project: Agent file
//     processing's cursor and recorded range;
//   - "agentparsecursor/" and "agentparseinsert/" + Project: the parse's
//     cursor and recorded range. The parse reads only the batches that
//     Agent file processing has stored, so its cursor is never past that
//     cursor, and it deletes the batches it has parsed;
//   - "agentparsestate/" + Project: what the parse keeps from one range
//     to the next, written with its cursor.
var (
	agentFileBatchKeyspace    = append(eventKeyPrefix(0), "agentunits/"...)
	agentFileBatchKeyspaceEnd = append(eventKeyPrefix(0), "agentunits0"...)
)

func agentFileBatchPrefix(projectID uint32) []byte {
	return binary.BigEndian.AppendUint32(bytes.Clone(agentFileBatchKeyspace), projectID)
}

func agentFileBatchUpperBound(projectID uint32) []byte {
	if projectID == math.MaxUint32 {
		return agentFileBatchKeyspaceEnd
	}
	return agentFileBatchPrefix(projectID + 1)
}

func agentFileBatchKey(projectID uint32, batchID uuid.UUID) []byte {
	return append(agentFileBatchPrefix(projectID), batchID[:]...)
}

func agentFileStatePrefix(projectID uint32, sessionID uuid.UUID) []byte {
	key := binary.BigEndian.AppendUint32(append(eventKeyPrefix(0), "agentfile/"...), projectID)
	return append(key, sessionID[:]...)
}

func agentSessionKey(projectID uint32, sessionID uuid.UUID) []byte {
	key := binary.BigEndian.AppendUint32(append(eventKeyPrefix(0), "agentsession/"...), projectID)
	return append(key, sessionID[:]...)
}

func agentFileCursorKey(projectID uint32) []byte {
	return binary.BigEndian.AppendUint32(append(eventKeyPrefix(0), "agentcursor/"...), projectID)
}

func agentFileInsertKey(projectID uint32) []byte {
	return binary.BigEndian.AppendUint32(append(eventKeyPrefix(0), "agentinsert/"...), projectID)
}

func agentParseCursorKey(projectID uint32) []byte {
	return binary.BigEndian.AppendUint32(append(eventKeyPrefix(0), "agentparsecursor/"...), projectID)
}

func agentParseStateKey(projectID uint32) []byte {
	return binary.BigEndian.AppendUint32(append(eventKeyPrefix(0), "agentparsestate/"...), projectID)
}

func agentParseInsertKey(projectID uint32) []byte {
	return binary.BigEndian.AppendUint32(append(eventKeyPrefix(0), "agentparseinsert/"...), projectID)
}

// AgentParseCursor returns the id of the last Agent file batch whose lines
// the parse has made into Logs for a Project, or uuid.Nil.
func (w *PebbleWriter) AgentParseCursor(projectID uint32) (uuid.UUID, error) {
	return w.logBatchID(agentParseCursorKey(projectID))
}

// SetAgentParseCursor moves a Project's parse cursor once the Logs of its
// range are inserted, in one write with the parse state after the range.
// NoSync, as SetLogProcessingCursor.
func (w *PebbleWriter) SetAgentParseCursor(projectID uint32, cursor uuid.UUID, state []byte) error {
	batch := w.db.NewBatch()
	defer batch.Close()
	if err := batch.Set(agentParseCursorKey(projectID), cursor[:], nil); err != nil {
		return err
	}
	if len(state) == 0 {
		if err := batch.Delete(agentParseStateKey(projectID), nil); err != nil {
			return err
		}
	} else if err := batch.Set(agentParseStateKey(projectID), state, nil); err != nil {
		return err
	}
	return batch.Commit(pebble.NoSync)
}

// AgentParseState returns what the parse keeps of a Project from one range
// to the next, as the parse encoded it, or nil. It moves with the cursor,
// so a range parsed again after a crash reads the state that it read the
// first time.
func (w *PebbleWriter) AgentParseState(projectID uint32) ([]byte, error) {
	value, closer, err := w.db.Get(agentParseStateKey(projectID))
	if errors.Is(err, pebble.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer closer.Close()
	return bytes.Clone(value), nil
}

// RecordAgentParseInsert records, synced, the last batch of the range that
// the parse is about to insert, as RecordLogInsert.
func (w *PebbleWriter) RecordAgentParseInsert(projectID uint32, upTo uuid.UUID) error {
	return w.db.Set(agentParseInsertKey(projectID), upTo[:], pebble.Sync)
}

// RecordedAgentParseInsert returns the last batch of the range that the
// parse last recorded for a Project, or uuid.Nil.
func (w *PebbleWriter) RecordedAgentParseInsert(projectID uint32) (uuid.UUID, error) {
	return w.logBatchID(agentParseInsertKey(projectID))
}

// agentFileBatchFormat is the first byte of every Agent file batch value.
const agentFileBatchFormat byte = 1

// WriteAgentFileUnit stores a unit when it continues what the server has of
// its path, in one Pebble write with the new state of the path and, for a
// new Agent session, the Agent session:
//   - at the stored end offset of the stored version, it is added;
//   - at offset 0 of a version higher than the stored one (none is version
//     0), it starts that version;
//   - when it lies wholly within what is stored of the stored version, it
//     is a repeat, and nothing is stored (stored false);
//   - else it leaves a gap, overlaps the end, or is of an older version:
//     an *AgentUploadConflict.
//
// A file that is not .jsonl is uploaded whole with its content hash: with
// the stored hash it is a repeat of any version from the stored one, and
// with another hash it must be a higher version. The first unit of a new
// version of a .jsonl file (a trim) is flagged AgentFlagTrimmed. The Agent
// session gets the flags of each unit.
//
// The unit must not be empty. One mutex serialises the check and the
// write, so two uploads of one path never both pass.
func (w *PebbleWriter) WriteAgentFileUnit(projectID uint32, upload AgentUpload) (state AgentFileState, stored bool, err error) {
	unit := upload.Unit
	encode := func() (*bytes.Buffer, error) {
		var buf bytes.Buffer
		buf.WriteByte(agentFileBatchFormat)
		if err := msgpack.NewEncoder(&buf).Encode(unit); err != nil {
			return nil, fmt.Errorf("encode agent file batch: %w", err)
		}
		return &buf, nil
	}
	// Encoded before the lock; again in the rare case that the flags change.
	buf, err := encode()
	if err != nil {
		return state, false, err
	}

	w.agentMu.Lock()
	defer w.agentMu.Unlock()
	session, known, err := w.agentSession(projectID, unit.SessionID)
	if err != nil {
		return state, false, err
	}
	if known && !upload.AnyPerson && session.UserID != unit.Session.UserID {
		return state, false, ErrAgentSessionOfAnotherPerson
	}
	stateKey := append(agentFileStatePrefix(projectID, unit.SessionID), unit.Path...)
	have, err := w.agentFileState(stateKey)
	if err != nil {
		return state, false, err
	}
	have.Path = unit.Path
	end := unit.End()
	whole := !strings.HasSuffix(unit.Path, ".jsonl")
	switch {
	case whole && have.Version > 0 && unit.ContentHash == have.ContentHash && unit.Version >= have.Version:
		return have, false, nil
	case whole && unit.Version == have.Version:
		return have, false, &AgentUploadConflict{State: have, Reason: fmt.Sprintf(
			"Another hash: the server has version %d of %s with the hash %s. Upload a changed file as version %d.",
			have.Version, unit.Path, have.ContentHash, have.Version+1)}
	case unit.Version > have.Version && unit.Offset == 0:
		state = AgentFileState{Path: unit.Path, Version: unit.Version, EndOffset: end, ContentHash: unit.ContentHash}
	case unit.Version > have.Version:
		return have, false, &AgentUploadConflict{State: have, Reason: fmt.Sprintf(
			"The server has no version %d of %s yet: a new version starts at offset 0, not %d.", unit.Version, unit.Path, unit.Offset)}
	case unit.Version < have.Version:
		return have, false, &AgentUploadConflict{State: have, Reason: fmt.Sprintf(
			"The server has version %d of %s, which is newer than version %d.", have.Version, unit.Path, unit.Version)}
	case end <= have.EndOffset:
		return have, false, nil
	case unit.Offset > have.EndOffset:
		return have, false, &AgentUploadConflict{State: have, Reason: fmt.Sprintf(
			"A gap: the server has version %d of %s up to offset %d, and this upload starts at offset %d. Upload from offset %d.",
			have.Version, unit.Path, have.EndOffset, unit.Offset, have.EndOffset)}
	case unit.Offset < have.EndOffset:
		return have, false, &AgentUploadConflict{State: have, Reason: fmt.Sprintf(
			"An overlap: the server has version %d of %s up to offset %d, and this upload starts at offset %d and ends at %d. Upload from offset %d.",
			have.Version, unit.Path, have.EndOffset, unit.Offset, end, have.EndOffset)}
	default:
		state = AgentFileState{Path: unit.Path, Version: have.Version, EndOffset: end, ContentHash: unit.ContentHash}
	}
	if !whole && have.Version > 0 && unit.Version > have.Version && unit.Flags&AgentFlagTrimmed == 0 {
		unit.Flags |= AgentFlagTrimmed
		if buf, err = encode(); err != nil {
			return have, false, err
		}
	}
	batch := w.db.NewBatch()
	defer batch.Close()
	if !known {
		session = unit.Session
		session.Flags = 0
	}
	if flags := session.Flags | unit.Flags; !known || flags != session.Flags {
		session.Flags = flags
		encodedSession, err := msgpack.Marshal(session)
		if err != nil {
			return have, false, fmt.Errorf("encode agent session: %w", err)
		}
		if err := batch.Set(agentSessionKey(projectID, unit.SessionID), encodedSession, nil); err != nil {
			return have, false, err
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return have, false, ErrShuttingDown
	}
	batchID, err := newLogBatchID(unit.StoredAt)
	if err != nil {
		return have, false, fmt.Errorf("generate agent file batch id: %w", err)
	}
	if bytes.Compare(batchID[:], w.logBatchFloor[:]) <= 0 {
		batchID = nextLogBatchID(w.logBatchFloor)
	}
	if err := batch.Set(agentFileBatchKey(projectID, batchID), buf.Bytes(), nil); err != nil {
		return have, false, err
	}
	state.Batch = batchID
	encodedState, err := msgpack.Marshal(state)
	if err != nil {
		return have, false, fmt.Errorf("encode agent file state: %w", err)
	}
	if err := batch.Set(stateKey, encodedState, nil); err != nil {
		return have, false, err
	}
	// NoSync, as for Logs: a 200 means stored as a Log batch is.
	if err := batch.Commit(pebble.NoSync); err != nil {
		return have, false, err
	}
	w.logBatchFloor = batchID
	return state, true, nil
}

func (w *PebbleWriter) agentSession(projectID uint32, sessionID uuid.UUID) (AgentSession, bool, error) {
	var session AgentSession
	value, err := w.get(agentSessionKey(projectID, sessionID))
	if err != nil || value == nil {
		return session, false, err
	}
	if err := msgpack.Unmarshal(value, &session); err != nil {
		return session, false, fmt.Errorf("agent session %s: %w", sessionID, err)
	}
	return session, true, nil
}

func (w *PebbleWriter) agentFileState(key []byte) (AgentFileState, error) {
	var state AgentFileState
	value, err := w.get(key)
	if err != nil || value == nil {
		return state, err
	}
	if err := msgpack.Unmarshal(value, &state); err != nil {
		return state, fmt.Errorf("agent file state %x: %w", key, err)
	}
	return state, nil
}

// AgentSessionFiles returns what the server has of an Agent session: who
// and where it is, and the state of each path, in path order. known is
// false for an Agent session with no upload.
func (w *PebbleWriter) AgentSessionFiles(projectID uint32, sessionID uuid.UUID) (session AgentSession, known bool, files []AgentFileState, err error) {
	session, known, err = w.agentSession(projectID, sessionID)
	if err != nil || !known {
		return session, known, nil, err
	}
	prefix := agentFileStatePrefix(projectID, sessionID)
	upper := append(bytes.Clone(prefix), 0xff)
	iter, err := w.db.NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: upper})
	if err != nil {
		return session, known, nil, err
	}
	defer iter.Close()
	files = []AgentFileState{}
	for valid := iter.First(); valid; valid = iter.Next() {
		var state AgentFileState
		if err := msgpack.Unmarshal(iter.Value(), &state); err != nil {
			return session, known, nil, fmt.Errorf("agent file state %x: %w", iter.Key(), err)
		}
		state.Path = string(iter.Key()[len(prefix):])
		files = append(files, state)
	}
	return session, known, files, iter.Error()
}

// AgentFilesInserted reports whether Agent file processing has inserted
// into ClickHouse each unit stored of files, the states that
// AgentSessionFiles gave: its cursor is at or after each one's batch.
func (w *PebbleWriter) AgentFilesInserted(projectID uint32, files []AgentFileState) (bool, error) {
	cursor, err := w.AgentFileCursor(projectID)
	if err != nil {
		return false, err
	}
	for _, f := range files {
		if bytes.Compare(f.Batch[:], cursor[:]) > 0 {
			return false, nil
		}
	}
	return true, nil
}

// AgentFileBatchesAfter returns a Project's Agent file batches after the
// batch with id after, in id order, uuid.Nil starting at its first. It
// stops once they hold maxBytes or more, so it returns at least one batch
// when there is one.
func (w *PebbleWriter) AgentFileBatchesAfter(projectID uint32, after uuid.UUID, maxBytes int) ([]AgentFileBatch, error) {
	lower := agentFileBatchPrefix(projectID)
	if after != uuid.Nil {
		lower = append(agentFileBatchKey(projectID, after), 0)
	}
	iter, err := w.db.NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: agentFileBatchUpperBound(projectID)})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var batches []AgentFileBatch
	size := 0
	for valid := iter.First(); valid && size < maxBytes; valid = iter.Next() {
		key := iter.Key()
		batchID, err := uuid.FromBytes(key[len(key)-16:])
		if err != nil {
			return nil, fmt.Errorf("agent file batch key %x: %w", key, err)
		}
		value := iter.Value()
		if len(value) == 0 || value[0] != agentFileBatchFormat {
			return nil, fmt.Errorf("agent file batch %x: unknown format", key)
		}
		batch := AgentFileBatch{ID: batchID}
		if err := msgpack.Unmarshal(value[1:], &batch.Unit); err != nil {
			return nil, fmt.Errorf("agent file batch %x: %w", key, err)
		}
		batches = append(batches, batch)
		size += len(value)
	}
	return batches, iter.Error()
}

// DeleteAgentFileBatchesUpTo deletes a Project's Agent file batches up to
// and including the batch with id upTo. NoSync, as DeleteLogBatchesUpTo.
func (w *PebbleWriter) DeleteAgentFileBatchesUpTo(projectID uint32, upTo uuid.UUID) error {
	return w.db.DeleteRange(agentFileBatchPrefix(projectID), append(agentFileBatchKey(projectID, upTo), 0), pebble.NoSync)
}

// ProjectsWithAgentFileBatches returns every Project that has at least one
// Agent file batch, reading one key per Project.
func (w *PebbleWriter) ProjectsWithAgentFileBatches() ([]uint32, error) {
	iter, err := w.db.NewIter(&pebble.IterOptions{LowerBound: agentFileBatchKeyspace, UpperBound: agentFileBatchKeyspaceEnd})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	var projects []uint32
	for valid := iter.First(); valid; {
		projectID := binary.BigEndian.Uint32(iter.Key()[len(agentFileBatchKeyspace):])
		projects = append(projects, projectID)
		valid = iter.SeekGE(agentFileBatchUpperBound(projectID))
	}
	return projects, iter.Error()
}

// AgentFileCursor returns the id of the last Agent file batch whose unit
// Agent file processing has stored for a Project, or uuid.Nil.
func (w *PebbleWriter) AgentFileCursor(projectID uint32) (uuid.UUID, error) {
	return w.logBatchID(agentFileCursorKey(projectID))
}

// SetAgentFileCursor moves a Project's Agent file processing cursor once
// its range is stored. NoSync, as SetLogProcessingCursor.
func (w *PebbleWriter) SetAgentFileCursor(projectID uint32, cursor uuid.UUID) error {
	return w.db.Set(agentFileCursorKey(projectID), cursor[:], pebble.NoSync)
}

// KeepBatchIDsAboveAgentFileCursors makes every batch id written from now
// on come after every Project's Agent file processing cursor.
func (w *PebbleWriter) KeepBatchIDsAboveAgentFileCursors() error {
	iter, err := w.db.NewIter(&pebble.IterOptions{
		LowerBound: append(eventKeyPrefix(0), "agentcursor/"...),
		UpperBound: append(eventKeyPrefix(0), "agentcursor0"...),
	})
	if err != nil {
		return err
	}
	defer iter.Close()
	for valid := iter.First(); valid; valid = iter.Next() {
		cursor, err := uuid.FromBytes(iter.Value())
		if err != nil {
			return fmt.Errorf("agent file processing cursor %x: %w", iter.Key(), err)
		}
		w.KeepLogBatchIDsAbove(cursor)
	}
	return iter.Error()
}

// RecordAgentFileInsert records, synced, the last batch of the range Agent
// file processing is about to insert, as RecordLogInsert.
func (w *PebbleWriter) RecordAgentFileInsert(projectID uint32, upTo uuid.UUID) error {
	return w.db.Set(agentFileInsertKey(projectID), upTo[:], pebble.Sync)
}

// RecordedAgentFileInsert returns the last batch of the range Agent file
// processing last recorded for a Project, or uuid.Nil.
func (w *PebbleWriter) RecordedAgentFileInsert(projectID uint32) (uuid.UUID, error) {
	return w.logBatchID(agentFileInsertKey(projectID))
}

// LogIngestCursor returns a Project's ingest cursor: the highest batch id
// such that every Log of a batch up to it is in ClickHouse, and no Log of a
// batch up to it comes later. The read of the Logs ingested after a batch
// reads up to it. Two workers insert a Project's Logs: Log processing, from
// Log batches, and the parse, from Agent file batches. Both take their ids
// from one sequence, and each inserts in id order and moves its cursor after
// the insert. So the ingest cursor is the higher of the two cursors that no
// batch of the other worker, still waiting, comes before. A batch written
// after this read has a higher id than every batch now in Pebble. It is
// uuid.Nil while neither worker has inserted Logs for the Project. It can be
// lower than a cursor it gave before, when the worker of that cursor has
// gone past a batch of the other that waits; that cursor stays valid
// (ClickHouseLogs.ScanLogsIngestedAfter).
func (w *PebbleWriter) LogIngestCursor(projectID uint32) (uuid.UUID, error) {
	logs, err := w.LogProcessingCursor(projectID)
	if err != nil {
		return uuid.Nil, err
	}
	firstLogBatch, logsWait, err := w.firstBatchAfter(logBatchPrefix(projectID), logBatchUpperBound(projectID), logs)
	if err != nil {
		return uuid.Nil, err
	}
	parse, err := w.AgentParseCursor(projectID)
	if err != nil {
		return uuid.Nil, err
	}
	firstAgentBatch, parseWaits, err := w.firstBatchAfter(agentFileBatchPrefix(projectID), agentFileBatchUpperBound(projectID), parse)
	if err != nil {
		return uuid.Nil, err
	}
	cursor := uuid.Nil
	if !parseWaits || bytes.Compare(logs[:], firstAgentBatch[:]) < 0 {
		cursor = logs
	}
	if (!logsWait || bytes.Compare(parse[:], firstLogBatch[:]) < 0) && bytes.Compare(parse[:], cursor[:]) > 0 {
		cursor = parse
	}
	return cursor, nil
}

// firstBatchAfter returns the id of the first batch in [prefix, upper)
// after the batch with id after, and whether there is one. Each key ends
// with its batch id.
func (w *PebbleWriter) firstBatchAfter(prefix, upper []byte, after uuid.UUID) (uuid.UUID, bool, error) {
	lower := prefix
	if after != uuid.Nil {
		lower = append(append(bytes.Clone(prefix), after[:]...), 0)
	}
	iter, err := w.db.NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: upper})
	if err != nil {
		return uuid.Nil, false, err
	}
	defer iter.Close()
	if !iter.First() {
		return uuid.Nil, false, iter.Error()
	}
	key := iter.Key()
	id, err := uuid.FromBytes(key[len(key)-16:])
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("batch key %x: %w", key, err)
	}
	return id, true, nil
}
