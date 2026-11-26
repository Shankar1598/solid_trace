package handler

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// endlessDownloads gives rows of 1 MB until a write fails, as the read of
// a large Agent session does.
type endlessDownloads struct{}

func (endlessDownloads) AgentDownloadFiles(context.Context, uint32, uuid.UUID) ([]storage.AgentDownloadFile, error) {
	return nil, nil
}

func (endlessDownloads) ReadAgentDownload(_ context.Context, _ uint32, _ uuid.UUID, _ []storage.AgentDownloadFile, write func(int, []byte) error) error {
	row := make([]byte, 1<<20)
	for {
		if err := write(0, row); err != nil {
			return err
		}
	}
}

// A client that keeps its connection open but stops reading must not keep
// the download: at the timeout, the write that waits for the client stops.
func TestAgentDownloadOfAClientThatStopsReading(t *testing.T) {
	logger.Init()
	h := &AgentHandler{downloads: endlessDownloads{}}
	reader, writer := io.Pipe()
	defer reader.Close()
	files := []storage.AgentDownloadFile{{Path: "session.jsonl", Version: 1, Size: 1 << 40, EndOffset: 1 << 40}}
	done := make(chan struct{})
	go func() {
		h.sendDownload(writer, 50*time.Millisecond, func() {}, 1, uuid.New(), files)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Expected the download to stop at its timeout, but it still waits for the client")
	}
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected the client to read the timeout as an error, got %v", err)
	}
}
