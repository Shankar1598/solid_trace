package main_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Redaction on upload: the server replaces each byte that a Redaction
// pattern matches with '*' before it stores a unit of an Agent session's
// file. So the stored unit has the length of the original, each offset is
// the client's offset, and each line stays valid JSON.

// fakeSecret is prefix and then n characters of alphabet. The test secrets
// are made at run time, so that the source holds no text in a credential's
// format for a secret scanner to find.
func fakeSecret(alphabet string, n int, parts ...string) string {
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(part)
	}
	for i := 0; i < n; i++ {
		b.WriteByte(alphabet[(i*7+3)%len(alphabet)])
	}
	return b.String()
}

const (
	tokenAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"
	alnumAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

// defaultSecrets are one secret in each format of the default list.
func defaultSecrets() map[string]string {
	return map[string]string{
		"Anthropic API key":          fakeSecret(tokenAlphabet, 93, "sk-ant-", "api03-") + "AA",
		"Anthropic Admin API key":    fakeSecret(tokenAlphabet, 93, "sk-ant-", "admin01-") + "AA",
		"Claude OAuth access token":  fakeSecret(tokenAlphabet, 95, "sk-ant-", "oat01-"),
		"Claude OAuth refresh token": fakeSecret(tokenAlphabet, 95, "sk-ant-", "ort01-"),
		"Bitbucket access token":     fakeSecret(tokenAlphabet, 180, "ATCTT", "3xFfGN0") + "=1A2B3C4D",
		"Bitbucket app password":     fakeSecret(alnumAlphabet, 32, "ATBB"),
		"Atlassian API token":        fakeSecret(tokenAlphabet, 180, "ATATT", "3xFfGF0") + "=5E6F7A8B",
	}
}

func stars(text string) string { return strings.Repeat("*", len(text)) }

// setRedactionPatterns saves the extra Redaction patterns, as an Instance
// admin does on Settings → Instance. Console checks them first; a test
// that saves a pattern Console refuses acts as a hand edit of the database.
func setRedactionPatterns(t *testing.T, env *TestEnv, patterns ...string) {
	t.Helper()
	raw, _ := json.Marshal(patterns)
	db, err := sql.Open("sqlite3", env.SQLitePath)
	if err != nil {
		t.Fatalf("Failed to open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		INSERT INTO instance_settings (id, redaction_patterns, created_at, updated_at) VALUES (1, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET redaction_patterns = excluded.redaction_patterns`,
		string(raw), time.Now(), time.Now()); err != nil {
		t.Fatalf("Failed to set the Redaction patterns: %v", err)
	}
}

// expectJSONLines fails unless each line of text is valid JSON.
func expectJSONLines(t *testing.T, text string) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if !json.Valid([]byte(line)) {
			t.Errorf("Expected a stored line to be JSON, got %q", line)
		}
	}
}

func textLine(text string) string {
	raw, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}}}})
	return string(raw) + "\n"
}

