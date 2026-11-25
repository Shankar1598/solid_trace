package review

import "testing"

func TestOutcomeIsTheFirstThatTheEventsProve(t *testing.T) {
	triage := func(decision string) RecordEvent { return RecordEvent{Type: "triage", Decision: decision} }
	verify := func(pushedBack bool) RecordEvent { return RecordEvent{Type: "verify", PushedBack: &pushedBack} }
	person := func(asks string) RecordEvent { return RecordEvent{Type: "person", Asks: asks} }
	found, refound, fix := RecordEvent{Type: "found"}, RecordEvent{Type: "refound"}, RecordEvent{Type: "fix"}
	cases := []struct {
		name   string
		events []RecordEvent
		want   string
	}{
		{"a fix", []RecordEvent{found, triage("fix"), fix}, "fixed"},
		{"a fix with a trivial decision", []RecordEvent{found, triage("trivial"), fix}, "fixed"},
		{"a fix that a later run found again", []RecordEvent{found, triage("fix"), fix, refound, triage("trivial")}, "ignored_trivial"},
		{"a fix after the run that found it again", []RecordEvent{found, fix, refound, triage("fix"), fix}, "fixed"},
		{"the person ignores it", []RecordEvent{found, triage("fix"), person("ignore")}, "ignored_by_person"},
		{"a fix over the person's ignore", []RecordEvent{found, person("ignore"), fix}, "fixed"},
		{"a dispute", []RecordEvent{found, triage("dispute")}, "disputed"},
		{"a dispute that a verifier pushed back", []RecordEvent{found, triage("dispute"), verify(true)}, "open"},
		{"a dispute that a verifier upheld", []RecordEvent{found, triage("dispute"), verify(false)}, "disputed"},
		{"a defer", []RecordEvent{found, triage("defer")}, "deferred"},
		{"the person says later", []RecordEvent{found, triage("ask"), person("defer")}, "deferred"},
		{"a trivial decision", []RecordEvent{found, triage("trivial")}, "ignored_trivial"},
		{"a trivial decision that a verifier pushed back", []RecordEvent{found, triage("trivial"), verify(true)}, "open"},
		{"a trivial decision again after a push back", []RecordEvent{found, triage("trivial"), verify(true), triage("trivial")}, "ignored_trivial"},
		{"a decision to fix and no fix", []RecordEvent{found, triage("fix")}, "open"},
		{"an ask", []RecordEvent{found, triage("ask")}, "open"},
		{"no decision", []RecordEvent{found}, "open"},
	}
	for _, c := range cases {
		if got := outcome(c.events); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}
