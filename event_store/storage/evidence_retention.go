package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/solidtrace/event_store/pkg/logger"
)

const (
	// evidenceSweepBatch is how many rows or objects Evidence Retention
	// reads, deletes and marks at a time.
	evidenceSweepBatch = 500
	// evidenceDeletes is how many objects Evidence Retention deletes at
	// the same time.
	evidenceDeletes = 16
	// unreferencedAfter is how old an object with no row must be before
	// Evidence Retention deletes it. An upload writes its bytes before its
	// row, so a newer object can be an upload in progress.
	unreferencedAfter = 24 * time.Hour
)

// EvidenceSweep is what one run of Evidence Retention deleted.
type EvidenceSweep struct {
	// Deleted is the Evidence files past Evidence Retention: their bytes
	// are deleted and their rows marked deleted.
	Deleted int
	// Unreferenced is the objects with no row: each from an upload that
	// stopped after its bytes and before its row.
	Unreferenced int
}

// DeletePastRetention runs Evidence Retention at now. retention is the
// Evidence Retention in whole days of each Organization
// ((*ConsoleSettings).EvidenceRetention); an Organization that is not in it
// has DefaultEvidenceRetentionDays, and one with 0 keeps its files. For
// each Evidence file older than that, it deletes the bytes, then sets
// deleted_at, so a change of the setting applies to the files already
// there. The row stays, so its link can say when the file was deleted.
// Then it deletes each object under evidence/ that is older than a day and
// that no row names.
//
// A run that stops after the delete of some bytes and before the update of
// their rows leaves the rows as they were. The next run deletes the bytes
// again, which are already gone, and marks the rows.
func (e *EvidenceFiles) DeletePastRetention(ctx context.Context, now time.Time, retention map[int64]int) (EvidenceSweep, error) {
	var sweep EvidenceSweep
	organizations, err := e.organizationsWithFiles(ctx)
	if err != nil {
		return sweep, fmt.Errorf("read the Organizations of Evidence files: %w", err)
	}
	var failed []error
	for _, organizationID := range organizations {
		days, ok := retention[organizationID]
		if !ok {
			days = DefaultEvidenceRetentionDays
		}
		cutoff, ok := evidenceCutoff(now, days)
		if !ok {
			logger.L.Error("Keeping the Evidence files of an Organization with no valid Evidence Retention", "organization_id", organizationID)
			continue
		}
		deleted, err := e.deleteCreatedBefore(ctx, organizationID, cutoff, now)
		sweep.Deleted += deleted
		if err != nil {
			failed = append(failed, fmt.Errorf("Organization %d: %w", organizationID, err))
		}
	}
	sweep.Unreferenced, err = e.deleteUnreferenced(ctx, now.Add(-unreferencedAfter))
	if err != nil {
		failed = append(failed, fmt.Errorf("objects with no row: %w", err))
	}
	return sweep, errors.Join(failed...)
}

// evidenceCutoff is the time before which a file is past an Evidence
// Retention of days at now, or false when days is not valid, as a file is
// then kept.
func evidenceCutoff(now time.Time, days int) (time.Time, bool) {
	if days < 1 {
		return time.Time{}, false
	}
	return now.Add(-time.Duration(days) * 24 * time.Hour), true
}

func (e *EvidenceFiles) organizationsWithFiles(ctx context.Context) ([]int64, error) {
	rows, err := e.db.QueryContext(ctx, "SELECT DISTINCT organization_id FROM evidence_files WHERE deleted_at IS NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var organizations []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		organizations = append(organizations, id)
	}
	return organizations, rows.Err()
}

// deleteCreatedBefore deletes the bytes of an Organization's Evidence files
// created before cutoff, a batch at a time, and marks the rows of the
// deleted bytes deleted at now. It stops at a batch with a failed delete,
// as the next read would give the same rows again.
func (e *EvidenceFiles) deleteCreatedBefore(ctx context.Context, organizationID int64, cutoff, now time.Time) (int, error) {
	deleted := 0
	for {
		ids, keys, err := e.filesCreatedBefore(ctx, organizationID, cutoff)
		if err != nil || len(ids) == 0 {
			return deleted, err
		}
		gone, deleteErr := e.deleteObjects(ctx, keys)
		goneIDs := make([]any, 0, len(ids))
		for i, id := range ids {
			if gone[i] {
				goneIDs = append(goneIDs, id)
			}
		}
		if err := e.markDeleted(ctx, goneIDs, now); err != nil {
			return deleted, errors.Join(deleteErr, err)
		}
		deleted += len(goneIDs)
		if deleteErr != nil {
			return deleted, deleteErr
		}
	}
}

