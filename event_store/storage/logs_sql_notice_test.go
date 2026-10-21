package storage

import (
	"database/sql"
	"encoding/json"
	"testing"
)

// parsedSelect is a SELECT as checkReadOnly gets it from DuckDB.
func parsedSelect(t *testing.T, db *sql.DB, query string) any {
	t.Helper()
	var serialized string
	if err := db.QueryRow("SELECT CAST(json_serialize_sql(CAST(? AS VARCHAR)) AS VARCHAR)", query).Scan(&serialized); err != nil {
		t.Fatalf("Parse %q: %v", query, err)
	}
	var parsed parsedSQL
	if err := json.Unmarshal([]byte(serialized), &parsed); err != nil || parsed.Error || len(parsed.Statements) != 1 {
		t.Fatalf("Parse %q: %v %s", query, err, serialized)
	}
	return parsed.Statements[0]
}

// ILIKE on message, and LIKE with ESCAPE, run several times slower than
// DuckDB's string functions, so a SQL query that writes them is told what
// to write instead; one DuckDB already runs fast, or with no faster form,
// isn't.
func TestSQLThatMatchesMessageSlowlyIsToldWhatToWriteInstead(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const regexp = "is slow on Logs: regexp_matches(message, '(?i)…'), with . for _ and .* for %, is faster."
	cases := []struct {
		sql, notice string
	}{
		{"SELECT * FROM logs WHERE message ILIKE '%Timeout%'",
			"message ILIKE '%Timeout%' is slow on Logs: contains(lower(message), 'timeout') matches the same Logs several times faster."},
		{"SELECT count(*) FROM logs l WHERE l.level = 'error' AND l.message NOT ILIKE '%it''s%'",
			"message NOT ILIKE '%it''s%' is slow on Logs: NOT contains(lower(message), 'it''s') matches the same Logs several times faster."},
		{"SELECT * FROM logs WHERE message ~~* '%a%'",
			"message ILIKE '%a%' is slow on Logs: contains(lower(message), 'a') matches the same Logs several times faster."},
		{`SELECT * FROM logs WHERE message LIKE '%50\%%' ESCAPE '\'`,
			`message LIKE '%50\%%' ESCAPE '\' is slow on Logs: regexp_matches(message, '…'), with . for _ and .* for %, is faster.`},
		{`SELECT * FROM logs WHERE message NOT LIKE '%done%' ESCAPE '\'`,
			`message NOT LIKE '%done%' ESCAPE '\' is slow on Logs: NOT contains(message, 'done') matches the same Logs several times faster.`},
		{`SELECT * FROM logs WHERE message ILIKE '%done%' ESCAPE '\'`,
			`message ILIKE '%done%' ESCAPE '\' is slow on Logs: contains(lower(message), 'done') matches the same Logs several times faster.`},
		{"SELECT * FROM logs WHERE message ILIKE '%user_42%'", "message ILIKE '%user_42%' " + regexp},
		{"SELECT * FROM logs WHERE message ILIKE '%user%status%'", "message ILIKE '%user%status%' " + regexp},
		{"SELECT * FROM logs WHERE message ILIKE '%timed out'", "message ILIKE '%timed out' " + regexp},
		{"WITH errors AS (SELECT * FROM logs WHERE message ILIKE '%x%') SELECT count(*) FROM errors",
			"message ILIKE '%x%' is slow on Logs: contains(lower(message), 'x') matches the same Logs several times faster."},
		{"SELECT count(*) FILTER (WHERE MESSAGE ILIKE '%x%') FROM logs",
			"message ILIKE '%x%' is slow on Logs: contains(lower(message), 'x') matches the same Logs several times faster."},

		// DuckDB rewrites a plain LIKE as contains, a pattern with no
		// leading % fails fast on most messages, and % alone matches them all.
		{"SELECT * FROM logs WHERE message LIKE '%timeout%'", ""},
		{"SELECT * FROM logs WHERE message ILIKE 'GET %'", ""},
		{"SELECT * FROM logs WHERE message ILIKE '%'", ""},
		{"SELECT * FROM logs WHERE contains(lower(message), 'timeout')", ""},
		{"SELECT * FROM logs WHERE lower(message) LIKE '%timeout%'", ""},
		{"SELECT * FROM logs WHERE service ILIKE '%api%'", ""},
		{"SELECT * FROM logs WHERE attrs_string['message'] ILIKE '%api%'", ""},
		{"SELECT 'message ILIKE ''%x%''' AS text FROM logs", ""},
		{"SELECT * FROM logs WHERE message ILIKE level", ""},
	}
	for _, c := range cases {
		if got := sqlNotice(parsedSelect(t, db, c.sql)); got != c.notice {
			t.Errorf("SQL %q\nexpected notice %q\n     got notice %q", c.sql, c.notice, got)
		}
	}
}
