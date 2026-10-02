package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
)

func TestMain(m *testing.M) {
	logger.Init()
	os.Exit(m.Run())
}

// fakeEventWriter records written Events on a channel and assigns each a
// UUID, as storage.PebbleWriter does. With block set, each write signals
// entered and waits for block to close.
type fakeEventWriter struct {
	written chan models.Event
	err     error
	block   chan struct{}
	entered chan struct{}
	nextID  atomic.Int64
}

func newFakeEventWriter() *fakeEventWriter {
	return &fakeEventWriter{
		written: make(chan models.Event, 10),
		entered: make(chan struct{}, 10),
	}
}

func (f *fakeEventWriter) WriteEvent(event *models.Event) error {
	if f.block != nil {
		f.entered <- struct{}{}
		<-f.block
	}
	if f.err != nil {
		return f.err
	}
	event.EventUUID = fmt.Sprintf("event-%d", f.nextID.Add(1))
	f.written <- *event
	return nil
}

func newTestService(events EventWriter) *Service {
	return NewService(Config{Events: events, NotifyEvents: func(uint32) {}, MaxWaiting: 10})
}

// ---------------------------------------------------------------------------
// Envelope parsing and validation
// ---------------------------------------------------------------------------

func TestServiceIngestEnvelopeRejectsInvalidEnvelopeHeader(t *testing.T) {
	service := newTestService(nil)

	err := service.IngestEnvelope(123, []byte("not-json\n{\"type\":\"event\"}\n{}"))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}
}

func TestServiceIngestEnvelopeRejectsItemHeaderWithoutType(t *testing.T) {
	service := newTestService(nil)

	err := service.IngestEnvelope(123, []byte("{}\n{}"))
	if !errors.Is(err, ErrInvalidItemHeader) {
		t.Fatalf("expected ErrInvalidItemHeader, got %v", err)
	}
}

func TestServiceIngestEnvelopeRejectsLengthPastEnd(t *testing.T) {
	service := newTestService(nil)

	err := service.IngestEnvelope(123, []byte("{}\n{\"type\":\"event\",\"length\":100}\n{}"))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}
}

func TestServiceIngestEnvelopeRejectsPayloadLongerThanLength(t *testing.T) {
	service := newTestService(nil)

	err := service.IngestEnvelope(123, []byte("{}\n{\"type\":\"event\",\"length\":1}\n{}"))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}
}

func TestServiceIngestEnvelopeRejectsInvalidItemHeader(t *testing.T) {
	service := newTestService(nil)
	envelope := []byte("{}\nnot-json\n{}")

	err := service.IngestEnvelope(123, envelope)
	if !errors.Is(err, ErrInvalidItemHeader) {
		t.Fatalf("expected ErrInvalidItemHeader, got %v", err)
	}
}

