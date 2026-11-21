package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// answerWith is a Console that answers each request with one status and body.
type answerWith struct {
	status int
	body   string
}

func (a answerWith) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: a.status, Body: io.NopCloser(strings.NewReader(a.body))}, nil
}

func TestChildTokensKeepTheScopesSessionsAndProjectLimitOfEachToken(t *testing.T) {
	children := &childTokens{next: answerWith{http.StatusCreated, `{"api_token":{"id":7},"secret":"st_abc"}`}}
	post := func(body string) {
		req, _ := http.NewRequest(http.MethodPost, "http://console.test/api/child_tokens", strings.NewReader(body))
		resp, err := children.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		if answer, _ := io.ReadAll(resp.Body); !strings.Contains(string(answer), "st_abc") {
			t.Errorf("the Dispatcher's answer changed: %s", answer)
		}
	}
	post(`{"scopes":["sessions:read","sessions:write"],"agent_session_ids":["s1"],"expires_at":"2026-11-21T10:00:00Z"}`)
	post(`{"scopes":["mcp:read"],"project_id":"42","expires_at":"2026-11-21T10:00:00Z"}`)
	post(`{"scopes":["mcp:read"],"project_id":43}`)

	got := children.list()
	if len(got) != 3 {
		t.Fatalf("%d tokens kept, want 3", len(got))
	}
	if got[0].ProjectID != "" || got[0].AgentSessionIDs[0] != "s1" || got[0].Secret != "st_abc" {
		t.Errorf("the session token: %+v", got[0])
	}
	if got[1].ProjectID != "42" || got[1].Scopes[0] != "mcp:read" || len(got[1].AgentSessionIDs) != 0 {
		t.Errorf("the MCP token: %+v", got[1])
	}
	if got[2].ProjectID != "43" {
		t.Errorf("a number as the Project limit: %+v", got[2])
	}
}
