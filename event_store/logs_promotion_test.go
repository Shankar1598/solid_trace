package main_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/event_store/storage"
)

// promotedNames lists a promoted set's (type, key) pairs as "type key", in
// order.
func promotedNames(set storage.PromotedSet) []string {
	names := make([]string, len(set.Keys))
	for i, key := range set.Keys {
		names[i] = string(key.Type) + " " + key.Key
	}
	return names
}

// attributesByMessage maps each listed Log's message to its Attributes.
func attributesByMessage(logs []listedLog) map[string]map[string]any {
	out := make(map[string]map[string]any, len(logs))
	for _, log := range logs {
		out[log.Message] = log.Attributes
	}
	return out
}

func TestALogsKeysArePromotedForItsDayAndReadBackUnchanged(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	postLogs(t, env, "application/json", `{
		"message": "checkout",
		"time": "2026-10-02T11:59:00Z",
		"user": "ada",
		"status": 200,
		"cached": true,
		"roles": ["admin", "dev"]
	}`)
	env.ProcessLogs(t)

	// Built-in fields aren't Attributes, and arrays are never promoted.
	set := env.LogStore.PromotedSet(123, testNow)
	if got, want := promotedNames(set), []string{"bool cached", "number status", "string user"}; !slices.Equal(got, want) {
		t.Errorf("Expected promoted %v, got %v", want, got)
	}
	if want := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC); !set.Day.Equal(want) {
		t.Errorf("Expected the set of %v, got %v", want, set.Day)
	}
	for _, key := range set.Keys {
		if !key.AddedAt.Equal(testNow) {
			t.Errorf("Expected %s added at %v, got %v", key.Key, testNow, key.AddedAt)
		}
		if key.Partway {
			t.Errorf("Expected %s promoted from the start of the day", key.Key)
		}
	}
	if key, ok := set.Lookup(storage.NumberAttribute, "status"); !ok || key.Column() != "attrs_number.status" {
		t.Errorf("Expected status in column attrs_number.status, got %+v %v", key, ok)
	}

	want := map[string]any{"user": "ada", "status": 200.0, "cached": true, "roles": []any{"admin", "dev"}}
	if got := attributesByMessage(latestLogs(t, env, 123, 10))["checkout"]; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected Attributes %v, got %v", want, got)
	}

	// Another day has a set of its own.
	if got := env.LogStore.PromotedSet(123, testNow.AddDate(0, 0, -1)); len(got.Keys) != 0 {
		t.Errorf("Expected no promoted keys the day before, got %v", promotedNames(got))
	}
}

// logJSON is one Log as a sender would send it: a message, a time and
// Attributes.
func logJSON(t *testing.T, message string, at time.Time, attributes map[string]any) string {
	t.Helper()
	object := map[string]any{"message": message, "time": at.Format(time.RFC3339Nano)}
	for name, value := range attributes {
		object[name] = value
	}
	raw, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("Encode Log: %v", err)
	}
	return string(raw)
}

// numberedKeys returns Attributes named <prefix>000, <prefix>001 … each with
// value.
func numberedKeys(prefix string, n int, value any) map[string]any {
	attributes := make(map[string]any, n)
	for i := range n {
		attributes[fmt.Sprintf("%s%03d", prefix, i)] = value
	}
	return attributes
}

