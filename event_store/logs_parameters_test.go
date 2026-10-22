package main_test

import (
	"slices"
	"testing"
)

// filters are the values of a request's Dashboard filters, by name.
type filters = map[string][]string

// filterCase is a Query, the values of the Dashboard filters it names, and
// the messages of the Logs it matches, newest first.
type filterCase struct {
	query  string
	values filters
	want   []string
}

// queryWith runs a Query over all time for Project 123 with Dashboard filter
// values, and returns the messages of the Logs it matches, newest first.
func queryWith(t *testing.T, env *TestEnv, query string, values filters) []string {
	t.Helper()
	return messages(runQueryPages(t, env, 123, queryRequest{
		Query: query, From: allTime.from, To: allTime.to, Limit: 1000, Parameters: values,
	}))
}

// assertFilterCases runs each case and checks its messages.
func assertFilterCases(t *testing.T, env *TestEnv, cases []filterCase, when string) {
	t.Helper()
	for _, c := range cases {
		if got := queryWith(t, env, c.query, c.values); !slices.Equal(got, c.want) {
			t.Errorf("%s: %s with %v\n want %v\n got  %v", when, c.query, c.values, c.want, got)
		}
	}
}

func TestAFiltersValueIsComparedAsIfWrittenInItsPlace(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)
	postAndProcess(t, env, logJSON(t, "gil", languageAt(11, 20), map[string]any{"$type": "order", "service": "shop"}))

	assertFilterCases(t, env, []filterCase{
		{`where service == $service`, filters{"service": {"api"}}, []string{"ed", "cy", "ada"}},
		{`where $service == service`, filters{"service": {"web"}}, []string{"bob"}},
		// $ no longer starts a name, so a Field named $type is written in
		// brackets.
		{`where ['$type'] == "order"`, nil, []string{"gil"}},
		{`where ['$type'] == $type`, filters{"type": {"order"}}, []string{"gil"}},
	}, "in the hot table")
}
