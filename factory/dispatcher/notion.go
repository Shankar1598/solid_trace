package dispatcher

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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/solidtrace/cli/dispatcherapi"
)

// Notion as a Tracker (W4). A Notion connection is an internal
// integration's token, with the capabilities to read content, read
// comments and insert comments, and the database that it reads: each page
// of the database is a ticket. The status is the database's status
// property, and the routing field is a select property (by default
// "Project"). Notion's webhooks are the fast path. The poll is every 30
// seconds while the connection has no verified webhook, and every 5
// minutes after its first one, so an install with no public route works
// by the poll alone.

// The Notion kind's names and limits.
const (
	NotionKind = "notion"
	// NotionAPIURL is the base of Notion's own API.
	NotionAPIURL = "https://api.notion.com"
	// NotionVersion is the version of Notion's API that the kind speaks.
	NotionVersion = "2022-06-28"
	// NotionSignatureHeader carries "sha256=" and the hex HMAC-SHA256 of
	// the raw body, with the subscription's verification token as the key.
	NotionSignatureHeader = "X-Notion-Signature"
	// NotionPoll is the backstop of a connection with no verified webhook,
	// and NotionBackstop the backstop after its first one.
	NotionPoll     = 30 * time.Second
	NotionBackstop = 5 * time.Minute

	notionRoutingField = "Project"
	// notionPageSize is the most results of one list call (Notion's own
	// limit), and notionMaxPages the most list calls of one poll.
	notionPageSize = 100
	notionMaxPages = 10
	// notionMaxBlockCalls is the most calls that read one page's content,
	// and notionMaxDepth the most levels of nested blocks.
	notionMaxBlockCalls = 25
	notionMaxDepth      = 3
	// notionMaxBody is the longest body of a ticket, as the longest
	// prompt of the generic webhook.
	notionMaxBody = 64 << 10
	// notionMaxAnswer is the largest answer that the kind reads.
	notionMaxAnswer = 8 << 20
	// notionTextLimit is the most UTF-16 units of one rich text object,
	// and notionMaxParts the most rich text objects of one comment.
	notionTextLimit = 2000
	notionMaxParts  = 100
)

// NewNotion is the Notion kind, which calls the API at apiURL ("" is
// Notion's own) with client.
func NewNotion(apiURL string, client *http.Client) Tracker {
	return newNotion(apiURL, client, time.Now)
}

func newNotion(apiURL string, client *http.Client, now func() time.Time) *notion {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &notion{api: strings.TrimRight(cmp.Or(apiURL, NotionAPIURL), "/"), http: client, now: now,
		users: map[[2]string]notionUser{}}
}

type notion struct {
	api  string
	http *http.Client
	now  func() time.Time
	// users keeps each Notion user that the kind read, by connection id
	// and user id: each connection's integration has its own
	// capabilities.
	mu    sync.Mutex
	users map[[2]string]notionUser
}

// notionUser is a user's name, or the time when Notion refused to give it.
type notionUser struct {
	name    string
	refused time.Time
}

// notionUserRetry is how long the kind keeps Notion's refusal to give a
// user: an Admin can give the integration the capability to read user
// information.
const notionUserRetry = 10 * time.Minute

func (*notion) Kind() string { return NotionKind }

func (*notion) Signature(header http.Header, body []byte) ([]byte, []byte, error) {
	signature, ok := strings.CutPrefix(header.Get(NotionSignatureHeader), "sha256=")
	if !ok {
		return nil, nil, errors.New("the delivery has no " + NotionSignatureHeader)
	}
	mac, err := hex.DecodeString(signature)
	return body, mac, err
}

