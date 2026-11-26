package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/logger"
)

func TestTheTypeOfAnEvidenceFileIsFoundFromItsFirstBytes(t *testing.T) {
	for _, c := range []struct {
		name, head, want string
	}{
		{"PNG", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR", "image/png"},
		{"JPEG", "\xff\xd8\xff\xe0\x00\x10JFIF", "image/jpeg"},
		{"GIF 87a", "GIF87a\x01\x00", "image/gif"},
		{"GIF 89a", "GIF89a\x01\x00", "image/gif"},
		{"WebP", "RIFF\x24\x00\x00\x00WEBPVP8 ", "image/webp"},
		{"MP4 of ffmpeg", "\x00\x00\x00\x20ftypisom\x00\x00\x02\x00isomiso2avc1mp41", "video/mp4"},
		{"MP4 with a compatible brand only", "\x00\x00\x00\x18ftypXXXX\x00\x00\x00\x00mp42XXXX", "video/mp4"},
		{"WebM", "\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01\x42\xf7\x81\x01\x42\x82\x84webm\x42\x87\x81\x04", "video/webm"},
		{"Matroska", "\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01\x42\x82\x88matroska", ""},
		{"HEIC", "\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic", ""},
		{"HEIC with isom as its minor version", "\x00\x00\x00\x18ftypheicisommif1heic", ""},
		{"QuickTime", "\x00\x00\x00\x14ftypqt  \x00\x00\x02\x00qt  ", ""},
		{"an ftyp box longer than the bytes", "\x00\x00\x01\x00ftypisom", ""},
		{"a RIFF that is not WebP", "RIFF\x24\x00\x00\x00WAVEfmt ", ""},
		{"SVG", `<svg xmlns="http://www.w3.org/2000/svg">`, ""},
		{"HTML", "<!doctype html>", ""},
		{"PDF", "%PDF-1.7", ""},
		{"empty", "", ""},
	} {
		got, ok := EvidenceContentType([]byte(c.head))
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s: expected %q, got %q (%v)", c.name, c.want, got, ok)
		}
	}
}

// evidenceFilesDDL copies the evidence_files table from console/db/schema.rb,
// which owns it, without its foreign keys, and the columns of projects that
// give Evidence Retention. Keep the unique index in step: the dedup depends
// on it.
const evidenceFilesDDL = `
CREATE TABLE projects (
	id INTEGER PRIMARY KEY,
	organization_id INTEGER NOT NULL,
	agent BOOLEAN NOT NULL DEFAULT 0,
	evidence_retention_days INTEGER NOT NULL DEFAULT 90
);
CREATE TABLE evidence_files (
	id VARCHAR NOT NULL PRIMARY KEY,
	organization_id INTEGER NOT NULL,
	agent_session_id VARCHAR NOT NULL,
	sha256 VARCHAR NOT NULL,
	caption VARCHAR,
	content_type VARCHAR NOT NULL,
	byte_size INTEGER NOT NULL,
	storage_key VARCHAR NOT NULL,
	uploaded_by_user_id INTEGER,
	created_at DATETIME(6) NOT NULL,
	deleted_at DATETIME(6)
);
CREATE UNIQUE INDEX index_evidence_files_on_session_and_sha256 ON evidence_files (organization_id, agent_session_id, sha256)
	WHERE deleted_at IS NULL;
`

// newTestEvidenceFiles keeps Evidence files in a new SQLite with the
// evidence_files table, and their bytes in bucket, or under dir without one.
func newTestEvidenceFiles(t *testing.T, bucket ObjectStore, dir string) *EvidenceFiles {
	t.Helper()
	logger.Init()
	writer := newTestSQLiteWriter(t)
	if _, err := writer.db.Exec(evidenceFilesDDL); err != nil {
		t.Fatalf("Failed to create evidence_files: %v", err)
	}
	files, err := NewEvidenceFiles(writer, bucket, dir)
	if err != nil {
		t.Fatalf("Failed to open the Evidence files: %v", err)
	}
	return files
}

