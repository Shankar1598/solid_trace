package intake

import (
	"errors"
	"testing"
)

func TestServiceIngestEnvelopeRejectsShortEnvelope(t *testing.T) {
	service := NewService(nil, nil)

	err := service.IngestEnvelope(123, []byte("{}\n{}"))
	if !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}
}

func TestServiceIngestEnvelopeRejectsInvalidItemHeader(t *testing.T) {
	service := NewService(nil, nil)
	envelope := []byte("{}\nnot-json\n{}")

	err := service.IngestEnvelope(123, envelope)
	if !errors.Is(err, ErrInvalidItemHeader) {
		t.Fatalf("expected ErrInvalidItemHeader, got %v", err)
	}
}

func TestServiceIngestEnvelopeIgnoresUnsupportedItems(t *testing.T) {
	service := NewService(nil, nil)
	envelope := []byte("{}\n{\"type\":\"attachment\"}\nignored")

	if err := service.IngestEnvelope(123, envelope); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestServiceIngestStoreRejectsInvalidJSON(t *testing.T) {
	service := NewService(nil, nil)

	err := service.IngestStore(123, []byte("not-json"))
	if !errors.Is(err, ErrInvalidEventJSON) {
		t.Fatalf("expected ErrInvalidEventJSON, got %v", err)
	}
}
