package main_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
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

func TestSeveralValuesMatchAnyOrNoneAndAllDropsTheComparison(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)

	everyLog := []string{"ed", "cy", "bob", "ada", "di"}
	cases := []filterCase{
		{`where service == $service`, filters{"service": {"api", "web"}}, []string{"ed", "cy", "bob", "ada"}},
		{`where service != $service`, filters{"service": {"api"}}, []string{"bob", "di"}},
		{`where service != $service`, filters{"service": {"api", "web"}}, []string{"di"}},
		// Text operators, case-insensitive unless _cs.
		{`where path contains $p`, filters{"p": {"USERS"}}, []string{"ed", "ada"}},
		{`where path contains $p`, filters{"p": {"users", "health"}}, []string{"ed", "bob", "ada"}},
		{`where path !contains $p`, filters{"p": {"users", "health"}}, []string{"cy", "di"}},
		{`where path startswith $p`, filters{"p": {"/api"}}, []string{"ed", "cy", "ada"}},
		{`where path startswith_cs $p`, filters{"p": {"/api"}}, []string{"cy", "ada"}},
		{`where path !startswith $p`, filters{"p": {"/api", "/health"}}, []string{"di"}},
		{`where user =~ $u`, filters{"u": {"bob"}}, []string{"bob"}},
		{`where $u =~ user`, filters{"u": {"ED"}}, []string{"ed"}},
		{`where user !~ $u`, filters{"u": {"BOB", "ada"}}, []string{"ed", "cy", "di"}},
		{`where tolower(user) == $u`, filters{"u": {"bob"}}, []string{"bob"}},
		// All, no values or only the empty string, keeps every Log the rest
		// of the Query keeps.
		{`where service == $service and level == "error"`, nil, []string{"ed", "cy"}},
		{`where service == $service and level == "error"`, filters{"service": {""}}, []string{"ed", "cy"}},
		{`where level == "error" and path !contains $p`, filters{"p": {}}, []string{"ed", "cy"}},
		{`where service == $service or level == "error"`, nil, everyLog},
		{`where service == $service`, filters{"other": {"web"}}, everyLog},
		// search $q searches for any of its values.
		{`search $q`, filters{"q": {"disk"}}, []string{"ed"}},
		{`search $q`, filters{"q": {"disk", "HEALTH"}}, []string{"ed", "bob"}},
		{`search $q | where service == "api"`, nil, []string{"ed", "cy", "ada"}},
	}
	assertFilterCases(t, env, cases, "in the hot table")
	exportLanguageLogs(t, env)
	assertFilterCases(t, env, []filterCase{
		{`where path contains $p`, filters{"p": {"users"}}, []string{"ed", "fy", "ada"}},
		{`where service != $service`, filters{"service": {"api"}}, []string{"bob", "di"}},
	}, "in files")

	// A filter set to All compiles to the SQL of the Query without its
	// comparison; readable SQL compiles every filter as All.
	for query, without := range map[string]string{
		`where service == $service and level == "error"`:           `where level == "error"`,
		`where service == $service | summarize count() by service`: `summarize count() by service`,
		`summarize n = count() by service | where n == $n`:         `summarize n = count() by service`,
		`search $q | where user contains $u | take 5`:              `take 5`,
	} {
		if got, want := readableSQL(t, env, query), readableSQL(t, env, without); got != want {
			t.Errorf("%s with every filter All\n want %s\n got  %s", query, want, got)
		}
	}
}

// tableWith runs a Query over all time for Project 123 with Dashboard filter
// values, and returns its table's rows as JSON.
func tableWith(t *testing.T, env *TestEnv, query string, values filters) []string {
	t.Helper()
	status, raw := postQueryApp(t, env, "/api/123/logs/query", queryRequest{
		Query: query, From: allTime.from, To: allTime.to, Parameters: values,
	})
	if status != 200 {
		t.Fatalf("Query %q expected 200, got %d: %s", query, status, raw)
	}
	var answer tableAnswer
	if err := json.Unmarshal(raw, &answer); err != nil || answer.Table == nil {
		t.Fatalf("Decode table %q: %v", raw, err)
	}
	rows := []string{}
	for _, row := range answer.Table.Rows {
		rows = append(rows, string(row))
	}
	return rows
}

