package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/solidtrace/event_store/models"
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
	return NewService(events, func(uint32) {}, 10)
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
	service := NewService(writer, func(uint32) {}, 2)

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