// memoryStore is an ObjectStore in memory. onPut, if set, runs after each
// Put, and failDelete, if set, gives the error of a Delete.
type memoryStore struct {
	mu         sync.Mutex
	objects    map[string][]byte
	modified   map[string]time.Time
	puts       int
	onPut      func()
	failDelete func(key string) error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{objects: map[string][]byte{}, modified: map[string]time.Time{}}
}

func (m *memoryStore) Put(_ context.Context, key, localPath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.objects[key] = data
	m.modified[key] = time.Now()
	m.puts++
	m.mu.Unlock()
	if m.onPut != nil {
		m.onPut()
	}
	return nil
}

func (m *memoryStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failDelete != nil {
		if err := m.failDelete(key); err != nil {
			return err
		}
	}
	delete(m.objects, key)
	return nil
}

func (m *memoryStore) List(_ context.Context, prefix string) ([]StoredObject, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var objects []StoredObject
	for key, data := range m.objects {
		if strings.HasPrefix(key, prefix) {
			objects = append(objects, StoredObject{Key: key, Size: int64(len(data)), Modified: m.modified[key]})
		}
	}
	slices.SortFunc(objects, func(a, b StoredObject) int { return strings.Compare(a.Key, b.Key) })
	return objects, nil
}

func (m *memoryStore) Get(_ context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	data, ok := m.objects[key]
	if !ok {
		return nil, errors.New("no object " + key)
	}
	return io.NopCloser(bytes.NewReader(data[offset : offset+length])), nil
}

func evidencePNG(size int) []byte {
	data := make([]byte, size)
	copy(data, "\x89PNG\r\n\x1a\n")
	for i := 8; i < size; i++ {
		data[i] = byte(i * 7)
	}
	return data
}

func evidenceUpload(data []byte) EvidenceUpload {
	return EvidenceUpload{
		OrganizationID: 1, AgentSessionID: uuid.MustParse("6b1f6c4e-2a3d-4e5f-8a9b-0c1d2e3f4a5b"), UserID: 7,
		ContentType: "image/png", Bytes: data, At: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC),
	}
}

// readEvidenceFile reads length bytes of file from offset.
func readEvidenceFile(t *testing.T, e *EvidenceFiles, file EvidenceFile, offset, length int64) []byte {
	t.Helper()
	reader, err := e.OpenEvidenceFile(context.Background(), file, offset, length)
	if err != nil {
		t.Fatalf("Open Evidence file %s: %v", file.ID, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("Read Evidence file %s: %v", file.ID, err)
	}
	return data
}

// refuseEvidenceInserts makes each insert into evidence_files fail.
func refuseEvidenceInserts(t *testing.T, e *EvidenceFiles) {
	t.Helper()
	if _, err := e.db.Exec(`CREATE TRIGGER refuse_evidence_files BEFORE INSERT ON evidence_files
		BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`); err != nil {
		t.Fatal(err)
	}
}

func TestOpeningEvidenceFilesEmptiesTheirTmpFolderAndKeepsTheStoredFiles(t *testing.T) {
	dir := t.TempDir()
	writer := newTestSQLiteWriter(t)
	if _, err := writer.db.Exec(evidenceFilesDDL); err != nil {
		t.Fatal(err)
	}
	first, err := NewEvidenceFiles(writer, nil, dir)
	if err != nil {
		t.Fatal(err)
	}
	data := evidencePNG(500)
	file, _, err := first.Add(context.Background(), evidenceUpload(data))
	if err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(dir, "tmp", "upload-stopped")
	if err := os.WriteFile(stray, []byte("half a file"), 0o640); err != nil {
		t.Fatal(err)
	}

	again, err := NewEvidenceFiles(writer, nil, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Expected the stray tmp file to be gone, got %v", err)
	}
	if got := readEvidenceFile(t, again, file, 0, file.ByteSize); !bytes.Equal(got, data) {
		t.Errorf("Expected the stored file's %d bytes, got %d", len(data), len(got))
	}
}

func TestASecondUploadOfTheSameBytesWritesNoSecondObject(t *testing.T) {
	store := newMemoryStore()
	e := newTestEvidenceFiles(t, store, t.TempDir())
	upload := evidenceUpload(evidencePNG(500))
	first, created, err := e.Add(context.Background(), upload)
	if err != nil || !created {
		t.Fatalf("Expected a new file, got %v, %v", created, err)
	}
	again, created, err := e.Add(context.Background(), upload)
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("Expected the first row %s, got %s (new %v, %v)", first.ID, again.ID, created, err)
	}
	if store.puts != 1 || len(store.objects) != 1 {
		t.Errorf("Expected 1 put and 1 object, got %d puts and %d objects", store.puts, len(store.objects))
	}
}