func TestAFiltersValueIsTextOrTheNumberOrBoolItReadsAs(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedLanguageLogs(t, env)
	postAndProcess(t, env,
		logJSON(t, "gil", languageAt(11, 20), map[string]any{"service": "7", "cached": true}),
		logJSON(t, "hal", languageAt(11, 25), map[string]any{"cached": "true"}),
	)

	assertFilterCases(t, env, []filterCase{
		// An Attribute matches the text, or the number or bool it reads as.
		{`where status == $status`, filters{"status": {"200"}}, []string{"bob", "ada"}},
		{`where status != $status`, filters{"status": {"200"}}, []string{"hal", "gil", "ed", "cy", "di"}},
		{`where status == $status`, filters{"status": {"200", "404"}}, []string{"ed", "bob", "ada"}},
		{`where status != $status`, filters{"status": {"200", "404"}}, []string{"hal", "gil", "cy", "di"}},
		{`where status =~ $status`, filters{"status": {"500"}}, []string{"di"}},
		{`where cached == $cached`, filters{"cached": {"true"}}, []string{"hal", "gil"}},
		{`where status == $status`, filters{"status": {"2e2"}}, []string{"ada"}},
		// A built-in field compares as text.
		{`where service == $service`, filters{"service": {"7"}}, []string{"gil"}},
		{`where service == $service`, filters{"service": {"7.0"}}, nil},
		{`where level == $level`, filters{"level": {"error"}}, []string{"ed", "cy"}},
	}, "in the hot table")

	// After summarize, a number column compares with the number the value
	// reads as, and a value that isn't one matches nothing.
	for _, c := range []struct {
		query  string
		values filters
		want   []string
	}{
		{`summarize n = count() by service | where n == $n | project service`, filters{"n": {"3"}}, []string{`["api"]`}},
		{`summarize n = count() by service | where n == $n | project service`, filters{"n": {"three"}}, []string{}},
		{`summarize n = count() by service | where n != $n | project service`, filters{"n": {"1"}}, []string{`["api"]`}},
		{`summarize n = count() by service | where service == $s | project n`, filters{"s": {"web"}}, []string{`[1]`}},
	} {
		if got := tableWith(t, env, c.query, c.values); !slices.Equal(got, c.want) {
			t.Errorf("%s with %v\n want %v\n got  %v", c.query, c.values, c.want, got)
		}
	}
}

