package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// MaxEvidenceFileBytes is the largest Evidence file: 32 MiB, which fits in
// the ingest app's body limit.
const MaxEvidenceFileBytes = 32 << 20

// EvidenceFile is a row of Console's evidence_files table, which EventStore
// writes (ADR 0001; console/db/migrate/20261125120000_create_evidence_files.rb
// owns its schema).
type EvidenceFile struct {
	ID             uuid.UUID
	OrganizationID int64
	AgentSessionID uuid.UUID
	SHA256         string
	Caption        string
	ContentType    string
	ByteSize       int64
	// StorageKey is the key of the bytes in the bucket, or their path under
	// the evidence folder: evidence/org=<id>/<id>.<ext>.
	StorageKey string
	// UploadedByUserID is 0 after Console deletes the user.
	UploadedByUserID int64
	CreatedAt        time.Time
	DeletedAt        *time.Time
}

// EvidenceUpload is one upload of an Evidence file: its bytes, with the
// type that their first bytes have (EvidenceContentType).
type EvidenceUpload struct {
	OrganizationID int64
	AgentSessionID uuid.UUID
	UserID         int64
	Caption        string
	ContentType    string
	Bytes          []byte
	At             time.Time
}

// Console serves the bytes from its own domain, so each other type, as SVG
// or HTML, could run script there.
var evidenceTypes = []struct {
	contentType, extension string
	matches                func(head []byte) bool
}{
	{"image/png", "png", prefix("\x89PNG\r\n\x1a\n")},
	{"image/jpeg", "jpg", prefix("\xff\xd8\xff")},
	{"image/gif", "gif", func(b []byte) bool { return prefix("GIF87a")(b) || prefix("GIF89a")(b) }},
	{"image/webp", "webp", func(b []byte) bool { return len(b) >= 12 && prefix("RIFF")(b) && string(b[8:12]) == "WEBP" }},
	{"video/mp4", "mp4", isMP4},
	{"video/webm", "webm", isWebM},
}

func prefix(magic string) func([]byte) bool {
	return func(b []byte) bool { return bytes.HasPrefix(b, []byte(magic)) }
}

// mp4Brands are the ftyp brands of an MP4 file that a browser plays. HEIC,
// AVIF and QuickTime files have an ftyp box too, with other brands.
var mp4Brands = map[string]bool{
	"isom": true, "iso2": true, "iso4": true, "iso5": true, "iso6": true,
	"mp41": true, "mp42": true, "avc1": true, "dash": true, "M4V ": true,
}

func isMP4(b []byte) bool {
	if len(b) < 12 || string(b[4:8]) != "ftyp" {
		return false
	}
	size := int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	if size < 12 || size > len(b) {
		return false
	}
	majorBrand, compatibleBrands := string(b[8:12]), b[min(16, size):size]
	if mp4Brands[majorBrand] {
		return true
	}
	for i := 0; i+4 <= len(compatibleBrands); i += 4 {
		if mp4Brands[string(compatibleBrands[i:i+4])] {
			return true
		}
	}
	return false
}

// Matroska files have the same EBML header as WebM, with the DocType
// matroska.
func isWebM(b []byte) bool {
	head := b[:min(len(b), 64)]
	docType := bytes.Index(head, []byte("\x42\x82"))
	return prefix("\x1a\x45\xdf\xa3")(head) && docType > 0 &&
		bytes.Contains(head[docType:min(len(head), docType+12)], []byte("webm"))
}

func EvidenceContentType(data []byte) (string, bool) {
	for _, t := range evidenceTypes {
		if t.matches(data) {
			return t.contentType, true
		}
	}
	return "", false
}

func evidenceExtension(contentType string) string {
	for _, t := range evidenceTypes {
		if t.contentType == contentType {
			return t.extension
		}
	}
	panic("no Evidence file type " + contentType)
}

type EvidenceFiles struct {
	db     *sql.DB
	bucket ObjectStore
	dir    string
}