func (e *EvidenceFiles) filesCreatedBefore(ctx context.Context, organizationID int64, cutoff time.Time) ([]string, []string, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT id, storage_key FROM evidence_files
		WHERE organization_id = ? AND created_at < ? AND deleted_at IS NULL
		ORDER BY created_at LIMIT ?`,
		organizationID, cutoff.UTC().Format(railsTimeLayout), evidenceSweepBatch)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var ids, keys []string
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			return nil, nil, err
		}
		ids, keys = append(ids, id), append(keys, key)
	}
	return ids, keys, rows.Err()
}

func (e *EvidenceFiles) markDeleted(ctx context.Context, ids []any, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := e.db.ExecContext(ctx, "UPDATE evidence_files SET deleted_at = ? WHERE deleted_at IS NULL AND id IN "+sqlList(len(ids)),
		append([]any{at.UTC().Format(railsTimeLayout)}, ids...)...)
	return err
}

// deleteObjects deletes the bytes of keys, evidenceDeletes at a time, and
// reports which are gone. Bytes that are already gone are not an error.
func (e *EvidenceFiles) deleteObjects(ctx context.Context, keys []string) ([]bool, error) {
	gone := make([]bool, len(keys))
	errs := make([]error, len(keys))
	slots := make(chan struct{}, evidenceDeletes)
	var wg sync.WaitGroup
	for i, key := range keys {
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			errs[i] = e.deleteBytes(ctx, key)
			gone[i] = errs[i] == nil
		})
	}
	wg.Wait()
	failed := slices.DeleteFunc(errs, func(err error) bool { return err == nil })
	if len(failed) > 0 {
		return gone, fmt.Errorf("%d of %d deletes failed, the first: %w", len(failed), len(keys), failed[0])
	}
	return gone, nil
}

// deleteUnreferenced deletes each object under evidence/ modified before
// cutoff that no row names, other than a deleted one.
func (e *EvidenceFiles) deleteUnreferenced(ctx context.Context, cutoff time.Time) (int, error) {
	objects, err := e.listObjects(ctx)
	if err != nil {
		return 0, err
	}
	var old []string
	for _, object := range objects {
		if object.Modified.Before(cutoff) {
			old = append(old, object.Key)
		}
	}
	deleted := 0
	for batch := range slices.Chunk(old, evidenceSweepBatch) {
		named, err := e.namedKeys(ctx, batch)
		if err != nil {
			return deleted, err
		}
		unnamed := slices.DeleteFunc(batch, func(key string) bool { return named[key] })
		if len(unnamed) == 0 {
			continue
		}
		logger.L.Info("Deleting Evidence objects that have no row", "count", len(unnamed), "first", unnamed[0])
		gone, err := e.deleteObjects(ctx, unnamed)
		for _, ok := range gone {
			if ok {
				deleted++
			}
		}
		if err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

// sqlList is the placeholders of an SQL list of n values: "(?, ?, ?)". It
// panics for no values, as "()" is not valid SQL: a caller with an empty
// list has nothing to query.
func sqlList(n int) string {
	if n < 1 {
		panic("an SQL list needs at least one value")
	}
	return "(" + strings.Repeat("?, ", n-1) + "?)"
}

// namedKeys is the keys of batch that a row which is not deleted names.
// The id of a row is the name of its key, without the extension.
func (e *EvidenceFiles) namedKeys(ctx context.Context, batch []string) (map[string]bool, error) {
	ids := make([]any, len(batch))
	for i, key := range batch {
		ids[i] = strings.TrimSuffix(path.Base(key), path.Ext(key))
	}
	rows, err := e.db.QueryContext(ctx, "SELECT storage_key FROM evidence_files WHERE deleted_at IS NULL AND id IN "+sqlList(len(ids)), ids...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	named := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		named[key] = true
	}
	return named, rows.Err()
}

// listObjects lists the objects under evidence/: in the bucket, or under
// the evidence folder.
func (e *EvidenceFiles) listObjects(ctx context.Context) ([]StoredObject, error) {
	if e.bucket != nil {
		return e.bucket.List(ctx, "evidence/")
	}
	var objects []StoredObject
	root := e.localPath("evidence")
	err := filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			// No evidence folder yet, or a file deleted during the walk.
			return nil
		}
		if err != nil || !entry.Type().IsRegular() {
			return err
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(e.dir, file)
		if err != nil {
			return err
		}
		objects = append(objects, StoredObject{Key: filepath.ToSlash(rel), Size: info.Size(), Modified: info.ModTime()})
		return nil
	})
	return objects, err
}
