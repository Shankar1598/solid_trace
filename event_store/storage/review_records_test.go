package storage_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/storage"
)

// The rounds of a PR: the Review records of one key, newest first. The key
// is the PR, or the Agent session id of a record with no PR. Newest is the
// highest round, then the latest store. A trim makes a new version of a
// path that repeats its old lines, so only the highest version counts.

var (
	sessionA = uuid.MustParse("0b6c2f1e-6a51-4c8e-9a3d-0f6d1c2b3a41")
	sessionB = uuid.MustParse("2c4e6a8b-0d1f-4e3a-9b5c-7d9f1b3d5e7a")
)

func record(session uuid.UUID, version uint32, offset uint64, pr string, round uint32, hour int) storage.ReviewRecord {
	return storage.ReviewRecord{
		ID: uuid.New(), SessionID: session, Path: "session.jsonl", Version: version, Offset: offset,
		StoredAt: time.Date(2026, 11, 20, hour, 0, 0, 0, time.UTC), PR: pr, Round: round,
	}
}

func roundsOf(pr storage.ReviewRecordPR) []uint32 {
	var rounds []uint32
	for _, r := range pr.Records {
		rounds = append(rounds, r.Round)
	}
	return rounds
}

func TestTheReviewRecordsOfOneKeyAreTheRoundsOfOnePRNewestFirst(t *testing.T) {
	const pr = "https://github.com/acme/shop/pull/7"
	records := []storage.ReviewRecord{
		record(sessionA, 1, 100, pr, 1, 9),
		// Round 3, stored before round 2's record: the round decides.
		record(sessionB, 1, 200, pr, 3, 10),
		record(sessionA, 1, 900, pr, 2, 11),
		record(sessionB, 1, 300, "", 1, 12),
	}
	prs := storage.ReviewRecordsByPR(records)
	if len(prs) != 2 {
		t.Fatalf("Expected two keys, got %+v", prs)
	}
	noPR, withPR := prs[0], prs[1]
	if noPR.Key != sessionB.String() || len(noPR.Records) != 1 {
		t.Errorf("Expected the record with no PR under its Agent session's id, first as stored last, got %+v", noPR)
	}
	if got := roundsOf(withPR); withPR.Key != pr || len(got) != 3 || got[0] != 3 || got[1] != 2 || got[2] != 1 {
		t.Errorf("Expected the rounds 3, 2, 1 of %s, got %v of %q", pr, got, withPR.Key)
	}
}

func TestARecordOfARoundStoredAgainLaterIsTheNewest(t *testing.T) {
	const pr = "local:/work/shop/.git/solidtrace-pr/cart.md"
	earlier, later := record(sessionA, 1, 100, pr, 1, 9), record(sessionB, 1, 50, pr, 1, 10)
	prs := storage.ReviewRecordsByPR([]storage.ReviewRecord{later, earlier})
	if len(prs) != 1 || prs[0].Records[0].ID != later.ID {
		t.Errorf("Expected the later store of round 1 first, got %+v", prs)
	}
}

func TestOnlyTheHighestVersionOfAPathCounts(t *testing.T) {
	const pr = "https://github.com/acme/shop/pull/7"
	old := record(sessionA, 1, 4000, pr, 1, 9)
	trimmed := record(sessionA, 2, 1000, pr, 1, 10)
	other := record(sessionB, 1, 4000, pr, 2, 8)
	prs := storage.ReviewRecordsByPR([]storage.ReviewRecord{old, trimmed, other})
	if len(prs) != 1 || len(prs[0].Records) != 2 || prs[0].Records[0].ID != other.ID || prs[0].Records[1].ID != trimmed.ID {
		t.Errorf("Expected round 2 and the trimmed copy of round 1 only, got %+v", prs)
	}
}