func TestFiltersGoOnlyInComparisonsAndSearch(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	misplaced := func(name string) string {
		return "$" + name + " only goes in a where, compared with ==, !=, =~, !~, contains or startswith, or in search $" + name
	}
	mistakes := map[string]queryProblem{
		`project $s`:                    {Message: misplaced("s"), Line: 1, Column: 9, EndLine: 1, EndColumn: 11},
		`extend x = $s`:                 {Message: misplaced("s"), Line: 1, Column: 12, EndLine: 1, EndColumn: 14},
		`summarize count() by $s`:       {Message: misplaced("s"), Line: 1, Column: 22, EndLine: 1, EndColumn: 24},
		`sort by $s`:                    {Message: misplaced("s"), Line: 1, Column: 9, EndLine: 1, EndColumn: 11},
		`take $n`:                       {Message: misplaced("n"), Line: 1, Column: 6, EndLine: 1, EndColumn: 8},
		`top $n by status`:              {Message: misplaced("n"), Line: 1, Column: 5, EndLine: 1, EndColumn: 7},
		`where status in ($s)`:          {Message: misplaced("s"), Line: 1, Column: 18, EndLine: 1, EndColumn: 20},
		`where duration > $d`:           {Message: misplaced("d"), Line: 1, Column: 18, EndLine: 1, EndColumn: 20},
		`where user matches regex $r`:   {Message: misplaced("r"), Line: 1, Column: 26, EndLine: 1, EndColumn: 28},
		`where $s contains "x"`:         {Message: misplaced("s"), Line: 1, Column: 7, EndLine: 1, EndColumn: 9},
		`where isnull($s)`:              {Message: misplaced("s"), Line: 1, Column: 14, EndLine: 1, EndColumn: 16},
		`summarize countif(a == $s)`:    {Message: misplaced("s"), Line: 1, Column: 24, EndLine: 1, EndColumn: 26},
		"where a == 1\n| where $s":      {Message: misplaced("s"), Line: 2, Column: 9, EndLine: 2, EndColumn: 11},
		`where not(service == $s)`:      {Message: `$s can't go inside not(…), where All would keep nothing; exclude with != instead`, Line: 1, Column: 22, EndLine: 1, EndColumn: 24},
		`where not(path contains $p)`:   {Message: `$p can't go inside not(…), where All would keep nothing; exclude with != instead`, Line: 1, Column: 25, EndLine: 1, EndColumn: 27},
		`where $a == $b`:                {Message: `compare $b with a field, as in service == $b`, Line: 1, Column: 13, EndLine: 1, EndColumn: 15},
		`where service == $`:            {Message: `expected a filter's name after $, such as $service`, Line: 1, Column: 18, EndLine: 1, EndColumn: 19},
		`summarize count() | search $q`: {Message: `search only comes before other stages; after them, use where … contains`, Line: 1, Column: 19, EndLine: 1, EndColumn: 30},
	}
	for query, want := range mistakes {
		v := validateQuery(t, env, query)
		if len(v.Errors) != 1 || v.Errors[0] != want || len(v.Parameters) != 0 || v.Parameters == nil {
			t.Errorf("%q: expected %+v and no parameters, got %+v", query, want, v)
		}
	}

	// A run places the mistake as validate does, whatever the values.
	status, raw := postQueryApp(t, env, "/api/123/logs/query", queryRequest{
		Query: `where duration > $d`, From: allTime.from, To: allTime.to, Parameters: filters{"d": {"100"}},
	})
	if want := `{"errors":[{"message":"` + misplaced("d") + `","line":1,"column":18,"end_line":1,"end_column":20}]}`; status != 400 || string(raw) != want {
		t.Errorf("Run of a misplaced filter expected 400 %s, got %d %s", want, status, raw)
	}
}

func TestValidateNamesTheFiltersAndShowsThemAsChips(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	parameters := map[string][]string{
		`where level == "error"`: {},
		`where service == $service | search $q | where level == $service or path contains $p`: {"service", "q", "p"},
		`where ['$type'] == "order"`: {},
	}
	for query, want := range parameters {
		if v := validateQuery(t, env, query); len(v.Errors) != 0 || v.Parameters == nil || !slices.Equal(v.Parameters, want) {
			t.Errorf("%q: expected parameters %v, got %+v", query, want, v)
		}
	}

	chips := map[string]string{
		`where service == $service | where path !contains $p | where status != $status`: `{"filters":[` +
			`{"field":"service","op":"==","value":{"filter":"service"}},` +
			`{"field":"path","op":"!contains","value":{"filter":"p"}},` +
			`{"field":"status","op":"!=","value":{"filter":"status"}}],"search":null}`,
		`where level == "error" | search $q`: `{"filters":[{"field":"level","op":"==","value":"error"}],"search":null,"search_filter":"q"}`,
		`search "timeout"`:                   `{"filters":[],"search":"timeout"}`,
		`where ['$type'] startswith $t`:      `{"filters":[{"field":"$type","op":"startswith","value":{"filter":"t"}}],"search":null}`,
	}
	for query, want := range chips {
		if got := builderChips(validateQuery(t, env, query)); got != want {
			t.Errorf("%q: expected chips %s, got %s", query, want, got)
		}
	}
	// Comparisons the Builder can't show as chips.
	for _, query := range []string{
		`where $service == service`,
		`where service =~ $service`,
		`where path contains_cs $p`,
		`search $q | search "x"`,
		`search $q | search $r`,
	} {
		if got := builderChips(validateQuery(t, env, query)); got != "null" {
			t.Errorf("%q: expected no chips, got %s", query, got)
		}
	}
}

