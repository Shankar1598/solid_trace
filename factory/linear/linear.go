// Package linear is a small client of Linear: the OAuth token endpoint,
// the GraphQL calls that the factory makes on tickets (Linear issues),
// and the parse of Linear's webhooks. It keeps no state: the Dispatcher
// keeps the tokens of each connection and refreshes them.
package linear

import (
	"bytes"
	"cmp"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// The defaults of Linear.
const (
	// APIURL is the base of the GraphQL API (/graphql) and of the token
	// endpoint (/oauth/token).
	APIURL = "https://api.linear.app"
	// SignatureHeader holds the hex HMAC-SHA256 of the raw body, with the
	// webhook's signing secret.
	SignatureHeader = "Linear-Signature"
	// DeliveryHeader holds the delivery id.
	DeliveryHeader = "Linear-Delivery"
	// Window is the longest time between a webhook's webhookTimestamp and
	// its arrival.
	Window = 60 * time.Second
)

// App is a Linear OAuth application.
type App struct{ ClientID, ClientSecret string }

// Tokens are the tokens of an install. The access token lasts 24 hours;
// each refresh gives a new refresh token, and the old one stops.
type Tokens struct {
	AccessToken, RefreshToken string
	ExpiresAt                 time.Time
}

// OAuthError is an error answer of the token endpoint, such as
// invalid_grant for a used or revoked code or refresh token.
type OAuthError struct {
	Code, Description string
	Status            int
}

func (e *OAuthError) Error() string {
	text := "Linear's token endpoint answered " + e.Code
	if e.Description != "" {
		text += ": " + e.Description
	}
	return text
}

// ErrUnauthorized is a GraphQL call that Linear refused with 401: the
// access token expired or was revoked.
var ErrUnauthorized = errors.New("Linear refused the access token (401)")

// ErrNotFound is an issue that Linear does not have, or that the token
// cannot read.
var ErrNotFound = errors.New("Linear has no such issue")

// API is Linear's API at URL.
type API struct {
	// URL is the base ("" is APIURL).
	URL string
	// HTTP is the client (nil is a client with a 30-second timeout).
	HTTP *http.Client
}

func (a API) url(path string) string {
	if a.URL == "" {
		return APIURL + path
	}
	return strings.TrimRight(a.URL, "/") + path
}

func (a API) client() *http.Client {
	if a.HTTP == nil {
		return &http.Client{Timeout: 30 * time.Second}
	}
	return a.HTTP
}

// Exchange exchanges the code of an approval for the tokens.
func (a API) Exchange(ctx context.Context, app App, code, redirectURI, verifier string, now time.Time) (Tokens, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI},
		"client_id": {app.ClientID}, "client_secret": {app.ClientSecret}}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	return a.token(ctx, form, now)
}

// Refresh gives new tokens for the refresh token.
func (a API) Refresh(ctx context.Context, app App, refreshToken string, now time.Time) (Tokens, error) {
	return a.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken},
		"client_id": {app.ClientID}, "client_secret": {app.ClientSecret}}, now)
}

func (a API) token(ctx context.Context, form url.Values, now time.Time) (Tokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url("/oauth/token"), strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := a.client().Do(req)
	if err != nil {
		return Tokens{}, fmt.Errorf("Linear's token endpoint: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return Tokens{}, &OAuthError{Code: e.Error, Description: e.Description, Status: resp.StatusCode}
		}
		return Tokens{}, fmt.Errorf("Linear's token endpoint answered %d", resp.StatusCode)
	}
	var t struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &t); err != nil || t.AccessToken == "" || t.RefreshToken == "" || t.ExpiresIn <= 0 {
		return Tokens{}, errors.New("Linear's token endpoint gave no access token, refresh token and expiry")
	}
	return Tokens{AccessToken: t.AccessToken, RefreshToken: t.RefreshToken,
		ExpiresAt: now.Add(time.Duration(t.ExpiresIn) * time.Second)}, nil
}

// graphQLError is one error of a GraphQL answer.
type graphQLError struct {
	Message string `json:"message"`
}

