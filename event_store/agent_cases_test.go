package main_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The request cases of the agent ingest API that the solidtrace CLI's fake
// also answers (cli/internal/fake/cases_test.go). They run here against the
// real API, so the fake that the CLI's tests use cannot drift from it.

const agentCasesFile = "../cli/agentapi/testdata/agent-ingest-cases.json"

type agentRequestCases struct {
	ServerVersion string   `json:"server_version"`
	Organization  string   `json:"organization"`
	Captured      []string `json:"captured_repositories"`
	Cases         []struct {
		Name  string             `json:"name"`
		Steps []agentRequestStep `json:"steps"`
	} `json:"cases"`
}

type agentRequestStep struct {
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

func TestTheAgentIngestAPIAnswersTheRequestCasesOfTheCLIsFake(t *testing.T) {
	data, err := os.ReadFile(agentCasesFile)
	if err != nil {
		t.Fatal(err)
	}
	var cases agentRequestCases
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("%s: %v", agentCasesFile, err)
	}

	env := setupTestEnv(t)
	defer env.Cleanup()
	rows := newConsoleRows(t, env)
	org := rows.organization(cases.Organization)
	rows.agentProject(org, cases.Captured...)
	member := rows.user()
	rows.member(org, member)
	secret, _ := rows.apiToken(apiTokenRow{user: member, organization: org, scopes: []string{"sessions:write"}})
	tokens := map[string]string{"member": secret, "wrong": "st_wrong", "none": ""}
	app := newAgentApp(t, env, cases.ServerVersion)

	for _, c := range cases.Cases {
		t.Run(c.Name, func(t *testing.T) {
			session := uuid.NewString()
			for i, s := range c.Steps {
				req := agentCaseRequest(t, session, tokens, s)
				resp, err := app.Test(req, 10000)
				if err != nil {
					t.Fatalf("step %d: %v", i+1, err)
				}
				raw, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				checkAgentCaseStep(t, i, s, session, cases.ServerVersion, resp.StatusCode,
					resp.Header.Get("SolidTrace-Version"), raw)
			}
		})
	}
}

func agentCaseRequest(t *testing.T, session string, tokens map[string]string, s agentRequestStep) *http.Request {
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
	target := "/api/agent/v1" + strings.ReplaceAll(s.Path, "{session}", session)
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
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", cmpOr(s.UserAgent, "solidtrace/0.19.0"))
	if token := tokens[cmpOr(s.Token, "member")]; token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func checkAgentCaseStep(t *testing.T, i int, s agentRequestStep, session, serverVersion string, status int, version string, raw []byte) {
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
		if problem := agentCaseSubset(agentCaseSession(s.Body, session), got, "body"); problem != "" {
			t.Errorf("%s: %s\nbody: %s", where, problem, raw)
		}
	}
	message, _ := got["error"].(string)
	if s.ErrorHas != "" && !strings.Contains(message, s.ErrorHas) {
		t.Errorf("%s: the error %q has no %q", where, message, s.ErrorHas)
	}
	if s.Status >= 400 && message == "" {
		t.Errorf("%s: no error text in %s", where, raw)
	}
}

// agentCaseSubset is "" when got has each key of want with its value; a
// list needs each item in order.
func agentCaseSubset(want, got any, at string) string {
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
			if problem := agentCaseSubset(v, gv, at+"."+k); problem != "" {
				return problem
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Sprintf("%s is %v, want %d items", at, got, len(w))
		}
		for i := range w {
			if problem := agentCaseSubset(w[i], g[i], fmt.Sprintf("%s[%d]", at, i)); problem != "" {
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

func agentCaseSession(v any, session string) any {
	switch x := v.(type) {
	case string:
		return strings.ReplaceAll(x, "{session}", session)
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			out[k] = agentCaseSession(e, session)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = agentCaseSession(e, session)
		}
		return out
	}
	return v
}