// NewEvidenceFiles keeps the rows through sqlite's write pool, and the
// bytes in bucket, or under dir when bucket is nil. The temporary files of
// uploads go in dir/tmp, which it empties: a file there is from an upload
// that stopped.
func NewEvidenceFiles(sqlite *SQLiteWriter, bucket ObjectStore, dir string) (*EvidenceFiles, error) {
	tmp := filepath.Join(dir, "tmp")
	if err := os.RemoveAll(tmp); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tmp, 0o750); err != nil {
		return nil, err
	}
	return &EvidenceFiles{db: sqlite.db, bucket: bucket, dir: dir}, nil
}

// Add stores an uploaded Evidence file, and reports whether it is new. The
// same bytes again to the same Agent session give the row of the first
// upload, and store nothing, while that row is within its Organization's
// Evidence Retention. A row past it is deleted first, as the next run of
// Evidence Retention would delete it, and the upload gets a new row.
// The bytes are stored before the row, so a row always has its bytes.
func (e *EvidenceFiles) Add(ctx context.Context, upload EvidenceUpload) (EvidenceFile, bool, error) {
	sum := sha256.Sum256(upload.Bytes)
	hash := hex.EncodeToString(sum[:])
	old, found, err := e.find(ctx, upload.OrganizationID, upload.AgentSessionID, hash)
	if err != nil {
		return EvidenceFile{}, false, err
	}
	if found {
		past, err := e.pastRetention(ctx, old, upload.At)
		if err != nil {
			return EvidenceFile{}, false, err
		}
		if !past {
			return old, false, nil
		}
		if err := e.deleteBytes(ctx, old.StorageKey); err != nil {
			return EvidenceFile{}, false, err
		}
		if err := e.markDeleted(ctx, []any{old.ID.String()}, upload.At); err != nil {
			return EvidenceFile{}, false, err
		}
	}
	id := uuid.New()
	file := EvidenceFile{
		ID:               id,
		OrganizationID:   upload.OrganizationID,
		AgentSessionID:   upload.AgentSessionID,
		SHA256:           hash,
		Caption:          upload.Caption,
		ContentType:      upload.ContentType,
		ByteSize:         int64(len(upload.Bytes)),
		StorageKey:       fmt.Sprintf("evidence/org=%d/%s.%s", upload.OrganizationID, id, evidenceExtension(upload.ContentType)),
		UploadedByUserID: upload.UserID,
		CreatedAt:        upload.At.UTC(),
	}
	if err := e.put(ctx, file.StorageKey, upload.Bytes); err != nil {
		return EvidenceFile{}, false, err
	}
	inserted, err := e.insert(ctx, file)
	if err != nil || !inserted {
		if deleteErr := e.deleteBytes(ctx, file.StorageKey); deleteErr != nil {
			err = errors.Join(err, deleteErr)
		}
	}
	if err != nil {
		return EvidenceFile{}, false, err
	}
	if !inserted {
		file, _, err = e.find(ctx, upload.OrganizationID, upload.AgentSessionID, hash)
		return file, false, err
	}
	return file, true, nil
}

func (e *EvidenceFiles) pastRetention(ctx context.Context, file EvidenceFile, now time.Time) (bool, error) {
	days, err := evidenceRetentionOf(ctx, e.db, file.OrganizationID)
	if err != nil {
		return false, fmt.Errorf("read the Evidence Retention of Organization %d: %w", file.OrganizationID, err)
	}
	cutoff, ok := evidenceCutoff(now, days)
	return ok && file.CreatedAt.Before(cutoff), nil
}

