package main_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

// textLog is a Log whose text differs in case from another's, isn't ASCII,
// or holds LIKE's wildcards. An empty note or street is one the Log lacks.
type textLog struct{ message, note, street string }

// textLogs are listed newest first. Each but two has its message as its
// note; elsewhere holds its text only in street.
var textLogs = []textLog{
	{"Disk FULL", "Disk FULL", ""},
	{"disk full", "", ""},
	{"ÉCOLE", "ÉCOLE", ""},
	{"école", "école", ""},
	{"Straße", "Straße", ""},
	{"STRASSE", "STRASSE", ""},
	{"100% off", "100% off", ""},
	{"1000 off", "1000 off", ""},
	{"user_id", "user_id", ""},
	{"userXid", "userXid", ""},
	{`C:\tmp`, `C:\tmp`, ""},
	{"C:tmp", "C:tmp", ""},
	{"elsewhere", "", "ÜBER STRAßE"},
}

// postTextLogs sends textLogs as one request, a minute apart, the last of
// them at start plus a minute.
func postTextLogs(t *testing.T, env *TestEnv, start time.Time) {
	t.Helper()
	logs := make([]string, len(textLogs))
	for i, l := range textLogs {
		attributes := map[string]any{}
		if l.note != "" {
			attributes["note"] = l.note
		}
		if l.street != "" {
			attributes["street"] = l.street
		}
		logs[i] = logJSON(t, l.message, start.Add(time.Duration(len(textLogs)-i)*time.Minute), attributes)
	}
	postAndProcess(t, env, logs...)
}

// allTextLogsBut lists textLogs' messages, newest first, but those given.
func allTextLogsBut(but ...string) []string {
	var messages []string
	for _, l := range textLogs {
		if !slices.Contains(but, l.message) {
			messages = append(messages, l.message)
		}
	}
	return messages
}

// sqlMessages runs a Query's readable SQL in SQL mode over [from, to) and
// returns its rows' messages.
func sqlMessages(t *testing.T, env *TestEnv, query string, from, to time.Time) []string {
	t.Helper()
	sql := readableSQL(t, env, query)
	columns, rows := sqlTable(t, env, sqlRequest{SQL: sql, From: from, To: to})
	message := slices.Index(columns, "message")
	if message < 0 {
		t.Fatalf("Readable SQL of %q has no message column: %v\n%s", query, columns, sql)
	}
	got := []string{}
	for _, row := range rows {
		var values []any
		if err := json.Unmarshal([]byte(row), &values); err != nil {
			t.Fatalf("Decode row %s: %v", row, err)
		}
		got = append(got, values[message].(string))
	}
	return got
}