// graphQL sends one named operation and decodes its data into out.
func (a API) graphQL(ctx context.Context, token, operation, query string, variables map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"operationName": operation, "query": query, "variables": variables})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url("/graphql"), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.client().Do(req)
	if err != nil {
		return fmt.Errorf("Linear's API: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	var answer struct {
		Data   json.RawMessage `json:"data"`
		Errors []graphQLError  `json:"errors"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		return fmt.Errorf("Linear's API answered %d to %s", resp.StatusCode, operation)
	}
	if len(answer.Errors) > 0 {
		for _, e := range answer.Errors {
			if strings.HasPrefix(e.Message, "Entity not found") {
				return ErrNotFound
			}
		}
		return fmt.Errorf("Linear's API refused %s: %s", operation, answer.Errors[0].Message)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Linear's API answered %d to %s", resp.StatusCode, operation)
	}
	return json.Unmarshal(answer.Data, out)
}

// User is a Linear user. An app's own user is the actor of what an
// install with actor=app does.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Viewer is the user of the token: the app's own user for an install with
// actor=app.
func (a API) Viewer(ctx context.Context, token string) (User, error) {
	var data struct{ Viewer User }
	err := a.graphQL(ctx, token, "Viewer", `query Viewer { viewer { id name } }`, nil, &data)
	return data.Viewer, err
}

// Team is a team of the workspace. Its key is the prefix of its issues'
// identifiers, such as ENG.
type Team struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Teams lists the teams that the token can read (at most 100).
func (a API) Teams(ctx context.Context, token string) ([]Team, error) {
	var data struct {
		Teams struct{ Nodes []Team }
	}
	err := a.graphQL(ctx, token, "Teams", `query Teams { teams(first: 100) { nodes { id key name } } }`, nil, &data)
	return data.Teams.Nodes, err
}

// Label is a label of an issue, with the name of its group ("" is a label
// in no group).
type Label struct{ Name, Group string }

// StatusChange is one change of an issue's status.
type StatusChange struct {
	At time.Time
	// By is the user who made the change (zero for an automation).
	By     User
	Status string
}

// Issue is a Linear issue as the factory reads it.
type Issue struct {
	ID, Identifier, URL, Title, Description string
	Status                                  string
	// Team is the id of the issue's team.
	Team      string
	UpdatedAt time.Time
	Labels    []Label
	// LastStatusChange is the newest change of the status that Linear
	// gives (zero is none).
	LastStatusChange StatusChange
}

// issueFields are the fields of Issue.
const issueFields = `id identifier url title description updatedAt state { name } team { id }
labels(first: 50) { nodes { name parent { name } } }
history(first: 50) { nodes { createdAt actor { id name } toState { name } } }`

type issueJSON struct {
	ID          string    `json:"id"`
	Identifier  string    `json:"identifier"`
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updatedAt"`
	State       struct {
		Name string `json:"name"`
	} `json:"state"`
	Team struct {
		ID string `json:"id"`
	} `json:"team"`
	Labels struct {
		Nodes []struct {
			Name   string `json:"name"`
			Parent *struct {
				Name string `json:"name"`
			} `json:"parent"`
		} `json:"nodes"`
	} `json:"labels"`
	History struct {
		Nodes []struct {
			CreatedAt time.Time `json:"createdAt"`
			Actor     *User     `json:"actor"`
			ToState   *struct {
				Name string `json:"name"`
			} `json:"toState"`
		} `json:"nodes"`
	} `json:"history"`
}

func (j issueJSON) issue() Issue {
	issue := Issue{ID: j.ID, Identifier: j.Identifier, URL: j.URL, Title: j.Title, Description: j.Description,
		Status: j.State.Name, Team: j.Team.ID, UpdatedAt: j.UpdatedAt}
	for _, l := range j.Labels.Nodes {
		label := Label{Name: l.Name}
		if l.Parent != nil {
			label.Group = l.Parent.Name
		}
		issue.Labels = append(issue.Labels, label)
	}
	// The newest change of the status, in any order of the list. Of two
	// changes at the same time, the first in the list wins.
	found := false
	for _, h := range j.History.Nodes {
		if h.ToState == nil || (found && !h.CreatedAt.After(issue.LastStatusChange.At)) {
			continue
		}
		found = true
		change := StatusChange{At: h.CreatedAt, Status: h.ToState.Name}
		if h.Actor != nil {
			change.By = *h.Actor
		}
		issue.LastStatusChange = change
	}
	return issue
}

// Issue reads one issue. ErrNotFound is an issue that Linear does not
// have.
func (a API) Issue(ctx context.Context, token, id string) (Issue, error) {
	var data struct{ Issue *issueJSON }
	err := a.graphQL(ctx, token, "Ticket", `query Ticket($id: String!) { issue(id: $id) { `+issueFields+` } }`,
		map[string]any{"id": id}, &data)
	if err != nil {
		return Issue{}, err
	}
	if data.Issue == nil {
		return Issue{}, ErrNotFound
	}
	return data.Issue.issue(), nil
}

// PageSize is the number of issues in one page of IssuesInStatus. Linear
// refuses a query above 10,000 complexity points, and the connection of
// issues multiplies the points of each issue (about 290 with its labels
// and history) by its first argument: 25 issues are about 7,300 points.
const PageSize = 25

// MaxPages is the most pages that IssuesInStatus reads (500 issues), so
// one backstop cannot use much of the app's 5,000 requests an hour.
const MaxPages = 20

// IssuesInStatus lists the issues of the team (its id) whose status has
// the name.
func (a API) IssuesInStatus(ctx context.Context, token, team, status string) ([]Issue, error) {
	var issues []Issue
	var after any
	for range MaxPages {
		var data struct {
			Issues struct {
				Nodes    []issueJSON `json:"nodes"`
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			}
		}
		err := a.graphQL(ctx, token, "ReadyTickets", `query ReadyTickets($team: ID!, $status: String!, $after: String) {
  issues(first: `+fmt.Sprint(PageSize)+`, after: $after,
    filter: { team: { id: { eq: $team } }, state: { name: { eq: $status } } }) {
    nodes { `+issueFields+` }
    pageInfo { hasNextPage endCursor } } }`, map[string]any{"team": team, "status": status, "after": after}, &data)
		if err != nil {
			return nil, err
		}
		for _, j := range data.Issues.Nodes {
			issues = append(issues, j.issue())
		}
		if !data.Issues.PageInfo.HasNextPage {
			break
		}
		after = data.Issues.PageInfo.EndCursor
	}
	return issues, nil
}

// SetStatus sets the issue's status to the status of its team with the
// name.
func (a API) SetStatus(ctx context.Context, token, id, status string) error {
	var data struct {
		Issue *struct {
			Team struct {
				States struct {
					Nodes []struct {
						ID string `json:"id"`
					} `json:"nodes"`
				} `json:"states"`
			} `json:"team"`
		}
	}
	err := a.graphQL(ctx, token, "TicketStatuses", `query TicketStatuses($id: String!, $name: String!) {
  issue(id: $id) { team { states(filter: { name: { eq: $name } }) { nodes { id } } } } }`,
		map[string]any{"id": id, "name": status}, &data)
	if err != nil {
		return err
	}
	if data.Issue == nil {
		return ErrNotFound
	}
	if len(data.Issue.Team.States.Nodes) == 0 {
		return fmt.Errorf("the team of the Linear issue %s has no status %q", id, status)
	}
	var done struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		}
	}
	err = a.graphQL(ctx, token, "SetTicketStatus", `mutation SetTicketStatus($id: String!, $stateId: String!) {
  issueUpdate(id: $id, input: { stateId: $stateId }) { success } }`,
		map[string]any{"id": id, "stateId": data.Issue.Team.States.Nodes[0].ID}, &done)
	if err == nil && !done.IssueUpdate.Success {
		err = fmt.Errorf("Linear did not set the status of the issue %s", id)
	}
	return err
}

