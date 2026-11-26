package command_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/solidtrace/cli/internal/fake"
)

func (w *world) capturedSession() *agentSession {
	w.t.Helper()
	w.signIn()
	return w.session(w.gitRepo("origin=https://bitbucket.org/acme/shop.git"))
}

func TestCaptureUploadsSubagentsWorkflowAgentsAndToolResultsWithTheAgentSession(t *testing.T) {
	w := newWorld(t, acme)
	s := w.capturedSession()
	s.write("", "{\"main\":1}\n")
	s.write("subagents/agent-a1.jsonl", "{\"sub\":1}\n")
	s.write("subagents/agent-a1.meta.json", `{"agentType":"Explore"}`)
	s.write("subagents/workflows/wf_1/agent-b2.jsonl", "{\"workflow\":1}\n")
	s.write("workflows/wf_1.json", `{"status":"done"}`)
	s.write("tool-results/t1.txt", "a large tool output")

	s.stop()
	for path, want := range map[string]string{
		s.main():                           "{\"main\":1}\n",
		s.id + "/subagents/agent-a1.jsonl": "{\"sub\":1}\n",
		s.id + "/subagents/workflows/wf_1/agent-b2.jsonl": "{\"workflow\":1}\n",
	} {
		if got := s.stored(path); got != want {
			t.Errorf("%s after the first call: %q, want %q", path, got, want)
		}
	}
	// A file that is not .jsonl may still change: it waits for the next call.
	if got := s.stored(s.id + "/tool-results/t1.txt"); got != "" {
		t.Errorf("tool result uploaded at its first sight: %q", got)
	}

	s.stop()
	for path, want := range map[string]string{
		s.id + "/subagents/agent-a1.meta.json": `{"agentType":"Explore"}`,
		s.id + "/workflows/wf_1.json":          `{"status":"done"}`,
		s.id + "/tool-results/t1.txt":          "a large tool output",
	} {
		file := w.agent.Stored(s.id, path)
		if file == nil || string(file.Bytes) != want {
			t.Fatalf("%s after the second call: %+v; log: %s", path, file, w.captureLog())
		}
		if file.ContentHash != sha256Of(want) || file.Version != 1 {
			t.Errorf("%s: hash %s version %d", path, file.ContentHash, file.Version)
		}
	}
	uploads := len(w.uploads())
	s.stop()
	if n := len(w.uploads()); n != uploads {
		t.Errorf("a third call with no change uploaded %d units", n-uploads)
	}
}

func sha256Of(s string) string {
	h := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(h[:])
}

func TestCaptureUploadsAChangedFileAsANewVersionAndAtSessionEndAtOnce(t *testing.T) {
	w := newWorld(t, acme)
	s := w.capturedSession()
	s.write("", "{\"n\":1}\n")
	s.write("workflows/wf_1.json", `{"status":"running"}`)
	s.stop()
	s.stop()
	if f := w.agent.Stored(s.id, s.id+"/workflows/wf_1.json"); f == nil || f.Version != 1 {
		t.Fatalf("first version: %+v", f)
	}

	s.write("workflows/wf_1.json", `{"status":"done, with more"}`)
	s.write("tool-results/last.txt", "the last output")
	s.hook("SessionEnd")
	f := w.agent.Stored(s.id, s.id+"/workflows/wf_1.json")
	if f == nil || f.Version != 2 || string(f.Bytes) != `{"status":"done, with more"}` {
		t.Errorf("changed file at SessionEnd: %+v", f)
	}
	if got := s.stored(s.id + "/tool-results/last.txt"); got != "the last output" {
		t.Errorf("new file at SessionEnd: %q", got)
	}
}

func TestCaptureUploadsATrimmedFileAgainFromOffsetZeroAsANewVersion(t *testing.T) {
	w := newWorld(t, acme)
	s := w.capturedSession()
	s.write("", "{\"n\":1}\n{\"n\":2}\n{\"n\":3}\n")
	s.stop()

	s.write("", "{\"n\":9}\n")
	s.stop()
	f := w.agent.Stored(s.id, s.main())
	if f == nil || f.Version != 2 || string(f.Bytes) != "{\"n\":9}\n" {
		t.Fatalf("after the trim: %+v; log: %s", f, w.captureLog())
	}
	last := w.uploads()[len(w.uploads())-1]
	if last.Query.Get("offset") != "0" || last.Query.Get("version") != "2" {
		t.Errorf("trim upload: %v", last.Query)
	}
}