func TestARunRefusesTooManyOrTooLongValues(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	many := make([]string, 101)
	for i := range many {
		many[i] = fmt.Sprint(i)
	}
	long := strings.Repeat("x", 1001)
	refused := map[string]filters{
		`{"error":"parameters: service has more than 100 values"}`:       {"service": many},
		`{"error":"parameters: a value of q is longer than 1000 bytes"}`: {"q": {"a", long}},
	}
	for want, values := range refused {
		status, raw := postQueryApp(t, env, "/api/123/logs/query", queryRequest{
			Query: `where service == $service | search $q`, From: allTime.from, To: allTime.to, Parameters: values,
		})
		if status != 400 || string(raw) != want {
			t.Errorf("Expected 400 %s, got %d %s", want, status, raw)
		}
	}
	queryWith(t, env, `where service == $service | search $q`, filters{"service": many[:100], "q": {long[:1000]}})
}

// binnedTableWith runs a Query over binCacheRange with Dashboard filter
// values, and returns its table as EventStore answered it, and the bins the
// run took from the bin cache and missed.
func binnedTableWith(t *testing.T, env *TestEnv, query string, values filters) (table string, hits, misses int64) {
	t.Helper()
	hitsBefore, missesBefore := binCacheStats(env)
	status, raw := postQueryApp(t, env, "/api/123/logs/query", queryRequest{
		Query: query, From: binCacheRange.from, To: binCacheRange.to, Parameters: values,
	})
	if status != 200 {
		t.Fatalf("Query %q expected 200, got %d: %s", query, status, raw)
	}
	var answer map[string]json.RawMessage
	if err := json.Unmarshal(raw, &answer); err != nil || answer["table"] == nil {
		t.Fatalf("Query %q expected a table, got %s (%v)", query, raw, err)
	}
	hitsAfter, missesAfter := binCacheStats(env)
	return string(answer["table"]), hitsAfter - hitsBefore, missesAfter - missesBefore
}

func TestEachSetOfFilterValuesHasItsOwnBinsAndAllSharesTheQuerysWithout(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	postChartLogs(t, env)
	env.ExportLogs(t)

	query := `where user == $user | summarize count() by bin(time, 1h)`
	ada, _, adaMisses := binnedTableWith(t, env, query, filters{"user": {"ada"}})
	if adaMisses == 0 {
		t.Fatalf("Expected ada's first run to fill the bin cache")
	}
	if _, hits, misses := binnedTableWith(t, env, query, filters{"user": {"ada"}}); hits != adaMisses || misses != 0 {
		t.Errorf("Expected ada's second run to take its %d bins from the cache, got %d hits and %d misses", adaMisses, hits, misses)
	}
	bob, hits, misses := binnedTableWith(t, env, query, filters{"user": {"bob"}})
	if hits != 0 || misses != adaMisses || bob == ada {
		t.Errorf("Expected bob's run to miss %d bins of its own, got %d hits and %d misses, and a table of its own", adaMisses, hits, misses)
	}

	// All takes the bins the Query without the comparison filled.
	without, _, withoutMisses := binnedTableWith(t, env, `summarize count() by bin(time, 1h)`, nil)
	all, hits, misses := binnedTableWith(t, env, query, nil)
	if hits != withoutMisses || misses != 0 || all != without {
		t.Errorf("Expected All to take the %d bins of the Query without the comparison, got %d hits and %d misses", withoutMisses, hits, misses)
	}

	env.LogStore.UseBinCache(0)
	for user, cached := range map[string]string{"ada": ada, "bob": bob} {
		if uncached, _, _ := binnedTableWith(t, env, query, filters{"user": {user}}); uncached != cached {
			t.Errorf("%s for %s\n from the bin cache: %s\n without it:         %s", query, user, cached, uncached)
		}
	}
}