func (e *EvidenceFiles) put(ctx context.Context, key string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Join(e.dir, "tmp"), "upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	if err == nil && e.bucket == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write an Evidence file: %w", err)
	}
	if e.bucket != nil {
		return e.bucket.Put(ctx, key, tmp.Name())
	}
	path := e.localPath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (e *EvidenceFiles) deleteBytes(ctx context.Context, key string) error {
	if e.bucket != nil {
		return e.bucket.Delete(ctx, key)
	}
	if err := os.Remove(e.localPath(key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (e *EvidenceFiles) localPath(key string) string {
	return filepath.Join(e.dir, filepath.FromSlash(key))
}

func (e *EvidenceFiles) insert(ctx context.Context, file EvidenceFile) (bool, error) {
	result, err := e.db.ExecContext(ctx, `
		INSERT INTO evidence_files (id, organization_id, agent_session_id, sha256, caption, content_type, byte_size,
			storage_key, uploaded_by_user_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (organization_id, agent_session_id, sha256) WHERE deleted_at IS NULL DO NOTHING`,
		file.ID.String(), file.OrganizationID, file.AgentSessionID.String(), file.SHA256,
		sql.NullString{String: file.Caption, Valid: file.Caption != ""}, file.ContentType, file.ByteSize,
		file.StorageKey, file.UploadedByUserID, file.CreatedAt.Format(railsTimeLayout))
	if err != nil {
		return false, fmt.Errorf("insert Evidence file %s: %w", file.ID, err)
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

const evidenceColumns = `id, organization_id, agent_session_id, sha256, caption, content_type, byte_size, storage_key,
	uploaded_by_user_id, created_at, deleted_at`

func (e *EvidenceFiles) find(ctx context.Context, organizationID int64, sessionID uuid.UUID, hash string) (EvidenceFile, bool, error) {
	return scanEvidenceFile(e.db.QueryRowContext(ctx, "SELECT "+evidenceColumns+` FROM evidence_files
		WHERE organization_id = ? AND agent_session_id = ? AND sha256 = ? AND deleted_at IS NULL`, organizationID, sessionID.String(), hash))
}

func (e *EvidenceFiles) EvidenceFile(ctx context.Context, id uuid.UUID) (EvidenceFile, bool, error) {
	return scanEvidenceFile(e.db.QueryRowContext(ctx, "SELECT "+evidenceColumns+" FROM evidence_files WHERE id = ?", id.String()))
}

// OpenEvidenceFile reads length bytes of file's bytes from offset. The
// caller closes the reader.
func (e *EvidenceFiles) OpenEvidenceFile(ctx context.Context, file EvidenceFile, offset, length int64) (io.ReadCloser, error) {
	if e.bucket != nil {
		return e.bucket.Get(ctx, file.StorageKey, offset, length)
	}
	f, err := os.Open(e.localPath(file.StorageKey))
	if err != nil {
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{io.NewSectionReader(f, offset, length), f}, nil
}

const railsTimeLayout = "2006-01-02 15:04:05.000000"

func scanEvidenceFile(row *sql.Row) (EvidenceFile, bool, error) {
	var file EvidenceFile
	var id, sessionID, createdAt string
	var caption, deletedAt sql.NullString
	var userID sql.NullInt64
	err := row.Scan(&id, &file.OrganizationID, &sessionID, &file.SHA256, &caption, &file.ContentType, &file.ByteSize,
		&file.StorageKey, &userID, &createdAt, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return EvidenceFile{}, false, nil
	}
	if err != nil {
		return EvidenceFile{}, false, err
	}
	if file.ID, err = uuid.Parse(id); err != nil {
		return EvidenceFile{}, false, fmt.Errorf("Evidence file id %q: %w", id, err)
	}
	if file.AgentSessionID, err = uuid.Parse(sessionID); err != nil {
		return EvidenceFile{}, false, fmt.Errorf("Agent session of Evidence file %s: %w", id, err)
	}
	file.Caption = caption.String
	file.UploadedByUserID = userID.Int64
	if file.CreatedAt, err = parseRailsTime(createdAt); err != nil {
		return EvidenceFile{}, false, fmt.Errorf("created_at of Evidence file %s: %w", id, err)
	}
	if deletedAt.Valid {
		at, err := parseRailsTime(deletedAt.String)
		if err != nil {
			return EvidenceFile{}, false, fmt.Errorf("deleted_at of Evidence file %s: %w", id, err)
		}
		file.DeletedAt = &at
	}
	return file, true, nil
}

func parseRailsTime(text string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02 15:04:05.999999999", text, time.UTC)
}