func TestAgentSessionRedactionOnUpload(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	session := laptopSession
	offset := 0
	// upload adds body to session.jsonl at the next offset and returns
	// what the server stored of it.
	upload := func(t *testing.T, body string, headers map[string]string) string {
		t.Helper()
		before := len(storedBytes(t, env, o.project, session, "session.jsonl"))
		sent := []byte(body)
		if headers["Content-Encoding"] == "zstd" {
			sent = zstdCompressed(t, sent)
		}
		answer := uploadUnit(t, env, o.secret, session, unitUpload{path: "session.jsonl", offset: offset, body: sent, headers: headers})
		expectStatus(t, answer, 200)
		offset += len(body)
		if answer.body["end_offset"] != float64(offset) {
			t.Fatalf("Expected the end offset %d, got %s", offset, answer.raw)
		}
		return storedBytes(t, env, o.project, session, "session.jsonl")[before:]
	}

	t.Run("each default pattern inside a JSON string is replaced by '*' of the same length", func(t *testing.T) {
		for name, secret := range defaultSecrets() {
			line := textLine("Use " + secret + " for the API, and keep it safe.")
			want := strings.ReplaceAll(line, secret, stars(secret))
			if got := upload(t, line, nil); got != want {
				t.Errorf("%s: expected %q, got %q", name, want, got)
			}
		}
		expectJSONLines(t, storedBytes(t, env, o.project, session, "session.jsonl"))
	})

	t.Run("a secret in a git remote URL and next to other text is replaced, and the rest stays", func(t *testing.T) {
		secrets := defaultSecrets()
		access, app := secrets["Bitbucket access token"], secrets["Bitbucket app password"]
		line := textLine("git push https://x-token-auth:" + access + "@bitbucket.org/acme/shop.git\n" +
			"user:" + app + "@bitbucket.org café ✓")
		want := strings.ReplaceAll(strings.ReplaceAll(line, access, stars(access)), app, stars(app))
		if got := upload(t, line, nil); got != want {
			t.Errorf("Expected %q, got %q", want, got)
		}
	})

	t.Run("a secret at the start and at the end of a unit is replaced", func(t *testing.T) {
		key := defaultSecrets()["Anthropic API key"]
		body := `{"` + key + `":1}` + "\n" + textLine("first") + `{"text":"` + key + `"}` + "\n"
		want := strings.ReplaceAll(body, key, stars(key))
		if got := upload(t, body, nil); got != want {
			t.Errorf("Expected %q, got %q", want, got)
		}
	})

	t.Run("a unit sent with zstd is redacted once decoded", func(t *testing.T) {
		key := defaultSecrets()["Claude OAuth access token"]
		line := textLine("export CLAUDE_CODE_OAUTH_TOKEN=" + key)
		if got := upload(t, line, map[string]string{"Content-Encoding": "zstd"}); got != strings.ReplaceAll(line, key, stars(key)) {
			t.Errorf("Expected the token replaced, got %q", got)
		}
	})

	t.Run("each stored unit has the length of the original, and the next upload's offset is the original offset", func(t *testing.T) {
		stored := storedBytes(t, env, o.project, session, "session.jsonl")
		if len(stored) != offset {
			t.Errorf("Expected %d stored bytes, the bytes sent, got %d", offset, len(stored))
		}
		_, files := offsetsOf(t, env, o.secret, session)
		if got := endOffsets(files)["session.jsonl"]; got != fmt.Sprintf("1 %d", offset) {
			t.Errorf("Expected the end offset %d, got %s", offset, got)
		}
		upload(t, textLine("next"), nil)
		if strings.Contains(storedBytes(t, env, o.project, session, "session.jsonl"), "sk-ant-") {
			t.Errorf("Expected no Anthropic key prefix left in the stored file")
		}
	})

	t.Run("a file that is not .jsonl is redacted in the same way", func(t *testing.T) {
		key, app := defaultSecrets()["Anthropic API key"], defaultSecrets()["Bitbucket app password"]
		path := "tool-results/toolu_01.txt"
		body := key + "\nANTHROPIC_API_KEY=" + key + "\npassword " + app
		answer := uploadUnit(t, env, o.secret, session, unitUpload{path: path, body: []byte(body)})
		expectStatus(t, answer, 200)
		if answer.body["end_offset"] != float64(len(body)) {
			t.Errorf("Expected the end offset %d, got %s", len(body), answer.raw)
		}
		want := strings.ReplaceAll(strings.ReplaceAll(body, key, stars(key)), app, stars(app))
		if got := storedBytes(t, env, o.project, session, path); got != want {
			t.Errorf("Expected %q, got %q", want, got)
		}
	})

	t.Run("an extra pattern replaces its matches from the next upload on", func(t *testing.T) {
		secret := "acme_live_" + fakeSecret("abcdef0123456789", 16)
		line := textLine("The shop's key is " + secret)
		if got := upload(t, line, nil); got != line {
			t.Errorf("Expected no change before the pattern is saved, got %q", got)
		}
		setRedactionPatterns(t, env, `acme_live_[a-z0-9]{16}`)
		if got := upload(t, line, nil); got != strings.ReplaceAll(line, secret, stars(secret)) {
			t.Errorf("Expected the extra pattern's match replaced, got %q", got)
		}
		key := defaultSecrets()["Anthropic API key"]
		if got := upload(t, textLine(key), nil); got != textLine(stars(key)) {
			t.Errorf("Expected the default patterns to stay on with an extra pattern, got %q", got)
		}
	})

	t.Run("matches of two patterns that overlap are both replaced", func(t *testing.T) {
		setRedactionPatterns(t, env, `KEY[0-9]{4}`, `[0-9]{4}TAIL`)
		if got := upload(t, textLine("a KEY1234TAIL b"), nil); got != textLine("a ***********"+" b") {
			t.Errorf("Expected both matches replaced, got %q", got)
		}
	})

	t.Run("a match on an escape or outside a JSON string leaves the line valid JSON", func(t *testing.T) {
		setRedactionPatterns(t, env, `[A-Za-z0-9]{16,}`)
		body := `{"text":"see\nABCDEFGHIJKLMNOPQR end","n":12345678901234567890}` + "\n" +
			`{"text":"caféAAAAAAAAAAAAAAAAAAAA"}` + "\n"
		want := `{"text":"see\n****************** end","n":12345678901234567890}` + "\n" +
			`{"text":"café********************"}` + "\n"
		got := upload(t, body, nil)
		if got != want {
			t.Errorf("Expected %q, got %q", want, got)
		}
		expectJSONLines(t, got)
	})

	t.Run("a line that is not JSON has each match replaced", func(t *testing.T) {
		if got := upload(t, "not json ABCDEFGHIJKLMNOPQRST {\n", nil); got != "not json ******************** {\n" {
			t.Errorf("Expected the match replaced, got %q", got)
		}
	})

	t.Run("an extra pattern that cannot be read stores nothing", func(t *testing.T) {
		setRedactionPatterns(t, env, `acme_(`)
		answer := uploadUnit(t, env, o.secret, session, unitUpload{path: "session.jsonl", offset: offset, body: []byte(textLine("x"))})
		expectStatus(t, answer, 500)
		if msg, _ := answer.body["error"].(string); !strings.Contains(msg, "Settings → Instance") {
			t.Errorf("Expected the error to say where to fix the patterns, got %s", answer.raw)
		}
		_, files := offsetsOf(t, env, o.secret, session)
		if got := endOffsets(files)["session.jsonl"]; got != fmt.Sprintf("1 %d", offset) {
			t.Errorf("Expected nothing stored, got the end offset %s", got)
		}
		setRedactionPatterns(t, env)
	})
}

