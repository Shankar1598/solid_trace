package ingest

import (
	"encoding/json"
	"regexp"

	"github.com/solidtrace/event_store/ingest/normalise"
	"github.com/solidtrace/event_store/models"
)

// RunEventsFile is the file of a Run's events: runs/<run id>, so that one
// Run's events are one range of the logs table's sort key.
const RunEventsFile = "runs/"

// DispatcherFile is the file of the Dispatcher's own Logs, its warnings.
const DispatcherFile = "dispatcher"

// runIDPattern is a Run id that can be part of a file: the Dispatcher's ids
// are 16 lower-case letters and digits.
var runIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// IngestRunEvents ingests a batch of the Dispatcher's Logs into the Agent
// Project projectID, as IngestLogs ingests a body of the JSON Log route.
//
// A Log with a run.id Attribute is a run event: it must also have a
// run.seq number, and the Run id must be a plain name, or it is dropped.
// Its service becomes its repository Attribute and its file runs/<run id>.
// A Log with no run.id is the Dispatcher's own Log, and its file becomes
// dispatcher. So the server, not the sender, puts each Log in its range.
func (s *Service) IngestRunEvents(projectID uint32, body Body) (LogCounts, error) {
	if !s.enterGate() {
		s.counts.count(projectID, s.clock.Now(), models.IngestCount{RequestsOverloaded: 1})
		return LogCounts{}, ErrOverloaded
	}
	defer s.leaveGate()

	raw, err := body.Read()
	if err != nil {
		return LogCounts{}, err
	}
	objects, dropped := logObjects(raw, false)
	request := models.IngestCount{BytesReceived: int64(len(raw)), BytesOnWire: int64(body.Wire)}
	return s.storeLogs(projectID, objects, dropped, request, normaliseRunEvent)
}

func normaliseRunEvent(object map[string]json.RawMessage, r normalise.Receipt) (models.Log, normalise.Outcome) {
	log, outcome := normalise.JSON(object, r)
	if outcome == normalise.Dropped {
		return log, outcome
	}
	if log.Attributes.String == nil {
		log.Attributes.String = map[string]string{}
	}
	// An Attribute's name is in one map only: file becomes a string.
	delete(log.Attributes.Number, "file")
	delete(log.Attributes.Bool, "file")
	delete(log.Attributes.JSON, "file")
	runID, isRun := log.Attributes.String["run.id"]
	if !isRun && !hasAttribute(log.Attributes, "run.id") {
		log.Attributes.String["file"] = DispatcherFile
		return log, outcome
	}
	if _, ok := log.Attributes.Number["run.seq"]; !ok || !runIDPattern.MatchString(runID) {
		return log, normalise.Dropped
	}
	log.Service = log.Attributes.String["repository"]
	log.Attributes.String["file"] = RunEventsFile + runID
	return log, outcome
}

// hasAttribute reports whether a has a value that is not a string under
// name.
func hasAttribute(a models.Attributes, name string) bool {
	_, number := a.Number[name]
	_, boolean := a.Bool[name]
	_, object := a.JSON[name]
	return number || boolean || object
}