func TestTheSameBytesAgainAfterEvidenceRetentionGetANewRowAndNewBytes(t *testing.T) {
	store := newMemoryStore()
	e := newTestEvidenceFiles(t, store, t.TempDir())
	if _, err := e.db.Exec("INSERT INTO projects (organization_id, agent, evidence_retention_days) VALUES (1, 1, 2)"); err != nil {
		t.Fatal(err)
	}
	upload := evidenceUpload(evidencePNG(500))
	first, _, err := e.Add(context.Background(), upload)
	if err != nil {
		t.Fatal(err)
	}

	upload.At = first.CreatedAt.Add(2*24*time.Hour - time.Minute)
	if within, created, err := e.Add(context.Background(), upload); err != nil || created || within.ID != first.ID {
		t.Fatalf("Expected the first row %s within 2 days, got %s (new %v, %v)", first.ID, within.ID, created, err)
	}

	upload.At = first.CreatedAt.Add(2*24*time.Hour + time.Minute)
	again, created, err := e.Add(context.Background(), upload)
	if err != nil || !created || again.ID == first.ID {
		t.Fatalf("Expected a new row past 2 days, got %s (new %v, %v)", again.ID, created, err)
	}
	if old, _, err := e.EvidenceFile(context.Background(), first.ID); err != nil || old.DeletedAt == nil || !old.DeletedAt.Equal(upload.At) {
		t.Errorf("Expected the first row deleted at %v, got %v (%v)", upload.At, old.DeletedAt, err)
	}
	if _, kept := store.objects[first.StorageKey]; kept || len(store.objects) != 1 {
		t.Errorf("Expected only the new bytes, got objects %v", slices.Collect(maps.Keys(store.objects)))
	}
	if got := readEvidenceFile(t, e, again, 0, again.ByteSize); !bytes.Equal(got, upload.Bytes) {
		t.Errorf("Expected the new row's %d bytes, got %d", len(upload.Bytes), len(got))
	}
	if got, err := e.DeletePastRetention(context.Background(), upload.At, map[int64]int{1: 2}); err != nil || got != (EvidenceSweep{}) {
		t.Errorf("Expected the next run of Evidence Retention to delete nothing, got %+v, %v", got, err)
	}
}

func TestAnUploadThatLosesTheRaceToItsRowDeletesItsBytesAndGivesTheWinnersRow(t *testing.T) {
	store := newMemoryStore()
	e := newTestEvidenceFiles(t, store, t.TempDir())
	upload := evidenceUpload(evidencePNG(500))
	sum := sha256.Sum256(upload.Bytes)
	winner := EvidenceFile{
		ID: uuid.New(), OrganizationID: upload.OrganizationID, AgentSessionID: upload.AgentSessionID,
		SHA256: hex.EncodeToString(sum[:]), ContentType: "image/png", ByteSize: 500,
		StorageKey: "evidence/org=1/winner.png", UploadedByUserID: 8, CreatedAt: upload.At,
	}
	store.onPut = func() {
		if inserted, err := e.insert(context.Background(), winner); err != nil || !inserted {
			t.Errorf("Insert the winner's row: %v, %v", inserted, err)
		}
	}
	file, created, err := e.Add(context.Background(), upload)
	if err != nil || created || file.ID != winner.ID || file.UploadedByUserID != 8 {
		t.Fatalf("Expected the winner's row %s, got %+v (new %v, %v)", winner.ID, file, created, err)
	}
	if store.puts != 1 || len(store.objects) != 0 {
		t.Errorf("Expected the loser's 1 put to be deleted, got %d puts and objects %v", store.puts, store.objects)
	}
}