func TestTextMatchesFindTheSameLogsOnTimeAndLate(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	setSQLQueries(t, env, true)

	// At 10:29, hour 10's Logs come on time and yesterday's late.
	yesterday := time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)
	env.Clock.Set(onTestDay(10, 29))
	postTextLogs(t, env, onTestDay(10, 0))
	postTextLogs(t, env, yesterday)

	wants := map[string][]string{
		`where message contains "disk FULL"`:       {"Disk FULL", "disk full"},
		`where message contains "école"`:           {"ÉCOLE", "école"},
		`where message contains "ÉCOLE"`:           {"ÉCOLE", "école"},
		`where message contains "straße"`:          {"Straße"},
		`where message contains "STRASSE"`:         {"STRASSE"},
		`where message contains "100%"`:            {"100% off"},
		`where message contains "r_i"`:             {"user_id"},
		`where message contains "C:\\t"`:           {`C:\tmp`},
		`where message contains "\\"`:              {`C:\tmp`},
		`where message !contains "DISK"`:           allTextLogsBut("Disk FULL", "disk full"),
		`where message !contains "%"`:              allTextLogsBut("100% off"),
		`where message contains_cs "FULL"`:         {"Disk FULL"},
		`where message contains_cs "ÉCOLE"`:        {"ÉCOLE"},
		`where message contains_cs "_"`:            {"user_id"},
		`where message !contains_cs "É"`:           allTextLogsBut("ÉCOLE"),
		`where message startswith "DISK"`:          {"Disk FULL", "disk full"},
		`where message startswith "éc"`:            {"ÉCOLE", "école"},
		`where message startswith "100%"`:          {"100% off"},
		`where message startswith "user_"`:         {"user_id"},
		`where message !startswith "c:\\"`:         allTextLogsBut(`C:\tmp`),
		`where message startswith_cs "Disk"`:       {"Disk FULL"},
		`where message startswith_cs "É"`:          {"ÉCOLE"},
		`where message !startswith_cs "d"`:         allTextLogsBut("disk full"),
		`where message =~ "DISK FULL"`:             {"Disk FULL", "disk full"},
		`where message =~ "École"`:                 {"ÉCOLE", "école"},
		`where message =~ "100_ off"`:              {},
		`where message !~ "c:\\TMP"`:               allTextLogsBut(`C:\tmp`),
		`where message matches regex "^C:.tmp$"`:   {`C:\tmp`},
		`where note contains "FULL"`:               {"Disk FULL"},
		`where note !contains "full"`:              allTextLogsBut("Disk FULL"),
		`where note contains_cs "é"`:               {"école"},
		`where note !contains_cs "É"`:              allTextLogsBut("ÉCOLE"),
		`where note startswith "école"`:            {"ÉCOLE", "école"},
		`where note !startswith "1000"`:            allTextLogsBut("1000 off"),
		`where note startswith_cs "Str"`:           {"Straße"},
		`where note !startswith_cs "user_"`:        allTextLogsBut("user_id"),
		`where note =~ "straße"`:                   {"Straße"},
		`where note !~ "100% OFF"`:                 allTextLogsBut("100% off"),
		`search "disk"`:                            {"Disk FULL", "disk full"},
		`search "ÉCOLE"`:                           {"ÉCOLE", "école"},
		`search "straße"`:                          {"Straße", "elsewhere"},
		`search "über"`:                            {"elsewhere"},
		`search "100%"`:                            {"100% off"},
		`search "_"`:                               {"user_id"},
		`search "\\"`:                              {`C:\tmp`},
		`where note !contains "o" | search "STRA"`: {"Straße", "STRASSE", "elsewhere"},
	}
	tables := map[string]tableCase{
		`summarize n = count(), full = countif(message contains "FULL"), cs = countif(message contains_cs "FULL"), pct = countif(note startswith "100%"), eq = countif(note =~ "ÉCOLE"), nofull = countif(note !contains "full")`: {
			"n,full,cs,pct,eq,nofull", []string{`[13,2,1,1,2,12]`}, false},
		`search "straße" | summarize count()`: {"count_", []string{`[2]`}, false},
		`summarize n = count() by note | where note contains "r_i" or note =~ "C:TMP"`: {
			"note,n", []string{`["C:tmp",1]`, `["user_id",1]`}, true},
	}
	check := func(from time.Time, when string) {
		t.Helper()
		to := from.Add(time.Hour)
		for query, want := range wants {
			got := messages(runQueryPages(t, env, 123, queryRequest{Query: query, From: from, To: to, Limit: 1000}))
			if !slices.Equal(got, want) {
				t.Errorf("%s: %s\n want %v\n got  %v", when, query, want, got)
			}
			if !sqlOnClickHouse {
				continue
			}
			if got := sqlMessages(t, env, query, from, to); !slices.Equal(got, want) {
				t.Errorf("%s, readable SQL: %s\n want %v\n got  %v", when, query, want, got)
			}
		}
		assertTablesIn(t, env, tables, from, to, when)
	}
	check(onTestDay(10, 0), "on time")
	check(yesterday, "a day late")

	env.Reopen(t)
	check(onTestDay(10, 0), "on time, after a restart")
	check(yesterday, "a day late, after a restart")
}
