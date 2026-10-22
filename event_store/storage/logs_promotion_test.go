package storage

import (
	"context"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
)

// These tests check the promoted sets Log processing stores, which the query
// compiler reads to decide where each Attribute is. A Query's rows are the
// same whichever keys a set holds, so only the stored sets show the rules of
// ADR 0005.

// promotionProject is the Project these tests store Logs for.
const promotionProject = 123

// october is a time on a day of October 2026, UTC.
func october(day, hour, minute int) time.Time {
	return time.Date(2026, 10, day, hour, minute, 0, 0, time.UTC)
}

// appendLogs stores Logs as one Log batch of the Project, with Log
// processing's clock at now. Its id comes after every earlier batch's, as
// Pebble's do.
func appendLogs(t *testing.T, store *LogStore, now time.Time, logs ...models.Log) {
	t.Helper()
	if err := store.AppendLogBatches(promotionProject, []LogBatch{{ID: uuid.Must(uuid.NewV7()), Logs: logs}}, now); err != nil {
		t.Fatalf("AppendLogBatches: %v", err)
	}
}

// stringLog is a Log at a time whose Attributes are string ones.
func stringLog(at time.Time, attributes map[string]string) models.Log {
	return models.Log{ID: uuid.New(), Time: at, Message: "log", Attributes: models.Attributes{String: attributes}}
}

// numbered returns string Attributes named <prefix>000, <prefix>001 … up to
// but not including <prefix><to>, each "v".
func numbered(prefix string, from, to int) map[string]string {
	attributes := make(map[string]string, to-from)
	for i := from; i < to; i++ {
		attributes[fmt.Sprintf("%s%03d", prefix, i)] = "v"
	}
	return attributes
}

// storedSet returns the Project's promoted set of the UTC day of t, as Log
// processing stored it.
func storedSet(t *testing.T, store *LogStore, day time.Time) []PromotedKey {
	t.Helper()
	sets, err := storedSets(context.Background(), store.db, promotionProject, day, day)
	if err != nil {
		t.Fatalf("storedSets: %v", err)
	}
	return sets[LogDay(day)]
}

// partway maps each key of a set to whether it is partway.
func partway(set []PromotedKey) map[typedKey]bool {
	keys := make(map[typedKey]bool, len(set))
	for _, key := range set {
		keys[typedKey{key.Type, key.Key}] = key.Partway
	}
	return keys
}

// stringKeys maps the string keys of each Attributes to partway.
func stringKeys(partway bool, attributes ...map[string]string) map[typedKey]bool {
	keys := make(map[typedKey]bool)
	for _, a := range attributes {
		for name := range a {
			keys[typedKey{StringAttribute, name}] = partway
		}
	}
	return keys
}

// assertSet checks a day's stored set: its keys, whether each is partway,
// and that each key was added at one of the times in addedAt, by name, or
// else at added.
func assertSet(t *testing.T, store *LogStore, day time.Time, want map[typedKey]bool, added time.Time, addedAt map[string]time.Time) {
	t.Helper()
	set := storedSet(t, store, day)
	if got := partway(set); !maps.Equal(got, want) {
		t.Errorf("The set of %s: expected %d keys\n %v\ngot %d\n %v", dayString(day), len(want), want, len(got), got)
	}
	for _, key := range set {
		wantAt, ok := addedAt[key.Key]
		if !ok {
			wantAt = added
		}
		if !key.AddedAt.Equal(wantAt) {
			t.Errorf("The set of %s: expected %s added at %v, got %v", dayString(day), key.Key, wantAt, key.AddedAt)
		}
	}
}