func TestCaptureSendsALastLineOnlyAfterItsNewline(t *testing.T) {
	w := newWorld(t, acme)
	s := w.capturedSession()
	s.write("", "{\"n\":1}\n{\"n\":")
	s.stop()
	if got := s.stored(s.main()); got != "{\"n\":1}\n" {
		t.Fatalf("stored: %q", got)
	}
	s.add("", "2}")
	s.stop()
	if n := len(w.uploads()); n != 1 {
		t.Errorf("a line with no newline was sent: %d uploads", n)
	}
	s.add("", "\n{\"n\":3}\n")
	s.stop()
	if got := s.stored(s.main()); got != "{\"n\":1}\n{\"n\":2}\n{\"n\":3}\n" {
		t.Fatalf("stored: %q", got)
	}
	content := "{\"n\":1}\n{\"n\":2}\n{\"n\":3}\n"
	for _, u := range w.uploads() {
		offset, _ := strconv.Atoi(u.Query.Get("offset"))
		if offset > 0 && content[offset-1] != '\n' {
			t.Errorf("offset %d is not on a line boundary", offset)
		}
		if !bytes.HasSuffix(u.Body, []byte("\n")) {
			t.Errorf("a unit does not end with a newline: %q", u.Body)
		}
	}
}

func TestTwoCaptureCallsForOneAgentSessionAtTheSameTimeUploadEachByteOnce(t *testing.T) {
	w := newWorld(t, acme)
	s := w.capturedSession()
	var content strings.Builder
	for i := range 2000 {
		content.WriteString("{\"line\":" + strconv.Itoa(i) + ",\"pad\":\"" + strings.Repeat("x", 200) + "\"}\n")
	}
	s.write("", content.String())
	s.write("subagents/agent-a1.jsonl", content.String())
	s.stop() // the list is now in the cache

	s.add("", content.String())
	s.add("subagents/agent-a1.jsonl", content.String())
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.hook("SubagentStop")
		}()
	}
	wg.Wait()
	for _, path := range []string{s.main(), s.id + "/subagents/agent-a1.jsonl"} {
		if got := s.stored(path); got != content.String()+content.String() {
			t.Errorf("%s: %d bytes stored, want %d", path, len(got), 2*content.Len())
		}
	}
	sent := 0
	for _, r := range w.agent.AllRequests() {
		if r.Status != 200 {
			t.Errorf("request %s %v: %d", r.Method, r.Query, r.Status)
		}
		sent += len(r.Body)
	}
	if sent != 4*content.Len() {
		t.Errorf("sent %d bytes, want each byte once (%d)", sent, 4*content.Len())
	}
}

func TestACaptureFailureGoesToTheLocalLogOnly(t *testing.T) {
	w := newWorld(t, acme)
	s := w.capturedSession()
	s.write("", "{\"n\":1}\n")
	w.agent.Set(func(f *fake.AgentAPI) { f.UploadsDown = true })
	s.stop() // checks exit 0 and an empty stderr
	if !strings.Contains(w.captureLog(), "500") {
		t.Errorf("log: %s", w.captureLog())
	}

	w.agent.Set(func(f *fake.AgentAPI) { f.UploadsDown = false })
	s.stop()
	if got := s.stored(s.main()); got != "{\"n\":1}\n" {
		t.Errorf("the next call did not catch up: %q", got)
	}
}

func TestCaptureWithNoSignInLogsAndExitsZero(t *testing.T) {
	w := newWorld(t, acme)
	w.gitRepo()
	s := w.session(w.t.TempDir())
	s.write("", "{\"n\":1}\n")
	s.stop()
	if !strings.Contains(w.captureLog(), "not signed in") {
		t.Errorf("log: %s", w.captureLog())
	}
}