// notionEvent is the part of a Notion webhook event that the kind reads.
type notionEvent struct {
	ID     string `json:"id"`
	Entity struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"entity"`
	Data struct {
		PageID string `json:"page_id"`
	} `json:"data"`
}

// Delivery reads an event: a page event announces that page, and a
// comment event (comment.created) the page of the comment. Another event,
// such as a change of the database, announces the whole connection.
// Notion's events have no time window.
func (*notion) Delivery(_ http.Header, body []byte, _ time.Time) (Delivery, error) {
	var event notionEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return Delivery{}, err
	}
	var page string
	switch event.Entity.Type {
	case "page":
		page = event.Entity.ID
	case "comment":
		page = event.Data.PageID
	}
	if page == "" {
		return Delivery{ID: event.ID}, nil
	}
	id, ok := notionID(page)
	if !ok {
		return Delivery{}, fmt.Errorf("the event names the page %q, which is not a Notion id", page)
	}
	return Delivery{ID: event.ID, Entity: id}, nil
}

func (*notion) Backstop(source WebhookSource) time.Duration {
	if source.LastWebhook.IsZero() {
		return NotionPoll
	}
	return NotionBackstop
}

func (*notion) Defaults() dispatcherapi.TrackerSettings {
	return dispatcherapi.TrackerSettings{ReadyStatus: defaultReadyStatus, RoutingField: notionRoutingField}
}

// checkSettings keeps the database as its id: an Admin can give the id or
// a link to the database.
func (*notion) checkSettings(s dispatcherapi.TrackerSettings) (dispatcherapi.TrackerSettings, error) {
	id, ok := notionDatabase(s.Database)
	if !ok {
		return s, errors.New("a Notion connection needs its database: its id, or the link to it " +
			"(https://www.notion.so/<workspace>/<name>-<32 hex digits>), not " + quote(s.Database))
	}
	s.Database = id
	return s, nil
}

// verificationToken is a Notion verification token.
var verificationToken = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// setupToken reads Notion's verification request: when an Admin makes a
// webhook subscription, Notion sends one unsigned request that holds only
// the verification token. The Admin pastes the token into Notion to
// verify the subscription, and saves it as the connection's webhook
// secret: Notion signs each event with it.
func (*notion) setupToken(header http.Header, body []byte) (string, bool) {
	if header.Get(NotionSignatureHeader) != "" {
		return "", false
	}
	var request struct {
		VerificationToken string `json:"verification_token"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.More() ||
		!verificationToken.MatchString(request.VerificationToken) || len(request.VerificationToken) > maxWebhookSecret {
		return "", false
	}
	return request.VerificationToken, true
}

