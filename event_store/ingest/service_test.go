package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
)

func TestMain(m *testing.M) {
	logger.Init()
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// fakeIssueRepository — in-memory adapter for testing Event ingest
// ---------------------------------------------------------------------------

type fakeIssueRepository struct {
	// State
	issues       map[string]fakeIssue // keyed by "projectID:fingerprint"
	nextID       int64
	reopenCalls  []int64 // records which issueIDs were reopened
	createCalls  []fakeCreateCall
	findCalls    []fakeFindCall
	createErr    error // inject errors
	findErr      error
	reopenErr    error
}

type fakeIssue struct {
	issueID       int64
	fingerprintID int64
	status        int
}

type fakeCreateCall struct {
	projectID   uint32
	fingerprint string
	title       string
	culprit     string
	kind        string
}

type fakeFindCall struct {
	projectID   uint32
	fingerprint string
}

func newFakeIssueRepository() *fakeIssueRepository {
	return &fakeIssueRepository{
		issues: make(map[string]fakeIssue),
		nextID: 1,
	}
}

func (f *fakeIssueRepository) key(projectID uint32, fingerprint string) string {
	return fmt.Sprintf("%d:%s", projectID, fingerprint)
}

func (f *fakeIssueRepository) FindIssueByFingerprint(projectID uint32, fingerprint string) (int64, int64, int, bool, error) {
	f.findCalls = append(f.findCalls, fakeFindCall{projectID, fingerprint})
	if f.findErr != nil {
		return 0, 0, 0, false, f.findErr
	}

	issue, ok := f.issues[f.key(projectID, fingerprint)]
	if !ok {
		return 0, 0, 0, false, nil
	}
	return issue.issueID, issue.fingerprintID, issue.status, true, nil
}

func (f *fakeIssueRepository) CreateIssueWithFingerprint(projectID uint32, fingerprint, title, culprit, kind string) (int64, int64, error) {
	f.createCalls = append(f.createCalls, fakeCreateCall{projectID, fingerprint, title, culprit, kind})
	if f.createErr != nil {
		return 0, 0, f.createErr
	}

	issueID := f.nextID
	fingerprintID := f.nextID + 1
	f.nextID += 2

	f.issues[f.key(projectID, fingerprint)] = fakeIssue{
		issueID:       issueID,
		fingerprintID: fingerprintID,
		status:        IssueStatusOpen,
	}

	return issueID, fingerprintID, nil
}

func (f *fakeIssueRepository) ReopenIssue(issueID int64) error {
	f.reopenCalls = append(f.reopenCalls, issueID)
	if f.reopenErr != nil {
		return f.reopenErr
	}

	// Update status in our fake store
	for k, issue := range f.issues {
		if issue.issueID == issueID {
			issue.status = IssueStatusOpen
			f.issues[k] = issue
			break
		}
	}
	return nil
}

// seedIssue pre-populates a known issue for testing find/reopen paths.
func (f *fakeIssueRepository) seedIssue(projectID uint32, fingerprint string, status int) (int64, int64) {
	issueID := f.nextID
	fingerprintID := f.nextID + 1
	f.nextID += 2
	f.issues[f.key(projectID, fingerprint)] = fakeIssue{
		issueID:       issueID,
		fingerprintID: fingerprintID,
		status:        status,
	}
	return issueID, fingerprintID
}

// ---------------------------------------------------------------------------
// Existing validation tests (updated to use new constructor signature)
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Happy-path ingest tests — now possible with fakeIssueRepository
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

func TestServiceIngestStore_NewIssue(t *testing.T) {
	repo := newFakeIssueRepository()
	pebbleChan := make(chan models.Event, 10)
	service := NewService(repo, pebbleChan)

	err := service.IngestStore(42, sampleEventJSON())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify an event was sent to the channel
	if len(pebbleChan) != 1 {
		t.Fatalf("expected 1 event in channel, got %d", len(pebbleChan))
	}

	event := <-pebbleChan

	// Verify event fields
	if event.ProjectID != 42 {
		t.Errorf("expected ProjectID 42, got %d", event.ProjectID)
	}
	if event.EventUUID == "" {
		t.Error("expected non-empty EventUUID")
	}
	if event.Environment != "production" {
		t.Errorf("expected environment 'production', got %q", event.Environment)
	}
	if event.ServerName != "web-1" {
		t.Errorf("expected server_name 'web-1', got %q", event.ServerName)
	}
	if event.Release != "v1.0.0" {
		t.Errorf("expected release 'v1.0.0', got %q", event.Release)
	}
	if event.Level != "error" {
		t.Errorf("expected level 'error', got %q", event.Level)
	}
	if event.Tags["region"] != "us-east" {
		t.Errorf("expected tag region=us-east, got %v", event.Tags)
	}
	if !event.IsNewIssue {
		t.Error("expected IsNewIssue=true for first event")
	}
	if event.IssueID == 0 {
		t.Error("expected non-zero IssueID")
	}
	if event.IssueFingerprintID == 0 {
		t.Error("expected non-zero IssueFingerprintID")
	}

	// Verify repository interactions
	if len(repo.findCalls) != 1 {
		t.Errorf("expected 1 find call, got %d", len(repo.findCalls))
	}
	if len(repo.createCalls) != 1 {
		t.Errorf("expected 1 create call, got %d", len(repo.createCalls))
	}
	if repo.createCalls[0].projectID != 42 {
		t.Errorf("expected create for project 42, got %d", repo.createCalls[0].projectID)
	}
	if repo.createCalls[0].title != "RuntimeError: something broke" {
		t.Errorf("expected title 'RuntimeError: something broke', got %q", repo.createCalls[0].title)
	}
	if repo.createCalls[0].kind != "error" {
		t.Errorf("expected kind 'error', got %q", repo.createCalls[0].kind)
	}
	if len(repo.reopenCalls) != 0 {
		t.Errorf("expected 0 reopen calls, got %d", len(repo.reopenCalls))
	}
}

func TestServiceIngestStore_ExistingOpenIssue(t *testing.T) {
	repo := newFakeIssueRepository()
	pebbleChan := make(chan models.Event, 10)
	service := NewService(repo, pebbleChan)

	// Pre-seed an open issue. We need the fingerprint to match what
	// classifyIssue will compute for sampleEventJSON().
	// First, ingest one event to create the issue.
	err := service.IngestStore(42, sampleEventJSON())
	if err != nil {
		t.Fatalf("first ingest failed: %v", err)
	}
	<-pebbleChan // drain

	// Reset call tracking
	repo.findCalls = nil
	repo.createCalls = nil

	// Ingest the same event again — should find existing issue
	err = service.IngestStore(42, sampleEventJSON())
	if err != nil {
		t.Fatalf("second ingest failed: %v", err)
	}

	event := <-pebbleChan
	if event.IsNewIssue {
		t.Error("expected IsNewIssue=false for existing issue")
	}
	if len(repo.createCalls) != 0 {
		t.Errorf("expected 0 create calls for existing issue, got %d", len(repo.createCalls))
	}
	if len(repo.reopenCalls) != 0 {
		t.Errorf("expected 0 reopen calls for open issue, got %d", len(repo.reopenCalls))
	}
}

func TestServiceIngestStore_ReopensResolvedIssue(t *testing.T) {
	repo := newFakeIssueRepository()
	pebbleChan := make(chan models.Event, 10)
	service := NewService(repo, pebbleChan)

	// First, ingest to create the issue
	err := service.IngestStore(42, sampleEventJSON())
	if err != nil {
		t.Fatalf("first ingest failed: %v", err)
	}
	firstEvent := <-pebbleChan

	// Simulate resolving the issue by updating its status in the fake repo
	for k, issue := range repo.issues {
		if issue.issueID == firstEvent.IssueID {
			issue.status = IssueStatusResolved
			repo.issues[k] = issue
			break
		}
	}

	// Reset tracking
	repo.reopenCalls = nil
	repo.createCalls = nil

	// Ingest again — should find the resolved issue and reopen it
	err = service.IngestStore(42, sampleEventJSON())
	if err != nil {
		t.Fatalf("second ingest failed: %v", err)
	}

	event := <-pebbleChan
	if event.IsNewIssue {
		t.Error("expected IsNewIssue=false for reopened issue")
	}
	if len(repo.reopenCalls) != 1 {
		t.Fatalf("expected 1 reopen call, got %d", len(repo.reopenCalls))
	}
	if repo.reopenCalls[0] != firstEvent.IssueID {
		t.Errorf("expected reopen for issue %d, got %d", firstEvent.IssueID, repo.reopenCalls[0])
	}
	if len(repo.createCalls) != 0 {
		t.Errorf("expected 0 create calls for reopened issue, got %d", len(repo.createCalls))
	}
}

func TestServiceIngestStore_ChannelFull_ReturnsOverloaded(t *testing.T) {
	repo := newFakeIssueRepository()
	pebbleChan := make(chan models.Event) // unbuffered = always full when non-blocking
	service := NewService(repo, pebbleChan)

	err := service.IngestStore(42, sampleEventJSON())
	if !errors.Is(err, ErrOverloaded) {
		t.Fatalf("expected ErrOverloaded, got %v", err)
	}
}

func TestServiceIngestStore_FindError_Propagates(t *testing.T) {
	repo := newFakeIssueRepository()
	repo.findErr = errors.New("db connection lost")
	pebbleChan := make(chan models.Event, 10)
	service := NewService(repo, pebbleChan)

	err := service.IngestStore(42, sampleEventJSON())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, repo.findErr) {
		t.Errorf("expected wrapped db error, got: %v", err)
	}
}