func TestASetTakesNewKeysUntilItHolds100(t *testing.T) {
	store := newTestLogStore(t)
	now := october(2, 12, 0)

	// One Log with 150 keys and a JSON Attribute: the set takes the first
	// 100 keys by name, and never a JSON one.
	appendLogs(t, store, now, models.Log{
		ID: uuid.New(), Time: now.Add(-time.Minute), Message: "wide",
		Attributes: models.Attributes{String: numbered("k", 0, 150), JSON: map[string]string{"roles": `["admin"]`}},
	})
	// Keys after the set is full stay out of it.
	appendLogs(t, store, now.Add(time.Minute), stringLog(now, map[string]string{"late": "x", "k120": "w"}))

	want := stringKeys(false, numbered("k", 0, MaxPromotedKeys))
	assertSet(t, store, now, want, now, nil)
	if got := len(storedSet(t, store, now)); got != MaxPromotedKeys {
		t.Errorf("Expected %d keys, got %d", MaxPromotedKeys, got)
	}
}

func TestANewDayStartsWithTheLastSetWhenItWasNotFull(t *testing.T) {
	store := newTestLogStore(t)
	yesterday, today := october(1, 12, 0), october(2, 12, 0)

	appendLogs(t, store, yesterday, models.Log{
		ID: uuid.New(), Time: yesterday, Message: "yesterday",
		Attributes: models.Attributes{String: map[string]string{"user": "ada"}, Number: map[string]float64{"status": 200}},
	})
	appendLogs(t, store, today, stringLog(today, map[string]string{"region": "eu"}))
	// A key the day's stored set gains later is partway: the day's Logs
	// stored before hold it in their maps.
	later := today.Add(5 * time.Minute)
	appendLogs(t, store, later, stringLog(later, map[string]string{"cart": "1"}))

	status := typedKey{NumberAttribute, "status"}
	assertSet(t, store, yesterday, map[typedKey]bool{{StringAttribute, "user"}: false, status: false}, yesterday, nil)
	want := stringKeys(false, map[string]string{"user": "", "region": ""})
	want[status] = false
	want[typedKey{StringAttribute, "cart"}] = true
	assertSet(t, store, today, want, today, map[string]time.Time{"cart": later})
}

func TestADaysSetMadeInTheBatchThatMakesTheDayBeforesStartsFromIt(t *testing.T) {
	store := newTestLogStore(t)
	midnight := october(2, 0, 0)
	now := midnight.Add(30 * time.Second)

	// One batch with the first Logs of two days, and later a Log of the day
	// before with a key it hasn't got: its set takes it partway, and today's
	// set, made already, doesn't.
	appendLogs(t, store, now,
		models.Log{ID: uuid.New(), Time: midnight.Add(-10 * time.Second), Message: "before midnight",
			Attributes: models.Attributes{String: map[string]string{"b": "x"}, Number: map[string]float64{"a": 1}}},
		models.Log{ID: uuid.New(), Time: midnight.Add(10 * time.Second), Message: "after midnight",
			Attributes: models.Attributes{Bool: map[string]bool{"c": true}, Number: map[string]float64{"a": 2}}})
	appendLogs(t, store, now, stringLog(midnight.Add(-5*time.Second), map[string]string{"d": "y"}))

	a, b, c := typedKey{NumberAttribute, "a"}, typedKey{StringAttribute, "b"}, typedKey{BoolAttribute, "c"}
	assertSet(t, store, midnight.Add(-time.Second), map[typedKey]bool{a: false, b: false, {StringAttribute, "d"}: true}, now, nil)
	assertSet(t, store, midnight, map[typedKey]bool{a: false, b: false, c: false}, now, nil)
}

func TestANewDayAfterAFullSetTakesTheKeysMostLogsCarriedTheDayBefore(t *testing.T) {
	store := newTestLogStore(t)
	yesterday, today := october(1, 12, 0), october(2, 12, 0)

	// Yesterday's set fills with k000…k099. Then three Logs carry k000…k089
	// and x000…x009, which came too late for it.
	appendLogs(t, store, yesterday, stringLog(yesterday, numbered("k", 0, 100)))
	common := numbered("k", 0, 90)
	maps.Copy(common, numbered("x", 0, 10))
	for i := range 3 {
		appendLogs(t, store, yesterday, stringLog(yesterday.Add(time.Duration(i+1)*time.Second), common))
	}
	appendLogs(t, store, today, stringLog(today, map[string]string{"region": "eu"}))

	// Four Logs carried k000…k089, three x000…x009 and one k090…k099, so
	// today's set is the 100 most common, full: region stays out.
	assertSet(t, store, today, stringKeys(false, numbered("k", 0, 90), numbered("x", 0, 10)), today, nil)
}