// Comment posts a comment of Markdown on the issue.
func (a API) Comment(ctx context.Context, token, id, body string) error {
	var done struct {
		CommentCreate struct {
			Success bool `json:"success"`
		}
	}
	err := a.graphQL(ctx, token, "CommentOnTicket", `mutation CommentOnTicket($id: String!, $body: String!) {
  commentCreate(input: { issueId: $id, body: $body }) { success } }`, map[string]any{"id": id, "body": body}, &done)
	if err == nil && !done.CommentCreate.Success {
		err = fmt.Errorf("Linear did not post the comment on the issue %s", id)
	}
	return err
}

// Signature gives the signature of a webhook, from its SignatureHeader.
func Signature(header http.Header) ([]byte, error) {
	value := header.Get(SignatureHeader)
	if value == "" {
		return nil, errors.New("the webhook has no " + SignatureHeader)
	}
	return hex.DecodeString(value)
}

// Comment is a comment on an issue, with its author: a user (a member, a
// guest, or an app's own user), a bot actor (an integration), or an
// external user (such as a customer from a support tool).
type Comment struct {
	ID, Body  string
	CreatedAt time.Time
	// EditedAt is the time of the last edit of its text (zero is none).
	EditedAt time.Time
	// User is nil when no Linear user wrote the comment.
	User *CommentUser
	// Bot is the name of the bot actor, and External the name of the
	// external user ("" is none).
	Bot, External string
}