func TestAFailedInsertOfTheRowDeletesTheBytes(t *testing.T) {
	t.Run("in a bucket", func(t *testing.T) {
		store := newMemoryStore()
		e := newTestEvidenceFiles(t, store, t.TempDir())
		refuseEvidenceInserts(t, e)
		if _, _, err := e.Add(context.Background(), evidenceUpload(evidencePNG(500))); err == nil || !strings.Contains(err.Error(), "refused by the test") {
			t.Fatalf("Expected the insert's error, got %v", err)
		}
		if store.puts != 1 || len(store.objects) != 0 {
			t.Errorf("Expected the 1 put to be deleted, got %d puts and objects %v", store.puts, store.objects)
		}
	})
	t.Run("on the data disk", func(t *testing.T) {
		dir := t.TempDir()
		e := newTestEvidenceFiles(t, nil, dir)
		refuseEvidenceInserts(t, e)
		if _, _, err := e.Add(context.Background(), evidenceUpload(evidencePNG(500))); err == nil {
			t.Fatal("Expected the insert's error")
		}
		left, err := filepath.Glob(filepath.Join(dir, "evidence", "org=1", "*"))
		if err != nil || len(left) != 0 {
			t.Errorf("Expected no stored file, got %v (%v)", left, err)
		}
	})
}

func TestEvidenceFilesInABucketOnMinIOAreStoredReadAndDeleted(t *testing.T) {
	store := minIOStore(t, minIOConfig(t))
	ctx := context.Background()
	e := newTestEvidenceFiles(t, store, t.TempDir())
	data := evidencePNG(1000)
	file, created, err := e.Add(ctx, evidenceUpload(data))
	if err != nil || !created {
		t.Fatalf("Expected a new file, got %v, %v", created, err)
	}
	objects, err := store.List(ctx, "evidence/")
	if err != nil || len(objects) != 1 || objects[0].Key != file.StorageKey || objects[0].Size != 1000 {
		t.Fatalf("Expected the one object %s of 1000 bytes, got %v (%v)", file.StorageKey, objects, err)
	}
	if got := readEvidenceFile(t, e, file, 0, 1000); !bytes.Equal(got, data) {
		t.Errorf("Expected the whole file, got %d bytes", len(got))
	}
	if got := readEvidenceFile(t, e, file, 100, 100); !bytes.Equal(got, data[100:200]) {
		t.Errorf("Expected bytes 100-199, got %d bytes", len(got))
	}

	refuseEvidenceInserts(t, e)
	if _, _, err := e.Add(ctx, evidenceUpload(evidencePNG(1001))); err == nil {
		t.Fatal("Expected the insert's error")
	}
	if objects, err := store.List(ctx, "evidence/"); err != nil || len(objects) != 1 || objects[0].Key != file.StorageKey {
		t.Errorf("Expected the failed upload's object to be deleted, got %v (%v)", objects, err)
	}

	// Evidence Retention deletes the file's object, and an object with no
	// row, which the bucket lists with its time.
	if err := store.Put(ctx, "evidence/org=1/"+uuid.NewString()+".png", writeFile(t, "no row")); err != nil {
		t.Fatal(err)
	}
	sweep, err := e.DeletePastRetention(ctx, time.Now().Add(91*24*time.Hour), nil)
	if err != nil || sweep != (EvidenceSweep{Deleted: 1, Unreferenced: 1}) {
		t.Errorf("Expected 1 file and 1 object with no row deleted, got %+v (%v)", sweep, err)
	}
	if objects, err := store.List(ctx, "evidence/"); err != nil || len(objects) != 0 {
		t.Errorf("Expected no objects after Evidence Retention, got %v (%v)", objects, err)
	}
}