func TestServiceIngestStore_CreateError_Propagates(t *testing.T) {
	repo := newFakeIssueRepository()
	repo.createErr = errors.New("unique constraint violated")
	pebbleChan := make(chan models.Event, 10)
	service := NewService(repo, pebbleChan)

	err := service.IngestStore(42, sampleEventJSON())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, repo.createErr) {
		t.Errorf("expected wrapped create error, got: %v", err)
	}
}

func TestServiceIngestStore_ReopenError_Propagates(t *testing.T) {
	repo := newFakeIssueRepository()
	pebbleChan := make(chan models.Event, 10)
	service := NewService(repo, pebbleChan)

	// Create then resolve
	_ = service.IngestStore(42, sampleEventJSON())
	firstEvent := <-pebbleChan
	for k, issue := range repo.issues {
		if issue.issueID == firstEvent.IssueID {
			issue.status = IssueStatusResolved
			repo.issues[k] = issue
			break
		}
	}

	// Inject reopen error
	repo.reopenErr = errors.New("reopen failed")

	err := service.IngestStore(42, sampleEventJSON())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, repo.reopenErr) {
		t.Errorf("expected wrapped reopen error, got: %v", err)
	}
}

func TestServiceIngestEnvelope_HappyPath(t *testing.T) {
	repo := newFakeIssueRepository()
	pebbleChan := make(chan models.Event, 10)
	service := NewService(repo, pebbleChan)

	eventPayload := `{"message": "Envelope Test", "environment": "staging"}`
	envelope := []byte("{}\n{\"type\":\"event\"}\n" + eventPayload)

	err := service.IngestEnvelope(99, envelope)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pebbleChan) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pebbleChan))
	}

	event := <-pebbleChan
	if event.ProjectID != 99 {
		t.Errorf("expected ProjectID 99, got %d", event.ProjectID)
	}
	if event.Environment != "staging" {
		t.Errorf("expected environment 'staging', got %q", event.Environment)
	}
	if !event.IsNewIssue {
		t.Error("expected IsNewIssue=true")
	}
}

func TestServiceIngestEnvelope_TransactionType(t *testing.T) {
	repo := newFakeIssueRepository()
	pebbleChan := make(chan models.Event, 10)
	service := NewService(repo, pebbleChan)

	eventPayload := `{"message": "Transaction event"}`
	envelope := []byte("{}\n{\"type\":\"transaction\"}\n" + eventPayload)

	err := service.IngestEnvelope(99, envelope)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pebbleChan) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pebbleChan))
	}
}
