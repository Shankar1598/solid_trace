package dispatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// solidTraceProject is a Project as SolidTrace lists it.
type solidTraceProject struct {
	id, slug string
}

// listProjects asks SolidTrace for the Organization's Projects, with the
// Dispatcher's own token, through the MCP server's list_projects tool:
// it is SolidTrace's public API that gives each Project's id. The token
// needs the mcp:read scope (the Dispatcher's token has it for its child
// tokens).
func (d *Dispatcher) listProjects(ctx context.Context, org organization) ([]solidTraceProject, error) {
	request, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "list_projects", "arguments": map[string]string{"organization": org.slug}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.config.SolidTraceURL+"/mcp", bytes.NewReader(request))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.config.SolidTraceToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := d.tokens.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, errCallerRefused
	case resp.StatusCode == http.StatusForbidden:
		return nil, errors.New("SolidTrace refuses it to the Dispatcher's token, which needs the mcp:read scope")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("SolidTrace answered %d", resp.StatusCode)
	}
	var answer struct {
		Result *struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &answer); err != nil || (answer.Result == nil && answer.Error == nil) {
		return nil, errors.New("the answer is not a JSON-RPC answer")
	}
	if answer.Error != nil {
		return nil, errors.New(answer.Error.Message)
	}
	var text string
	if len(answer.Result.Content) > 0 {
		text = answer.Result.Content[0].Text
	}
	if answer.Result.IsError {
		return nil, errors.New(text)
	}
	// The text is the JSON answer, then a line with the Console link.
	text, _, _ = strings.Cut(text, "\nConsole: ")
	var listed struct {
		Projects []struct {
			ID   json.Number `json:"id"`
			Slug string      `json:"slug"`
		} `json:"projects"`
	}
	if err := json.Unmarshal([]byte(text), &listed); err != nil {
		return nil, fmt.Errorf("read the list: %w", err)
	}
	projects := make([]solidTraceProject, 0, len(listed.Projects))
	for _, p := range listed.Projects {
		projects = append(projects, solidTraceProject{id: p.ID.String(), slug: p.Slug})
	}
	return projects, nil
}

// solidTraceFailure answers a request when SolidTrace could not list the
// Projects.
func (d *Dispatcher) solidTraceFailure(w http.ResponseWriter, org organization, err error) {
	if errors.Is(err, errCallerRefused) {
		d.log.Error("SolidTrace refuses the Dispatcher's own token")
		writeProblem(w, http.StatusServiceUnavailable, callerRefused)
		return
	}
	d.log.Warn("could not list the Projects at SolidTrace", "organization", org.slug, "error", err)
	writeProblem(w, http.StatusBadGateway, "the Dispatcher could not list the Projects of "+org.slug+
		" at SolidTrace: "+err.Error())
}
