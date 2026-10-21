package main_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// problem is an error or a warning the check route reports in a program.
type problem struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Text    string `json:"text"`
}

// checkAnswer is the check route's answer.
type checkAnswer struct {
	Errors   []problem `json:"errors"`
	Warnings []problem `json:"warnings"`
}

// checkProgram checks a program through the check route, as Console does
// while an admin types it.
func checkProgram(t *testing.T, env *TestEnv, program string) checkAnswer {
	t.Helper()
	status, raw := postQueryApp(t, env, "/api/123/logs/transform/check", map[string]string{"program": program})
	if status != 200 {
		t.Fatalf("Check expected 200, got %d: %s", status, raw)
	}
	if !strings.Contains(string(raw), `"errors":[`) || !strings.Contains(string(raw), `"warnings":[`) {
		t.Errorf("Expected errors and warnings as lists, got %s", raw)
	}
	var answer checkAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode check %q: %v", raw, err)
	}
	return answer
}

func TestCheckReportsEachErrorAndWarningWithItsSpan(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	program := ".level = \"info\"\n.x = nope(.message)"
	got := checkProgram(t, env, program)
	if len(got.Errors) != 1 || len(got.Warnings) != 0 {
		t.Fatalf("Expected one error, got %+v", got)
	}
	e := got.Errors[0]
	if e.Code != 105 || e.Message != "call to undefined function" || program[e.Start:e.End] != "nope" ||
		!strings.Contains(e.Text, "error[E105]: call to undefined function") {
		t.Errorf("Expected E105 spanning nope, got %+v", e)
	}

	program = ".a = 1\n\"unused\"\n.b = 2"
	got = checkProgram(t, env, program)
	if len(got.Errors) != 0 || len(got.Warnings) != 1 {
		t.Fatalf("Expected one warning, got %+v", got)
	}
	if w := got.Warnings[0]; program[w.Start:w.End] != `"unused"` || w.Code == 0 || !strings.Contains(w.Text, "warning") {
		t.Errorf("Expected a warning spanning the unused string, got %+v", w)
	}

	if got := checkProgram(t, env, jobLine); len(got.Errors) != 0 || len(got.Warnings) != 0 {
		t.Errorf("Expected nothing wrong with the job line program, got %+v", got)
	}
}

func TestCheckRefusesAProgramOver64KB(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	padded := func(size int) string {
		program := ".a = 1\n"
		return program + "#" + strings.Repeat("x", size-len(program)-1)
	}
	if got := checkProgram(t, env, padded(64<<10)); len(got.Errors) != 0 {
		t.Errorf("Expected a program of 64 KB to pass, got %+v", got)
	}
	program := padded(64<<10 + 1)
	got := checkProgram(t, env, program)
	if len(got.Errors) != 1 || !strings.Contains(got.Errors[0].Message, "64 KB") {
		t.Fatalf("Expected one error naming the limit, got %+v", got)
	}
	if e := got.Errors[0]; e.Start != 64<<10 || e.End != len(program) {
		t.Errorf("Expected the error to span what is past 64 KB, got %d to %d", e.Start, e.End)
	}
}