func TestANewDaysKeysArePartwayOnlyIfItsFilesOrHotLogsMayHoldThemInTheirMaps(t *testing.T) {
	now := october(2, 12, 29)
	late := func(t *testing.T, store *LogStore) {
		appendLogs(t, store, now, stringLog(october(2, 10, 10), map[string]string{"user": "ada", "plan": "pro"}))
	}
	export := func(t *testing.T, store *LogStore) {
		if err := store.ExportLogs(now); err != nil {
			t.Fatalf("ExportLogs: %v", err)
		}
	}
	cases := []struct {
		name    string
		before  func(*testing.T, *LogStore)
		partway bool
	}{
		{
			// A read takes them from the maps, and the export pulls their
			// values into the columns of the set the day has then.
			name:    "its first Logs are late and still in logs_late",
			before:  late,
			partway: false,
		},
		{
			name: "its first Logs are late and in hour 10's file",
			before: func(t *testing.T, store *LogStore) {
				late(t, store)
				export(t, store)
			},
			partway: true,
		},
		{
			// Yesterday's set, which today's starts from, has user.
			name: "its first Logs are late and in hour 10's file, after a set the day before",
			before: func(t *testing.T, store *LogStore) {
				yesterday := october(1, 12, 0)
				appendLogs(t, store, yesterday, stringLog(yesterday, map[string]string{"user": "yan"}))
				late(t, store)
				export(t, store)
			},
			partway: true,
		},
		{
			// Such a Log makes no set, and holds no key a set could take.
			name: "its first hot Log has no Attributes",
			before: func(t *testing.T, store *LogStore) {
				appendLogs(t, store, now, models.Log{ID: uuid.New(), Time: october(2, 12, 5), Message: "app started"})
			},
			partway: false,
		},
		{
			name: "its first hot Log has only a JSON Attribute",
			before: func(t *testing.T, store *LogStore) {
				appendLogs(t, store, now, models.Log{ID: uuid.New(), Time: october(2, 12, 5), Message: "roles",
					Attributes: models.Attributes{JSON: map[string]string{"roles": `["admin"]`}}})
			},
			partway: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := newTestLogStore(t)
			c.before(t, store)
			appendLogs(t, store, now, stringLog(october(2, 12, 10), map[string]string{"user": "cy", "plan": "free"}))
			assertSet(t, store, now, stringKeys(c.partway, map[string]string{"user": "", "plan": ""}), now, nil)
		})
	}
}

func TestANewDaysKeysArePartwayIfItsHotLogsKeepAnAttributeInAMap(t *testing.T) {
	store := newTestLogStore(t)
	midnight := october(3, 0, 0)
	now := midnight.Add(10 * time.Minute)

	// Today's set takes User. Then yesterday's first Log arrives, still
	// hot: its day has no set to start from, and its user can't have a
	// column, as User has the name in another case. So yesterday's day
	// has no set, and its hot Log keeps user in its map.
	appendLogs(t, store, now, stringLog(midnight.Add(5*time.Minute), map[string]string{"User": "ada"}))
	appendLogs(t, store, now, stringLog(midnight.Add(-10*time.Minute), map[string]string{"user": "cy"}))
	if set := storedSet(t, store, midnight.Add(-time.Minute)); len(set) != 0 {
		t.Fatalf("Expected yesterday to have no set yet, got %v", set)
	}

	// A key the day's new set takes is partway: that Log is stored without it.
	appendLogs(t, store, now, stringLog(midnight.Add(-5*time.Minute), map[string]string{"plan": "free"}))
	assertSet(t, store, midnight.Add(-time.Minute), stringKeys(true, map[string]string{"plan": ""}), now, nil)
}