// CommentUser is the Linear user who wrote a comment.
type CommentUser struct {
	ID, Name string
	// Guest is a guest of the workspace; App is an app's own user.
	Guest, App bool
}

// CommentsPageSize is the number of comments in one page of Comments,
// and MaxCommentPages the most pages that it reads.
const (
	CommentsPageSize = 50
	MaxCommentPages  = 10
)

// Comments lists the comments on the issue, oldest first, and gives the
// id of the issue's team.
func (a API) Comments(ctx context.Context, token, issueID string) (string, []Comment, error) {
	type actor struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var comments []Comment
	team := ""
	var after any
	for range MaxCommentPages {
		var data struct {
			Issue *struct {
				Team struct {
					ID string `json:"id"`
				} `json:"team"`
				Comments struct {
					Nodes []struct {
						ID        string     `json:"id"`
						Body      string     `json:"body"`
						CreatedAt time.Time  `json:"createdAt"`
						EditedAt  *time.Time `json:"editedAt"`
						User      *struct {
							ID    string `json:"id"`
							Name  string `json:"name"`
							Guest bool   `json:"guest"`
							App   bool   `json:"app"`
						} `json:"user"`
						BotActor     *actor `json:"botActor"`
						ExternalUser *actor `json:"externalUser"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"comments"`
			} `json:"issue"`
		}
		err := a.graphQL(ctx, token, "TicketComments", `query TicketComments($id: String!, $after: String) {
  issue(id: $id) { team { id }
    comments(first: `+fmt.Sprint(CommentsPageSize)+`, after: $after) {
      nodes { id body createdAt editedAt user { id name guest app } botActor { id name } externalUser { id name } }
      pageInfo { hasNextPage endCursor } } } }`, map[string]any{"id": issueID, "after": after}, &data)
		if err != nil {
			return "", nil, err
		}
		if data.Issue == nil {
			return "", nil, ErrNotFound
		}
		team = data.Issue.Team.ID
		for _, n := range data.Issue.Comments.Nodes {
			c := Comment{ID: n.ID, Body: n.Body, CreatedAt: n.CreatedAt}
			if n.EditedAt != nil {
				c.EditedAt = *n.EditedAt
			}
			if n.User != nil {
				c.User = &CommentUser{ID: n.User.ID, Name: n.User.Name, Guest: n.User.Guest, App: n.User.App}
			}
			if n.BotActor != nil {
				c.Bot = cmp.Or(n.BotActor.Name, n.BotActor.ID, "a bot")
			}
			if n.ExternalUser != nil {
				c.External = cmp.Or(n.ExternalUser.Name, n.ExternalUser.ID, "an external user")
			}
			comments = append(comments, c)
		}
		if !data.Issue.Comments.PageInfo.HasNextPage {
			break
		}
		after = data.Issue.Comments.PageInfo.EndCursor
	}
	slices.SortStableFunc(comments, func(x, y Comment) int { return x.CreatedAt.Compare(y.CreatedAt) })
	return team, comments, nil
}

// Webhook is what a Linear webhook says.
type Webhook struct {
	// Type is the kind of entity, such as Issue or Comment.
	Type, Action string
	// Issue is the issue of the change: the entity of an Issue webhook,
	// the issue of a Comment ("" for an entity of no issue).
	Issue string
	// Timestamp is the webhookTimestamp (zero is none).
	Timestamp time.Time
}

// ParseWebhook reads a webhook's body.
func ParseWebhook(body []byte) (Webhook, error) {
	var b struct {
		Type             string `json:"type"`
		Action           string `json:"action"`
		WebhookTimestamp int64  `json:"webhookTimestamp"`
		Data             struct {
			ID      string `json:"id"`
			IssueID string `json:"issueId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return Webhook{}, err
	}
	w := Webhook{Type: b.Type, Action: b.Action}
	if b.WebhookTimestamp > 0 {
		w.Timestamp = time.UnixMilli(b.WebhookTimestamp)
	}
	switch {
	case b.Type == "Issue":
		w.Issue = b.Data.ID
	case b.Data.IssueID != "":
		w.Issue = b.Data.IssueID
	}
	return w, nil
}