// notionIDForm is an id of Notion: 32 hex digits with no dashes, or with
// the dashes of Notion's own form, 8-4-4-4-12.
var notionIDForm = regexp.MustCompile(`^(?i)(?:[0-9a-f]{32}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)

// notionID gives a Notion id (with or without dashes) in Notion's own
// form, 8-4-4-4-12 lower-case hex digits. An id with its dashes in other
// places is no id.
func notionID(s string) (string, bool) {
	if !notionIDForm.MatchString(s) {
		return "", false
	}
	h := strings.ToLower(strings.ReplaceAll(s, "-", ""))
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], true
}

// notionDatabase gives the id of a database from its id or the link to it.
// The id in a link is the end of its last path part, after the name; the
// query (?v=) names a view, not the database.
func notionDatabase(s string) (string, bool) {
	if !strings.Contains(s, "://") {
		return notionID(s)
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", false
	}
	last := u.Path[strings.LastIndex(u.Path, "/")+1:]
	if len(last) > 32 {
		last = last[len(last)-32:]
	}
	return notionID(last)
}

// notionError is an answer of Notion's API that is not a success.
type notionError struct {
	method, path  string
	status        int
	code, message string
}

func (e *notionError) Error() string {
	text := fmt.Sprintf("Notion answered %s %s with HTTP %d", e.method, e.path, e.status)
	if e.code != "" {
		text += " " + e.code
	}
	if e.message != "" {
		text += ": " + e.message
	}
	switch e.status {
	case http.StatusUnauthorized:
		text += " (the connection's token is not valid: an Admin saves a new one with " +
			"'solidtrace connections set NAME --kind notion')"
	case http.StatusForbidden:
		text += " (the integration needs the capabilities to read content, read comments and insert comments)"
	case http.StatusNotFound:
		text += " (share the database with the integration in Notion: ... → Connections)"
	case http.StatusTooManyRequests:
		text += " (Notion's rate limit: the next step tries again)"
	}
	return text
}

func notFound(err error) bool {
	var e *notionError
	return errors.As(err, &e) && e.status == http.StatusNotFound
}

// call sends one request to Notion's API as the connection, and reads the
// JSON answer into out.
func (n *notion) call(ctx context.Context, c TrackerConnection, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, n.api+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Notion-Version", NotionVersion)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := n.http.Do(req)
	if err != nil {
		return fmt.Errorf("call Notion: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, notionMaxAnswer+1))
	if err != nil {
		return fmt.Errorf("read Notion's answer to %s %s: %w", method, path, err)
	}
	if resp.StatusCode != http.StatusOK {
		e := &notionError{method: method, path: path, status: resp.StatusCode}
		var problem struct{ Code, Message string }
		if json.Unmarshal(data, &problem) == nil {
			e.code, e.message = problem.Code, problem.Message
		}
		return e
	}
	if len(data) > notionMaxAnswer {
		return fmt.Errorf("Notion's answer to %s %s is over %d bytes", method, path, notionMaxAnswer)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("read Notion's answer to %s %s: %w", method, path, err)
	}
	return nil
}

// database is the id of the connection's database.
func (*notion) database(c TrackerConnection) (string, error) {
	if id, ok := notionID(c.Settings.Database); ok {
		return id, nil
	}
	return "", errors.New("the Notion connection " + c.Name + " names no database: an Admin saves it with " +
		"PUT /api/v1/connections/" + c.Name + "/tracker and {\"database\": \"...\"}")
}

// notionText is a rich text object.
type notionText struct {
	PlainText string `json:"plain_text"`
}

func plain(texts []notionText) string {
	var sb strings.Builder
	for _, t := range texts {
		sb.WriteString(t.PlainText)
	}
	return sb.String()
}

// notionPage is the part of a page object that the kind reads.
type notionPage struct {
	ID             string `json:"id"`
	URL            string `json:"url"`
	LastEditedTime string `json:"last_edited_time"`
	LastEditedBy   struct {
		ID string `json:"id"`
	} `json:"last_edited_by"`
	Parent struct {
		DatabaseID string `json:"database_id"`
	} `json:"parent"`
	Archived   bool `json:"archived"`
	InTrash    bool `json:"in_trash"`
	Properties map[string]struct {
		Type   string       `json:"type"`
		Title  []notionText `json:"title"`
		Status *struct {
			Name string `json:"name"`
		} `json:"status"`
		Select *struct {
			Name string `json:"name"`
		} `json:"select"`
	} `json:"properties"`
}

func (p notionPage) types() map[string]string {
	types := map[string]string{}
	for name, property := range p.Properties {
		types[name] = property.Type
	}
	return types
}

// statusProperty is the name of the status property among the properties
// (name → type): the only one of type status, else the one named
// "Status".
func statusProperty(types map[string]string) (string, error) {
	var names []string
	for name, kind := range types {
		if kind == "status" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	switch {
	case len(names) == 1:
		return names[0], nil
	case slices.Contains(names, "Status"):
		return "Status", nil
	case len(names) == 0:
		return "", errors.New("the Notion database has no property of the type Status")
	}
	return "", errors.New("the Notion database has several Status properties and none is named \"Status\": " +
		strings.Join(names, ", "))
}

// page reads a page of the connection's database. A page of another
// database, or in the trash, is ErrNoTicket.
func (n *notion) page(ctx context.Context, c TrackerConnection, id string) (notionPage, error) {
	db, err := n.database(c)
	if err != nil {
		return notionPage{}, err
	}
	id, ok := notionID(id)
	if !ok {
		return notionPage{}, ErrNoTicket
	}
	var p notionPage
	err = n.call(ctx, c, http.MethodGet, "/v1/pages/"+id, nil, &p)
	if notFound(err) {
		return p, ErrNoTicket
	}
	if err != nil {
		return p, err
	}
	if parent, _ := notionID(p.Parent.DatabaseID); parent != db || p.Archived || p.InTrash {
		return p, ErrNoTicket
	}
	return p, nil
}

func (n *notion) Ticket(ctx context.Context, c TrackerConnection, id string) (Ticket, error) {
	p, err := n.page(ctx, c, id)
	if err != nil {
		return Ticket{}, err
	}
	return n.ticket(ctx, c, p)
}

func (n *notion) ReadyTickets(ctx context.Context, c TrackerConnection) ([]Ticket, error) {
	db, err := n.database(c)
	if err != nil {
		return nil, err
	}
	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := n.call(ctx, c, http.MethodGet, "/v1/databases/"+db, nil, &schema); err != nil {
		return nil, err
	}
	types := map[string]string{}
	for name, property := range schema.Properties {
		types[name] = property.Type
	}
	status, err := statusProperty(types)
	if err != nil {
		return nil, err
	}
	var tickets []Ticket
	cursor := ""
	for range notionMaxPages {
		query := map[string]any{"page_size": notionPageSize,
			"filter": map[string]any{"property": status, "status": map[string]string{"equals": c.Settings.ReadyStatus}}}
		if cursor != "" {
			query["start_cursor"] = cursor
		}
		var list struct {
			Results    []notionPage `json:"results"`
			HasMore    bool         `json:"has_more"`
			NextCursor string       `json:"next_cursor"`
		}
		if err := n.call(ctx, c, http.MethodPost, "/v1/databases/"+db+"/query", query, &list); err != nil {
			return nil, err
		}
		for _, p := range list.Results {
			if p.Archived || p.InTrash {
				continue
			}
			ticket, err := n.ticket(ctx, c, p)
			if err != nil {
				return nil, err
			}
			tickets = append(tickets, ticket)
		}
		if !list.HasMore || list.NextCursor == "" {
			break
		}
		cursor = list.NextCursor
	}
	return tickets, nil
}

// ticket is a page as a ticket, with no Body. For a page in the ready
// status, it reads the name of the user who edited it last.
func (n *notion) ticket(ctx context.Context, c TrackerConnection, p notionPage) (Ticket, error) {
	id, ok := notionID(p.ID)
	if !ok {
		return Ticket{}, fmt.Errorf("Notion gave the page id %q", p.ID)
	}
	ticket := Ticket{ID: id, URL: p.URL}
	if name, err := statusProperty(p.types()); err == nil && p.Properties[name].Status != nil {
		ticket.Status = p.Properties[name].Status.Name
	}
	for _, property := range p.Properties {
		if property.Type == "title" {
			ticket.Title = plain(property.Title)
		}
	}
	if property, ok := p.Properties[c.Settings.RoutingField]; ok && property.Type == "select" && property.Select != nil {
		ticket.Route = property.Select.Name
	}
	// last_edited_time has whole minutes: the status and the route are
	// part of the edit, so that a change of either in the same minute is
	// a new edit. A page that is back at the same status and route in the
	// same minute is the same edit.
	ticket.Edit = p.LastEditedTime + "|" + ticket.Status + "|" + ticket.Route
	// Notion gives no history of a property: the user who set the status
	// is the user who edited the page last. Only a ticket in the ready
	// status needs the user's name.
	ticket.StatusBy = TrackerUser{ID: p.LastEditedBy.ID}
	if ticket.Status == c.Settings.ReadyStatus {
		ticket.StatusBy.Name = n.user(ctx, c, p.LastEditedBy.ID)
	}
	return ticket, nil
}

// ticketBody reads the page's content: the kind is a bodyReader.
func (n *notion) ticketBody(ctx context.Context, c TrackerConnection, id string) (string, error) {
	return n.body(ctx, c, id)
}

// user is the name of a Notion user. It is the id when the integration
// cannot read user information.
func (n *notion) user(ctx context.Context, c TrackerConnection, id string) string {
	if id == "" {
		return ""
	}
	key := [2]string{c.ID, id}
	n.mu.Lock()
	kept, ok := n.users[key]
	n.mu.Unlock()
	switch {
	case ok && kept.refused.IsZero():
		return kept.name
	case ok && n.now().Sub(kept.refused) < notionUserRetry:
		return id
	}
	var u struct {
		Name string `json:"name"`
	}
	err := n.call(ctx, c, http.MethodGet, "/v1/users/"+url.PathEscape(id), nil, &u)
	var e *notionError
	if err != nil && !(errors.As(err, &e) && (e.status == http.StatusForbidden || e.status == http.StatusNotFound)) {
		return id // try again at the next read
	}
	kept = notionUser{name: cmp.Or(u.Name, id)}
	if err != nil {
		kept.refused = n.now()
	}
	n.mu.Lock()
	n.users[key] = kept
	n.mu.Unlock()
	return kept.name
}

// notionBlock is a block of a page's content, with the text of a block
// type that holds text.
type notionBlock struct {
	id, kind    string
	hasChildren bool
	text        string
	checked     bool
	// cells are the texts of a table row.
	cells []string
}

func (b *notionBlock) UnmarshalJSON(data []byte) error {
	var head struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		HasChildren bool   `json:"has_children"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	var content struct {
		RichText []notionText   `json:"rich_text"`
		Checked  bool           `json:"checked"`
		Cells    [][]notionText `json:"cells"`
	}
	if raw, ok := all[head.Type]; ok {
		_ = json.Unmarshal(raw, &content) // a block type with no text
	}
	*b = notionBlock{id: head.ID, kind: head.Type, hasChildren: head.HasChildren, text: plain(content.RichText),
		checked: content.Checked}
	for _, cell := range content.Cells {
		b.cells = append(b.cells, plain(cell))
	}
	return nil
}