func TestServiceIngestEnvelopeIgnoresUnsupportedItems(t *testing.T) {
	service := newTestService(nil)
	envelope := []byte("{}\n{\"type\":\"attachment\"}\nignored")

	if err := service.IngestEnvelope(123, envelope); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestServiceIngestStoreRejectsInvalidJSON(t *testing.T) {
	service := newTestService(nil)

	err := service.IngestStore(123, []byte("not-json"))
	if !errors.Is(err, ErrInvalidEventJSON) {
		t.Fatalf("expected ErrInvalidEventJSON, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Storing Events
// ---------------------------------------------------------------------------

func sampleEventJSON() []byte {
	event := map[string]interface{}{
		"event_id": "abc123",
		"message":  "Test error",
		"exception": map[string]interface{}{
			"values": []interface{}{
				map[string]interface{}{
					"type":  "RuntimeError",
					"value": "something broke",
				},
			},
		},
		"environment": "production",
		"server_name": "web-1",
		"release":     "v1.0.0",
		"level":       "error",
		"tags": map[string]interface{}{
			"region": "us-east",
		},
	}
	b, _ := json.Marshal(event)
	return b
}

func TestServiceIngestStore_TooManyWaiting_ReturnsOverloaded(t *testing.T) {
	writer := newFakeEventWriter()
	writer.block = make(chan struct{})
	service := NewService(Config{Events: writer, NotifyEvents: func(uint32) {}, MaxWaiting: 2})

	results := make(chan error, 2)
	for range 2 {
		go func() { results <- service.IngestStore(42, sampleEventJSON()) }()
	}
	for range 2 {
		<-writer.entered
	}

	if err := service.IngestStore(42, sampleEventJSON()); !errors.Is(err, ErrOverloaded) {
		t.Fatalf("expected ErrOverloaded, got %v", err)
	}

	close(writer.block)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("waiting request failed: %v", err)
		}
	}
	if err := service.IngestStore(42, sampleEventJSON()); err != nil {
		t.Fatalf("expected success once the waiting requests finished, got %v", err)
	}
}

func TestServiceIngestStore_WriteError_Propagates(t *testing.T) {
	writer := newFakeEventWriter()
	writer.err = errors.New("pebble: disk full")
	service := newTestService(writer)

	err := service.IngestStore(42, sampleEventJSON())
	if !errors.Is(err, writer.err) {
		t.Fatalf("expected wrapped write error, got %v", err)
	}
}

func TestServiceIngestEnvelope_HappyPath(t *testing.T) {
	writer := newFakeEventWriter()
	service := newTestService(writer)

	eventPayload := `{"message": "Envelope Test", "environment": "staging"}`
	envelope := []byte("{}\n{\"type\":\"event\"}\n" + eventPayload)

	err := service.IngestEnvelope(99, envelope)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(writer.written) != 1 {
		t.Fatalf("expected 1 event, got %d", len(writer.written))
	}

	event := <-writer.written
	if event.ProjectID != 99 {
		t.Errorf("expected ProjectID 99, got %d", event.ProjectID)
	}
	if string(event.RawJSON) != eventPayload {
		t.Errorf("expected RawJSON %q, got %q", eventPayload, event.RawJSON)
	}
}

func TestServiceIngestEnvelope_DropsTransactions(t *testing.T) {
	writer := newFakeEventWriter()
	service := newTestService(writer)

	eventPayload := `{"type": "transaction", "transaction": "GET /"}`
	envelope := []byte("{}\n{\"type\":\"transaction\"}\n" + eventPayload)

	err := service.IngestEnvelope(99, envelope)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(writer.written) != 0 {
		t.Fatalf("expected no events, got %d", len(writer.written))
	}
}

func TestServiceIngestEnvelope_MultipleItems(t *testing.T) {
	eventPayload := `{"message": "multi item", "environment": "production"}`
	attachment := "line one\n{\"type\":\"event\"}\nline three"

	tests := []struct {
		name     string
		envelope string
	}{
		{
			name: "event then attachment, with lengths",
			envelope: "{\"event_id\":\"9ec79c33ec9942ab8353589fcb2e04dc\"}\n" +
				fmt.Sprintf("{\"type\":\"event\",\"length\":%d}\n", len(eventPayload)) + eventPayload + "\n" +
				fmt.Sprintf("{\"type\":\"attachment\",\"length\":%d}\n", len(attachment)) + attachment + "\n",
		},
		{
			name: "attachment before event, event without length",
			envelope: "{}\n" +
				fmt.Sprintf("{\"type\":\"attachment\",\"length\":%d}\n", len(attachment)) + attachment + "\n" +
				"{\"type\":\"event\"}\n" + eventPayload,
		},
		{
			name: "event then client report, no trailing newline",
			envelope: "{}\n" +
				"{\"type\":\"event\"}\n" + eventPayload + "\n" +
				"{\"type\":\"client_report\"}\n{\"discarded_events\":[]}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := newFakeEventWriter()
			service := newTestService(writer)

			if err := service.IngestEnvelope(99, []byte(tt.envelope)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(writer.written) != 1 {
				t.Fatalf("expected 1 event, got %d", len(writer.written))
			}

			event := <-writer.written
			if string(event.RawJSON) != eventPayload {
				t.Errorf("expected RawJSON %q, got %q", eventPayload, event.RawJSON)
			}
		})
	}
}

func TestServiceIngestStore_CopiesRequestBody(t *testing.T) {
	writer := newFakeEventWriter()
	service := newTestService(writer)

	body := sampleEventJSON()
	if err := service.IngestStore(42, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := string(body)
	for i := range body {
		body[i] = 'x' // fasthttp reuses the request buffer after the handler returns
	}

	event := <-writer.written
	if string(event.RawJSON) != want {
		t.Errorf("RawJSON aliases the request body: got %q", event.RawJSON)
	}
}

// ---------------------------------------------------------------------------
// Logs
// ---------------------------------------------------------------------------

// fakeLogWriter records written Log batches. With block set, each write
// signals entered and waits for block to close.
type fakeLogWriter struct {
	written chan []models.Log
	block   chan struct{}
	entered chan struct{}
}

func newFakeLogWriter() *fakeLogWriter {
	return &fakeLogWriter{
		written: make(chan []models.Log, 10),
		entered: make(chan struct{}, 10),
	}
}

func (f *fakeLogWriter) WriteLogBatch(projectID uint32, logs []models.Log) error {
	if f.block != nil {
		f.entered <- struct{}{}
		<-f.block
	}
	f.written <- logs
	return nil
}

func newTestLogService(events EventWriter, logs LogWriter, maxWaiting int) *Service {
	return newRetentionTestService(events, logs, maxWaiting, FixedRetention(30))
}

// newRetentionTestService is a Service whose clock reads 2026-10-02 12:00 UTC
// and whose Projects keep Logs as retention says.
func newRetentionTestService(events EventWriter, logs LogWriter, maxWaiting int, retention Retention) *Service {
	return NewService(Config{
		Events:       events,
		NotifyEvents: func(uint32) {},
		Logs:         logs,
		NotifyLogs:   func(uint32) {},
		Clock:        clock.NewManual(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)),
		Retention:    retention,
		MaxWaiting:   maxWaiting,
	})
}

func TestLogsAreTurnedAwayWhileEventsFillTheWaitingLimit(t *testing.T) {
	events := newFakeEventWriter()
	events.block = make(chan struct{})
	logs := newFakeLogWriter()
	service := newTestLogService(events, logs, 1)

	result := make(chan error, 1)
	go func() { result <- service.IngestStore(42, sampleEventJSON()) }()
	<-events.entered

	if _, err := service.IngestLogs(42, []byte(`{"message":"hi"}`), false); !errors.Is(err, ErrOverloaded) {
		t.Fatalf("expected ErrOverloaded, got %v", err)
	}

	close(events.block)
	if err := <-result; err != nil {
		t.Fatalf("waiting Event failed: %v", err)
	}
	if counts, err := service.IngestLogs(42, []byte(`{"message":"hi"}`), false); err != nil || counts.Accepted != 1 {
		t.Fatalf("expected the Log accepted once the Event was written, got %+v %v", counts, err)
	}
}

func TestEventsAreTurnedAwayWhileLogsFillTheWaitingLimit(t *testing.T) {
	events := newFakeEventWriter()
	logs := newFakeLogWriter()
	logs.block = make(chan struct{})
	service := newTestLogService(events, logs, 1)

	result := make(chan error, 1)
	go func() {
		_, err := service.IngestLogs(42, []byte(`{"message":"hi"}`), false)
		result <- err
	}()
	<-logs.entered

	if err := service.IngestStore(42, sampleEventJSON()); !errors.Is(err, ErrOverloaded) {
		t.Fatalf("expected ErrOverloaded, got %v", err)
	}

	close(logs.block)
	if err := <-result; err != nil {
		t.Fatalf("waiting Logs failed: %v", err)
	}
	if err := service.IngestStore(42, sampleEventJSON()); err != nil {
		t.Fatalf("expected the Event stored once the Logs were written, got %v", err)
	}
}

// projectRetention gives each Project its own Retention, as Console's
// settings do, or fails.
type projectRetention struct {
	days map[uint32]int
	err  error
}

func (r projectRetention) LogRetentionDays(projectID uint32) (int, error) {
	return r.days[projectID], r.err
}

func TestEachProjectsRetentionDecidesWhichLogTimesAreTooOld(t *testing.T) {
	logs := newFakeLogWriter()
	service := newRetentionTestService(nil, logs, 10, projectRetention{days: map[uint32]int{42: 7, 43: 14}})

	// Ten days before the service's clock.
	body := []byte(`{"message": "ten days old", "time": "2026-09-22T12:00:00Z"}`)
	for _, projectID := range []uint32{42, 43} {
		if _, err := service.IngestLogs(projectID, body, false); err != nil {
			t.Fatalf("IngestLogs for %d: %v", projectID, err)
		}
	}

	receivedAt := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	if got := (<-logs.written)[0].Time; !got.Equal(receivedAt) {
		t.Errorf("Expected the receipt time in a Project keeping 7 days, got %v", got)
	}
	if got := (<-logs.written)[0].Time; !got.Equal(time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("Expected the Log's own time in a Project keeping 14 days, got %v", got)
	}
}

func TestLogsAreNotStoredWhenTheProjectsRetentionCantBeRead(t *testing.T) {
	logs := newFakeLogWriter()
	service := newRetentionTestService(nil, logs, 10, projectRetention{err: errors.New("database is locked")})

	if _, err := service.IngestLogs(42, []byte(`{"message": "hi"}`), false); err == nil {
		t.Fatalf("Expected an error")
	}
	if len(logs.written) != 0 {
		t.Errorf("Expected nothing stored, got %d batches", len(logs.written))
	}
}

// ---------------------------------------------------------------------------
// Sentry envelope Logs
// ---------------------------------------------------------------------------

// pythonLogItem is a log item as sentry-python 2.71 sends it: LogBatcher's
// payload, serialised by envelope.Item with its length and a newline.
func pythonLogItem(records ...string) string {
	payload := `{"version": 2, "items": [` + strings.Join(records, ", ") + `]}`
	return fmt.Sprintf(`{"type": "log", "content_type": "application/vnd.sentry.items.log+json", "item_count": %d, "length": %d}`, len(records), len(payload)) +
		"\n" + payload + "\n"
}

// pythonRecord is one record as sentry-python 2.71 serialises a
// sentry_sdk.logger.info call (LogBatcher._to_transport_format).
func pythonRecord(body string) string {
	return `{"timestamp": 1790942340.25, "level": "info", "body": "` + body + `", ` +
		`"attributes": {` +
		`"sentry.environment": {"value": "production", "type": "string"}, ` +
		`"sentry.sdk.name": {"value": "sentry.python", "type": "string"}, ` +
		`"sentry.severity_number": {"value": 9, "type": "integer"}, ` +
		`"sentry.severity_text": {"value": "info", "type": "string"}}, ` +
		`"trace_id": "5b8efff798038103d269b633813fc60c"}`
}

const pythonEnvelopeHeader = `{"sent_at": "2026-10-02T12:00:00.000000Z"}` + "\n"

func messages(logs []models.Log) []string {
	var out []string
	for _, log := range logs {
		out = append(out, log.Message)
	}
	return out
}

func TestAnEnvelopesLogItemIsStoredAsOneLogBatch(t *testing.T) {
	logs := newFakeLogWriter()
	notified := make(chan uint32, 1)
	service := newTestLogService(nil, logs, 10)
	service.notifyLogs = func(projectID uint32) { notified <- projectID }

	envelope := pythonEnvelopeHeader + pythonLogItem(pythonRecord("first"), pythonRecord("second"))
	if err := service.IngestEnvelope(42, []byte(envelope)); err != nil {
		t.Fatalf("IngestEnvelope: %v", err)
	}

	if len(logs.written) != 1 {
		t.Fatalf("Expected one Log batch, got %d", len(logs.written))
	}
	batch := <-logs.written
	if got := messages(batch); !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Errorf("Expected the Logs first and second, got %v", got)
	}
	if got := batch[0].Environment; got != "production" {
		t.Errorf("Expected the environment from sentry.environment, got %q", got)
	}
	if batch[0].ID == batch[1].ID {
		t.Errorf("Expected each Log its own id")
	}
	if got := <-notified; got != 42 {
		t.Errorf("Expected Log processing woken for Project 42, got %d", got)
	}
}