func TestALineOrAFileOver32MiBIsSentAsItsFirst32MiBWithItsFullSize(t *testing.T) {
	w := newWorld(t, acme)
	s := w.capturedSession()
	long := "{\"big\":\"" + strings.Repeat("y", fake.BodyLimit+100) + "\"}\n"
	s.write("", "{\"n\":1}\n"+long+"{\"n\":3}\n")
	s.write("tool-results/huge.txt", strings.Repeat("z", fake.BodyLimit+5))
	s.stop()
	s.stop()

	uploads := w.uploads()
	var cut *fake.Request
	for i := range uploads {
		if uploads[i].Query.Get("size") != "" && uploads[i].Query.Get("path") == s.main() {
			cut = &uploads[i]
		}
	}
	if cut == nil {
		t.Fatalf("no cut unit; log: %s", w.captureLog())
	}
	if len(cut.Body) != fake.BodyLimit || cut.Query.Get("size") != strconv.Itoa(len(long)) || cut.Query.Get("offset") != "8" {
		t.Errorf("cut unit: %d bytes, query %v", len(cut.Body), cut.Query)
	}
	f := w.agent.Stored(s.id, s.main())
	if want := uint64(8 + len(long) + 8); f.End != want || !f.Cut {
		t.Errorf("main file end %d cut %v, want end %d", f.End, f.Cut, want)
	}
	if !strings.HasSuffix(string(f.Bytes), "{\"n\":3}\n") {
		t.Errorf("the line after the long line was not sent")
	}
	huge := w.agent.Stored(s.id, s.id+"/tool-results/huge.txt")
	if huge == nil || len(huge.Bytes) != fake.BodyLimit || huge.End != fake.BodyLimit+5 ||
		huge.ContentHash != sha256Of(strings.Repeat("z", fake.BodyLimit+5)) {
		t.Errorf("huge file: %+v", huge)
	}
}

func TestARefusalOfOnePathLetsTheOtherPathsGo(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusConflict} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			w := newWorld(t, acme)
			s := w.capturedSession()
			s.write("", "{\"main\":1}\n")
			s.write("subagents/agent-a1.jsonl", "{\"sub\":1}\n")
			s.write("tool-results/t1.txt", "out")
			w.agent.Set(func(f *fake.AgentAPI) { f.Refuse = map[string]int{s.main(): status} })
			s.hook("SessionEnd")
			if s.stored(s.id+"/subagents/agent-a1.jsonl") != "{\"sub\":1}\n" || s.stored(s.id+"/tool-results/t1.txt") != "out" {
				t.Errorf("the other paths did not go; log: %s", w.captureLog())
			}
			if !strings.Contains(w.captureLog(), s.main()) || !strings.Contains(w.captureLog(), strconv.Itoa(status)) {
				t.Errorf("the refusal is not in the log: %s", w.captureLog())
			}
		})
	}
}

func TestAForbiddenOrFailedUploadStopsTheCall(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			w := newWorld(t, acme)
			s := w.capturedSession()
			s.write("", "{\"main\":1}\n")
			s.write("subagents/agent-a1.jsonl", "{\"sub\":1}\n")
			s.write("tool-results/t1.txt", "out")
			w.agent.Set(func(f *fake.AgentAPI) { f.Refuse = map[string]int{s.main(): status} })
			s.hook("SessionEnd")
			posts := 0
			for _, r := range w.agent.AllRequests() {
				if r.Method == http.MethodPost {
					posts++
				}
			}
			if posts != 1 {
				t.Errorf("uploads after a %d: %d, want only the refused one", status, posts)
			}
			if !strings.Contains(w.captureLog(), strconv.Itoa(status)) {
				t.Errorf("log: %s", w.captureLog())
			}
		})
	}
}

// The HTTP client's Timeout is for the small calls: an upload of a large
// unit on a slow link can take longer.
func TestAnUploadIsNotStoppedByTheTimeoutOfTheSmallCalls(t *testing.T) {
	w := newWorld(t, acme)
	s := w.capturedSession()
	s.write("", "{\"main\":1}\n")
	s.write("tool-results/t1.txt", "out")
	s.write("workflows/wf_1.json", `{"status":"done"}`)
	w.httpTimeout = 200 * time.Millisecond
	w.agent.Set(func(f *fake.AgentAPI) { f.UploadDelay = 400 * time.Millisecond })
	s.hook("SessionEnd")
	for _, path := range []string{s.main(), s.id + "/tool-results/t1.txt", s.id + "/workflows/wf_1.json"} {
		if s.stored(path) == "" {
			t.Errorf("%s did not upload; log: %s", path, w.captureLog())
		}
	}
}