// layout is true for a block that holds no text of its own and shows its
// children: a column, a synced block, a table, or a toggle or callout with
// no title. Its children are at its own level.
func (b notionBlock) layout() bool {
	switch b.kind {
	case "column_list", "column", "synced_block", "table":
		return true
	case "paragraph", "toggle", "callout":
		return b.text == ""
	}
	return false
}

// line is a block as one line of plain text: false for a block that holds
// no text, such as an image, a column, or a child page (another ticket).
func (b notionBlock) line() (string, bool) {
	switch b.kind {
	case "paragraph", "toggle", "callout":
		return b.text, b.text != ""
	case "heading_1", "heading_2", "heading_3":
		level, _ := strconv.Atoi(b.kind[len(b.kind)-1:])
		return strings.Repeat("#", level) + " " + b.text, true
	case "bulleted_list_item":
		return "- " + b.text, true
	case "numbered_list_item":
		return "1. " + b.text, true
	case "to_do":
		if b.checked {
			return "[x] " + b.text, true
		}
		return "[ ] " + b.text, true
	case "quote":
		return "> " + b.text, true
	case "code":
		return "```\n" + b.text + "\n```", true
	case "divider":
		return "---", true
	case "table_row":
		return "| " + strings.Join(b.cells, " | ") + " |", true
	}
	return "", false
}

