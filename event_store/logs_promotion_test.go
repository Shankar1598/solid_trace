package main_test

import (
	"reflect"
	"slices"
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