// posts are the upload requests for a path, also the refused ones.
func (w *world) posts(path string) []fake.Request {
	var posts []fake.Request
	for _, r := range w.agent.AllRequests() {
		if r.Method == http.MethodPost && r.Query.Get("path") == path {
			posts = append(posts, r)
		}
	}
	return posts
}

// A call that stops early keeps what the last call saw of the paths that
// it did not reach: a file that did not change goes at the next call, and
// does not wait one more call as if it were new.
func TestACallThatStopsEarlyKeepsWhatItDidNotReach(t *testing.T) {
	w := newWorld(t, acme)
	s := w.capturedSession()
	s.write("", "{\"main\":1}\n")
	s.write("tool-results/a.txt", "first")
	s.write("tool-results/b.txt", "second")
	s.stop()

	a, b := s.id+"/tool-results/a.txt", s.id+"/tool-results/b.txt"
	w.agent.Set(func(f *fake.AgentAPI) { f.Refuse = map[string]int{a: http.StatusInternalServerError} })
	s.stop()
	if len(w.posts(b)) != 0 {
		t.Fatalf("the call did not stop at the failed path")
	}

	w.agent.Set(func(f *fake.AgentAPI) { f.Refuse = nil })
	s.stop()
	if got := s.stored(b); got != "second" {
		t.Errorf("%s after the call that follows the failure: %q; log: %s", b, got, w.captureLog())
	}
	if got := s.stored(a); got != "first" {
		t.Errorf("%s after the call that follows the failure: %q", a, got)
	}
}

func TestAnEmptyFileIsNotUploaded(t *testing.T) {
	w := newWorld(t, acme)
	s := w.capturedSession()
	s.write("", "{\"main\":1}\n")
	s.write("tool-results/empty.txt", "")
	s.hook("SessionEnd")
	if posts := w.posts(s.id + "/tool-results/empty.txt"); len(posts) != 0 {
		t.Errorf("an empty file was sent %d times", len(posts))
	}
	if s.stored(s.main()) == "" {
		t.Errorf("the main file did not upload; log: %s", w.captureLog())
	}
}

// outside is a file out of the Claude projects directory, such as a
// secrets file.
func (w *world) outside(content string) string {
	w.t.Helper()
	path := filepath.Join(w.t.TempDir(), "secrets")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		w.t.Fatal(err)
	}
	return path
}

func (w *world) noBodyHas(secret string) {
	w.t.Helper()
	for _, r := range w.agent.AllRequests() {
		if bytes.Contains(r.Body, []byte(secret)) {
			w.t.Errorf("an upload of %s has the bytes of the file outside", r.Query.Get("path"))
		}
	}
}

func TestCaptureDoesNotFollowASymlinkAsTheMainFile(t *testing.T) {
	w := newWorld(t, acme)
	s := w.capturedSession()
	secret := "{\"secret\":\"main\"}\n"
	if err := os.Symlink(w.outside(secret), s.file("")); err != nil {
		t.Fatal(err)
	}
	s.hook("SessionEnd")
	if posts := w.posts(s.main()); len(posts) != 0 {
		t.Errorf("the symlink %s was sent %d times", s.main(), len(posts))
	}
	w.noBodyHas("secret")
}

func TestCaptureSkipsASymlinkUnderTheAgentSessionDirectory(t *testing.T) {
	w := newWorld(t, acme)
	s := w.capturedSession()
	s.write("", "{\"main\":1}\n")
	s.write("tool-results/t1.txt", "out")
	if err := os.Symlink(w.outside("secret tool output"), s.file("tool-results/link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(w.outside("{\"secret\":\"sub\"}\n"), s.file("tool-results/link.jsonl")); err != nil {
		t.Fatal(err)
	}
	s.hook("SessionEnd")
	for _, path := range []string{s.id + "/tool-results/link.txt", s.id + "/tool-results/link.jsonl"} {
		if posts := w.posts(path); len(posts) != 0 {
			t.Errorf("the symlink %s was sent %d times", path, len(posts))
		}
	}
	w.noBodyHas("secret")
	if s.stored(s.id+"/tool-results/t1.txt") != "out" {
		t.Errorf("the regular file did not upload; log: %s", w.captureLog())
	}
}