// body is a page's content as plain text, at most notionMaxBody bytes. It
// reads at most notionMaxBlockCalls lists of blocks and notionMaxDepth
// levels, and ends with a note where it stopped.
func (n *notion) body(ctx context.Context, c TrackerConnection, pageID string) (string, error) {
	var lines []string
	calls, size, cut := 0, 0, false
	var walk func(id string, depth int) error
	walk = func(id string, depth int) error {
		cursor := ""
		for {
			if calls >= notionMaxBlockCalls || size > notionMaxBody {
				cut = true
				return nil
			}
			calls++
			query := "?page_size=" + strconv.Itoa(notionPageSize)
			if cursor != "" {
				query += "&start_cursor=" + url.QueryEscape(cursor)
			}
			var list struct {
				Results    []notionBlock `json:"results"`
				HasMore    bool          `json:"has_more"`
				NextCursor string        `json:"next_cursor"`
			}
			if err := n.call(ctx, c, http.MethodGet, "/v1/blocks/"+url.PathEscape(id)+"/children"+query, nil, &list); err != nil {
				return err
			}
			for _, b := range list.Results {
				line, ok := b.line()
				if ok {
					line = strings.Repeat("  ", depth) + line
					lines = append(lines, line)
					size += len(line) + 1
				}
				switch {
				case !b.hasChildren && (ok || b.text == ""):
				case b.kind == "child_page" || b.kind == "child_database" || b.kind == "code":
					// Another ticket or database, or no children.
				case b.layout():
					if err := walk(b.id, depth); err != nil {
						return err
					}
				case !ok || depth+1 >= notionMaxDepth:
					// A block type that the kind cannot read, or
					// too deep.
					cut = true
				default:
					if err := walk(b.id, depth+1); err != nil {
						return err
					}
				}
			}
			if !list.HasMore || list.NextCursor == "" {
				return nil
			}
			cursor = list.NextCursor
		}
	}
	if err := walk(pageID, 0); err != nil {
		return "", err
	}
	body := strings.Join(lines, "\n")
	if len(body) > notionMaxBody {
		body = strings.ToValidUTF8(body[:notionMaxBody], "")
		cut = true
	}
	if cut {
		body += "\n\n(The page has more content: read the rest in Notion.)"
	}
	return body, nil
}

