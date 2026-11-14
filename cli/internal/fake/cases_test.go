package fake_test

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/solidtrace/cli/internal/fake"
)

// The request cases of the agent ingest API run against this fake here,
// and against the real API in EventStore's test environment
// (event_store/agent_cases_test.go): the fake cannot drift from it.

// casesFile is shared with EventStore's tests.
const casesFile = "../../agentapi/testdata/agent-ingest-cases.json"

type requestCases struct {
	ServerVersion string   `json:"server_version"`
	Organization  string   `json:"organization"`
	Captured      []string `json:"captured_repositories"`
	Cases         []struct {
		Name  string        `json:"name"`
		Steps []requestStep `json:"steps"`
	} `json:"cases"`
}

type requestStep struct {
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Token     string            `json:"token"`
	UserAgent string            `json:"user_agent"`
	Query     map[string]string `json:"query"`
	BodyText  string            `json:"body_text"`
	Gzip      bool              `json:"gzip"`
	Status    int               `json:"status"`
	Body      any               `json:"body"`
	ErrorHas  string            `json:"error_has"`
}

func TestTheFakeAnswersTheRequestCasesOfTheAgentIngestAPI(t *testing.T) {
	data, err := os.ReadFile(casesFile)
	if err != nil {
		t.Fatal(err)
	}
	var cases requestCases
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("%s: %v", casesFile, err)
	}
	oauth := fake.NewOAuth(fake.Organization{Name: cases.Organization, Slug: "acme", Repositories: cases.Captured})
	defer oauth.Close()
	api := fake.NewAgentAPI(oauth)
	defer api.Close()
	// The real server works out its minimum version from its own version;
	// the fake is told it.
	api.Version, api.MinimumVersion = cases.ServerVersion, "0.16.0"
	tokens := map[string]string{"member": oauth.AccessToken("acme"), "wrong": "sto_wrong", "none": ""}

	for _, c := range cases.Cases {
		t.Run(c.Name, func(t *testing.T) {
			session := newSessionID()
			for i, s := range c.Steps {
				status, version, body := send(t, api.URL(), session, tokens, s)
				checkStep(t, i, s, session, cases.ServerVersion, status, version, body)
			}
		})
	}
}

func send(t *testing.T, base, session string, tokens map[string]string, s requestStep) (int, string, []byte) {
	t.Helper()
	q := url.Values{}
	for k, v := range s.Query {
		q.Set(k, strings.ReplaceAll(v, "{session}", session))
	}
	body := []byte(s.BodyText)
	if s.Gzip {
		var b bytes.Buffer
		zw := gzip.NewWriter(&b)
		_, _ = zw.Write(body)
		_ = zw.Close()
		body = b.Bytes()
	}
	target := base + "/api/agent/v1" + strings.ReplaceAll(s.Path, "{session}", session)
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	req, err := http.NewRequest(s.Method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if s.Gzip {
		req.Header.Set("Content-Encoding", "gzip")
	}
	req.Header.Set("User-Agent", cmpOr(s.UserAgent, "solidtrace/0.19.0"))
	if token := tokens[cmpOr(s.Token, "member")]; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("SolidTrace-Version"), raw
}

func checkStep(t *testing.T, i int, s requestStep, session, serverVersion string, status int, version string, raw []byte) {
	t.Helper()
	where := fmt.Sprintf("step %d (%s %s)", i+1, s.Method, s.Path)
	if status != s.Status {
		t.Errorf("%s: status %d, want %d: %s", where, status, s.Status, raw)
		return
	}
	if version != serverVersion {
		t.Errorf("%s: version header %q, want %q", where, version, serverVersion)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Errorf("%s: the body is not a JSON object: %q", where, raw)
		return
	}
	if s.Body != nil {
		if problem := subset(withSession(s.Body, session), got, "body"); problem != "" {
			t.Errorf("%s: %s\nbody: %s", where, problem, raw)
		}
	}
	if message, _ := got["error"].(string); s.ErrorHas != "" && !strings.Contains(message, s.ErrorHas) {
		t.Errorf("%s: the error %q has no %q", where, message, s.ErrorHas)
	}
	if s.Status >= 400 {
		if message, _ := got["error"].(string); message == "" {
			t.Errorf("%s: no error text in %s", where, raw)
		}
	}
}

// subset is "" when got has each key of want with its value; a list needs
// each item in order.
func subset(want, got any, at string) string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s is %v, want an object", at, got)
		}
		for k, v := range w {
			gv, ok := g[k]
			if !ok {
				return fmt.Sprintf("%s has no %s", at, k)
			}
			if problem := subset(v, gv, at+"."+k); problem != "" {
				return problem
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Sprintf("%s is %v, want %d items", at, got, len(w))
		}
		for i := range w {
			if problem := subset(w[i], g[i], fmt.Sprintf("%s[%d]", at, i)); problem != "" {
				return problem
			}
		}
	default:
		if !reflect.DeepEqual(want, got) {
			return fmt.Sprintf("%s is %v, want %v", at, got, want)
		}
	}
	return ""
}

func withSession(v any, session string) any {
	switch x := v.(type) {
	case string:
		return strings.ReplaceAll(x, "{session}", session)
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = withSession(e, session)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = withSession(e, session)
		}
		return out
	}
	return v
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func cmpOr(v, otherwise string) string {
	if v == "" {
		return otherwise
	}
	return v
}