// patternCheckAnswer is the Redaction pattern check's answer.
type patternCheckAnswer struct {
	Errors []struct {
		Index   int    `json:"index"`
		Pattern string `json:"pattern"`
		Error   string `json:"error"`
	} `json:"errors"`
}

func checkPatterns(t *testing.T, env *TestEnv, patterns ...string) patternCheckAnswer {
	t.Helper()
	status, raw := postQueryApp(t, env, "/redaction-patterns/check", map[string]any{"patterns": patterns})
	if status != http.StatusOK {
		t.Fatalf("Expected 200 from the check, got %d: %s", status, raw)
	}
	var answer patternCheckAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Check answer %s: %v", raw, err)
	}
	return answer
}

func TestRedactionPatternCheck(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	t.Run("patterns that cannot match a quote or a backslash pass", func(t *testing.T) {
		answer := checkPatterns(t, env, `acme_live_[a-z0-9]{16}`, `(?i)token_[a-z]{8}`, `\d{6}`, `\s+pw=\w+`, `[A-Za-z0-9+/=]{40}`, `^ghp_\b`)
		if len(answer.Errors) != 0 {
			t.Errorf("Expected no errors, got %+v", answer.Errors)
		}
	})

	t.Run("a pattern that can match a quote or a backslash, or that is not valid, is refused with a clear error", func(t *testing.T) {
		cases := []struct{ pattern, says string }{
			{`secret="[a-z]+"`, "quote"},
			{`token\\[a-z]+`, "backslash"},
			{`password=.+`, "quote"},
			{`(?s)key.`, "quote"},
			{`key=[^ ]+`, "quote"},
			{`\W+`, "quote"},
			{`\pP+`, "quote"},
			{`[\x00-\x7f]{8}`, "quote"},
			{`[\x22]`, "quote"},
			{`sk_(`, "not a valid pattern"},
			{`(?=sk_)`, "not a valid pattern"},
			{``, "empty"},
			{strings.Repeat("a", 1025), "longer than"},
		}
		patterns := make([]string, len(cases))
		for i, c := range cases {
			patterns[i] = c.pattern
		}
		answer := checkPatterns(t, env, append([]string{`ok_[a-z]{4}`}, patterns...)...)
		if len(answer.Errors) != len(cases) {
			t.Fatalf("Expected %d errors, got %+v", len(cases), answer.Errors)
		}
		for i, c := range cases {
			e := answer.Errors[i]
			if e.Index != i+1 || e.Pattern != c.pattern || !strings.Contains(e.Error, c.says) {
				t.Errorf("Expected %q (at %d) to be refused with %q, got %+v", c.pattern, i+1, c.says, e)
			}
		}
	})

	t.Run("more than 100 patterns are refused", func(t *testing.T) {
		patterns := make([]string, 101)
		for i := range patterns {
			patterns[i] = fmt.Sprintf("p%d_[a-z]{4}", i)
		}
		answer := checkPatterns(t, env, patterns...)
		if len(answer.Errors) != 1 || !strings.Contains(answer.Errors[0].Error, "100") {
			t.Errorf("Expected one error naming the limit, got %+v", answer.Errors)
		}
	})
}