func (n *notion) SetStatus(ctx context.Context, c TrackerConnection, id, status string) error {
	p, err := n.page(ctx, c, id)
	if err != nil {
		return err
	}
	property, err := statusProperty(p.types())
	if err != nil {
		return err
	}
	patch := map[string]any{"properties": map[string]any{property: map[string]any{"status": map[string]string{"name": status}}}}
	var updated notionPage
	return n.call(ctx, c, http.MethodPatch, "/v1/pages/"+p.ID, patch, &updated)
}

func (n *notion) Comment(ctx context.Context, c TrackerConnection, id, body string) error {
	id, ok := notionID(id)
	if !ok {
		return ErrNoTicket
	}
	parts := []map[string]any{}
	for _, part := range notionParts(body) {
		parts = append(parts, map[string]any{"type": "text", "text": map[string]string{"content": part}})
	}
	comment := map[string]any{"parent": map[string]string{"page_id": id}, "rich_text": parts}
	var made struct {
		ID string `json:"id"`
	}
	err := n.call(ctx, c, http.MethodPost, "/v1/comments", comment, &made)
	if notFound(err) {
		return ErrNoTicket
	}
	return err
}

// notionParts cuts a text into rich text objects of at most
// notionTextLimit UTF-16 units (Notion counts its strings so), at most
// notionMaxParts of them. A longer text ends with "…".
func notionParts(text string) []string {
	var parts []string
	var part strings.Builder
	units := 0
	for _, r := range text {
		n := utf16.RuneLen(r)
		if n < 0 {
			n = 1
		}
		if units+n > notionTextLimit {
			parts = append(parts, part.String())
			part.Reset()
			units = 0
			if len(parts) == notionMaxParts {
				last := []rune(parts[len(parts)-1])
				parts[len(parts)-1] = string(last[:len(last)-1]) + "…"
				return parts
			}
		}
		part.WriteRune(r)
		units += n
	}
	if part.Len() > 0 || len(parts) == 0 {
		parts = append(parts, part.String())
	}
	return parts
}