func TestPromotionStopsAt100KeysAndTheRestStayAttributes(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Every built-in field, and 150 keys: built-in fields don't count.
	wide := numberedKeys("k", 150, "v")
	wide["level"] = "info"
	wide["service"] = "web"
	wide["environment"] = "production"
	wide["trace_id"] = "4bf92f3577b34da6a3ce929d0e0e4736"
	wide["span_id"] = "00f067aa0ba902b7"
	postLogs(t, env, "application/json", logJSON(t, "wide", testNow.Add(-time.Minute), wide))
	env.ProcessLogs(t)

	env.Clock.Advance(time.Minute)
	postLogs(t, env, "application/json", logJSON(t, "after", testNow, map[string]any{"late": 1, "k000": "w"}))
	env.ProcessLogs(t)

	set := env.LogStore.PromotedSet(123, testNow)
	if len(set.Keys) != storage.MaxPromotedKeys {
		t.Fatalf("Expected %d promoted keys, got %d", storage.MaxPromotedKeys, len(set.Keys))
	}
	if got, want := promotedNames(set)[99], "string k099"; got != want {
		t.Errorf("Expected the first 100 keys promoted, ending %q, got %q", want, got)
	}
	if _, ok := set.Lookup(storage.NumberAttribute, "late"); ok {
		t.Errorf("Expected a key arriving after the set filled to stay unpromoted")
	}

	attributes := attributesByMessage(latestLogs(t, env, 123, 10))
	wantWide := numberedKeys("k", 150, "v")
	if got := attributes["wide"]; !reflect.DeepEqual(got, wantWide) {
		t.Errorf("Expected all 150 Attributes back, got %d: %v", len(got), got)
	}
	if got, want := attributes["after"], map[string]any{"late": 1.0, "k000": "w"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
}

func TestANewDayKeepsYesterdaysSetWhenItWasNotFull(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	yesterday := testNow.AddDate(0, 0, -1)
	env.Clock.Set(yesterday)
	postLogs(t, env, "application/json", logJSON(t, "yesterday", yesterday, map[string]any{"user": "ada", "status": 200}))
	env.ProcessLogs(t)

	env.Clock.Set(testNow)
	postLogs(t, env, "application/json", logJSON(t, "today", testNow, map[string]any{"region": "eu"}))
	env.ProcessLogs(t)

	today := env.LogStore.PromotedSet(123, testNow)
	if got, want := promotedNames(today), []string{"number status", "string region", "string user"}; !slices.Equal(got, want) {
		t.Errorf("Expected today's set %v, got %v", want, got)
	}
	for _, key := range today.Keys {
		if !key.AddedAt.Equal(testNow) || key.Partway {
			t.Errorf("Expected %s in today's set from its start, at %v, got %+v", key.Key, testNow, key)
		}
	}
	if got, want := promotedNames(env.LogStore.PromotedSet(123, yesterday)), []string{"number status", "string user"}; !slices.Equal(got, want) {
		t.Errorf("Expected yesterday's set unchanged, %v, got %v", want, got)
	}

	// The sets outlive a restart.
	env.Reopen(t)
	if got := env.LogStore.PromotedSet(123, testNow); !reflect.DeepEqual(got, today) {
		t.Errorf("Expected today's set after a restart\n%+v\ngot\n%+v", today, got)
	}
	if got := env.LogStore.PromotedSets(123, yesterday, testNow); len(got) != 2 || !got[0].Day.Before(got[1].Day) {
		t.Errorf("Expected yesterday's and today's sets, oldest first, got %+v", got)
	}

	attributes := attributesByMessage(latestLogs(t, env, 123, 10))
	if got, want := attributes["yesterday"], map[string]any{"user": "ada", "status": 200.0}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
	if got, want := attributes["today"], map[string]any{"region": "eu"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
}

// postAndProcess sends one request of Logs and processes it before the next,
// so each is its own Log processing transaction.
func postAndProcess(t *testing.T, env *TestEnv, logs ...string) {
	t.Helper()
	body := "[" + strings.Join(logs, ",") + "]"
	if counts := postLogs(t, env, "application/json", body); counts.Accepted != len(logs) {
		t.Fatalf("Expected %d Logs accepted, got %+v", len(logs), counts)
	}
	env.ProcessLogs(t)
}

func TestANewDayAfterAFullSetPromotesYesterdaysMostCommonKeys(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	yesterday := testNow.AddDate(0, 0, -1)
	env.Clock.Set(yesterday)

	// The first Log fills the set with k000…k099.
	postAndProcess(t, env, logJSON(t, "first", yesterday, numberedKeys("k", 100, "v")))

	// A burst of 1,000 keys seen once each.
	postAndProcess(t, env,
		logJSON(t, "junk 1", yesterday, numberedKeys("j0", 500, "x")),
		logJSON(t, "junk 2", yesterday, numberedKeys("j1", 500, "x")))

	// Then five Logs, one at a time, carrying k000…k089 and ten keys that
	// came too late to be promoted.
	common := numberedKeys("k", 90, "v")
	for name, value := range numberedKeys("x", 10, 1) {
		common[name] = value
	}
	for i := range 5 {
		postAndProcess(t, env, logJSON(t, fmt.Sprintf("common %d", i), yesterday.Add(time.Duration(i)*time.Second), common))
	}
	if got := env.LogStore.PromotedSet(123, yesterday); len(got.Keys) != 100 {
		t.Fatalf("Expected yesterday's set full, got %d keys", len(got.Keys))
	}
	if _, ok := env.LogStore.PromotedSet(123, yesterday).Lookup(storage.NumberAttribute, "x000"); ok {
		t.Fatalf("Expected x000 not promoted yesterday")
	}

	// Today's first Log re-ranks: the 100 keys most Logs carried yesterday.
	env.Clock.Set(testNow)
	postAndProcess(t, env, logJSON(t, "today", testNow, map[string]any{"x000": 7, "k095": "v"}))

	var want []string
	for i := range 10 {
		want = append(want, fmt.Sprintf("number x%03d", i))
	}
	for i := range 90 {
		want = append(want, fmt.Sprintf("string k%03d", i))
	}
	if got := promotedNames(env.LogStore.PromotedSet(123, testNow)); !slices.Equal(got, want) {
		t.Errorf("Expected today's set to be yesterday's top 100\n%v\ngot\n%v", want, got)
	}

	// Every Log reads back as it was sent, whichever day's set it follows.
	attributes := attributesByMessage(latestLogs(t, env, 123, 100))
	if got, want := attributes["today"], map[string]any{"x000": 7.0, "k095": "v"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
	if got, want := attributes["first"], numberedKeys("k", 100, "v"); !reflect.DeepEqual(got, want) {
		t.Errorf("Expected the first Log's 100 Attributes back, got %v", got)
	}
	wantCommon := numberedKeys("k", 90, "v")
	for name := range numberedKeys("x", 10, 1) {
		wantCommon[name] = 1.0
	}
	if got := attributes["common 4"]; !reflect.DeepEqual(got, wantCommon) {
		t.Errorf("Expected\n%v\ngot\n%v", wantCommon, got)
	}
	if got := len(attributes["junk 2"]); got != 500 {
		t.Errorf("Expected the junk Log's 500 Attributes back, got %d", got)
	}
}

func TestAKeyFirstSeenPartwayThroughADayIsPromotedFromThen(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	early := testNow.Add(-time.Hour)
	env.Clock.Set(early)
	postAndProcess(t, env, logJSON(t, "early", early, map[string]any{"user": "ada"}))

	env.Clock.Set(testNow)
	postAndProcess(t, env, logJSON(t, "later", testNow, map[string]any{"user": "bob", "cart": 3}))

	set := env.LogStore.PromotedSet(123, testNow)
	user, _ := set.Lookup(storage.StringAttribute, "user")
	if user.Partway || !user.AddedAt.Equal(early) {
		t.Errorf("Expected user promoted from the start of the day, at %v, got %+v", early, user)
	}
	cart, ok := set.Lookup(storage.NumberAttribute, "cart")
	if !ok || !cart.Partway || !cart.AddedAt.Equal(testNow) {
		t.Errorf("Expected cart promoted partway through the day, at %v, got %+v %v", testNow, cart, ok)
	}

	env.Reopen(t)
	if got, _ := env.LogStore.PromotedSet(123, testNow).Lookup(storage.NumberAttribute, "cart"); got != cart {
		t.Errorf("Expected %+v after a restart, got %+v", cart, got)
	}
	attributes := attributesByMessage(latestLogs(t, env, 123, 10))
	if got, want := attributes["early"], map[string]any{"user": "ada"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
	if got, want := attributes["later"], map[string]any{"user": "bob", "cart": 3.0}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
}

func TestKeysThatCannotHaveAColumnOfTheirOwnStayAttributes(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// DuckDB column names ignore case, so only one of User and user can be
	// promoted. One key name can carry two types, each promoted on its own.
	postAndProcess(t, env,
		`{"message": "one", "time": "2026-10-02T11:59:00Z", "User": "a", "user": "b", "code": 5, "say \"hi\"": "q", "nul\u0000key": "n"}`,
		`{"message": "two", "time": "2026-10-02T11:59:01Z", "code": "E5"}`)

	set := env.LogStore.PromotedSet(123, testNow)
	if got, want := promotedNames(set), []string{"number code", "string User", "string code", "string say \"hi\""}; !slices.Equal(got, want) {
		t.Errorf("Expected promoted %v, got %v", want, got)
	}

	attributes := attributesByMessage(latestLogs(t, env, 123, 10))
	if got, want := attributes["one"], map[string]any{"User": "a", "user": "b", "code": 5.0, "say \"hi\"": "q", "nul\u0000key": "n"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
	if got, want := attributes["two"], map[string]any{"code": "E5"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
}

func TestLogsEitherSideOfMidnightFollowTheirOwnDaysSet(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProjectKey(t, env.SQLitePath, 456, "other_public_key")

	midnight := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	env.Clock.Set(midnight.Add(30 * time.Second))

	// One batch with the first Logs of two days: today's set starts from
	// yesterday's, made in the same transaction.
	postAndProcess(t, env,
		logJSON(t, "before midnight", midnight.Add(-10*time.Second), map[string]any{"a": 1, "b": "x"}),
		logJSON(t, "after midnight", midnight.Add(10*time.Second), map[string]any{"c": true}))

	if got, want := promotedNames(env.LogStore.PromotedSet(123, midnight.Add(-time.Second))), []string{"number a", "string b"}; !slices.Equal(got, want) {
		t.Errorf("Expected yesterday's set %v, got %v", want, got)
	}
	if got, want := promotedNames(env.LogStore.PromotedSet(123, midnight)), []string{"bool c", "number a", "string b"}; !slices.Equal(got, want) {
		t.Errorf("Expected today's set %v, got %v", want, got)
	}
	attributes := attributesByMessage(latestLogs(t, env, 123, 10))
	if got, want := attributes["before midnight"], map[string]any{"a": 1.0, "b": "x"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
	if got, want := attributes["after midnight"], map[string]any{"c": true}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}

	// Another Project's first Log of yesterday arrives after its first Log
	// of today: yesterday gets a set of its own, and today's is unchanged.
	for _, log := range []string{
		logJSON(t, "today first", midnight.Add(20*time.Second), map[string]any{"t": "1"}),
		logJSON(t, "yesterday after", midnight.Add(-20*time.Second), map[string]any{"y": "2"}),
	} {
		resp := sendLogs(t, env, "/api/456/logs", "application/json", log, map[string]string{"Authorization": "Bearer other_public_key"})
		decodeLogCounts(t, resp)
		env.ProcessLogs(t)
	}
	if got, want := promotedNames(env.LogStore.PromotedSet(456, midnight)), []string{"string t"}; !slices.Equal(got, want) {
		t.Errorf("Expected Project 456's set today %v, got %v", want, got)
	}
	if got, want := promotedNames(env.LogStore.PromotedSet(456, midnight.Add(-time.Second))), []string{"string y"}; !slices.Equal(got, want) {
		t.Errorf("Expected Project 456's set yesterday %v, got %v", want, got)
	}
	attributes = attributesByMessage(latestLogs(t, env, 456, 10))
	if got, want := attributes["yesterday after"], map[string]any{"y": "2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Expected %v, got %v", want, got)
	}
}
