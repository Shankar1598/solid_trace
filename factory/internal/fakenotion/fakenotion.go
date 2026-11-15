// Package fakenotion is the fake Notion of Seam 3: an HTTP server that
// speaks the part of Notion's public API that the Dispatcher's Notion
// kind calls (API version 2022-06-28), with databases, pages, their
// blocks, users and comments in memory. A test changes the pages as a
// Notion user would, signs and sends the webhooks that Notion sends, and
// reads the statuses and the comments that the Dispatcher wrote.
//
// It follows the spec's Notion API facts: a database page has url and
// public_url, last_edited_time has whole minutes, and a comment on a page
// gives a comment.created event whose data.page_id is the page. Ticket
// 88 checks them on the real Notion API.
package fakenotion

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
)

// Version is the Notion-Version that the fake accepts.
const Version = "2022-06-28"

// SignatureHeader carries "sha256=" and the hex HMAC-SHA256 of the raw
// body, with the subscription's verification token as the key.
const SignatureHeader = "X-Notion-Signature"

// BotID is the id of the integration's bot user: the author of each
// comment that the Dispatcher posts.
const BotID = "b0000000-0000-4000-8000-000000000001"

// User is a Notion user.
type User struct{ ID, Name string }

// Block is a block of a page's content. Type is a Notion block type, such
// as paragraph, heading_2, bulleted_list_item, to_do, code, column_list or
// table. Cells are the texts of a table_row.
type Block struct {
	Type, Text string
	Checked    bool
	Cells      []string
	Children   []Block
}

// Page is a page of a database: a ticket.
type Page struct {
	ID, Title, Status string
	// Select are the values of the page's select properties, by name,
	// such as {"Project": "shop"}.
	Select map[string]string
	Blocks []Block
	// Public is true for a page that is published to the web.
	Public bool
}

// Comment is a comment on a page.
type Comment struct {
	ID, Page, Text string
	By             string
	// Parts is how many rich text objects the comment had.
	Parts int
}

type page struct {
	Page
	database string
	edited   time.Time
	editedBy string
	// archived and inTrash are Notion's two flags of a page in the
	// trash.
	archived, inTrash bool
}

// gone is true for a page in the trash.
func (p *page) gone() bool { return p.archived || p.inTrash }

type database struct {
	id string
	// status is the name of the status property, and statuses its
	// options.
	status   string
	statuses []string
	selects  []string
}

// Notion is the fake Notion API, for each integration token together.
type Notion struct {
	server *httptest.Server
	// Now is the clock of last_edited_time (time.Now when nil).
	Now func() time.Time
	// PageSize is the most results that a list gives (100 when 0).
	PageSize int
	// ListTrashed is true when a database query also gives the pages in
	// the trash, as Notion can do for a short time after a page moves
	// there.
	ListTrashed bool

	mu        sync.Mutex
	tokens    map[string]bool
	databases map[string]*database
	pages     map[string]*page
	users     map[string]User
	comments  []Comment
	refuse    map[string]int
	calls     []string
	usersOff  map[string]bool // by token; "" is each token
	blockKids map[string][]Block
}

// New is a fake Notion with no databases, served until the test ends.
func New(t testing.TB) *Notion {
	n := &Notion{tokens: map[string]bool{}, databases: map[string]*database{}, pages: map[string]*page{},
		users: map[string]User{}, usersOff: map[string]bool{}, refuse: map[string]int{}, blockKids: map[string][]Block{}}
	n.server = httptest.NewServer(http.HandlerFunc(n.serve))
	t.Cleanup(n.server.Close)
	return n
}

// URL is the base of the API, as https://api.notion.com.
func (n *Notion) URL() string { return n.server.URL }

// Token adds an integration token that the API accepts.
func (n *Notion) Token(token string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.tokens[token] = true
}

// Database adds a database with the status property "Status" and its
// options, and the select properties.
func (n *Notion) Database(id string, statuses []string, selects ...string) {
	n.DatabaseWith(id, "Status", statuses, selects...)
}

// DatabaseWith adds a database whose status property has the name status.
func (n *Notion) DatabaseWith(id, status string, statuses []string, selects ...string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.databases[id] = &database{id: id, status: status, statuses: statuses, selects: selects}
}

// AddUser adds a user that GET /v1/users/{id} gives.
func (n *Notion) AddUser(u User) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.users[u.ID] = u
}

// RefuseUsers makes GET /v1/users/{id} answer 403 to the tokens (each
// token when none), as for an integration with no capability to read user
// information.
func (n *Notion) RefuseUsers(tokens ...string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(tokens) == 0 {
		tokens = []string{""}
	}
	for _, token := range tokens {
		n.usersOff[token] = true
	}
}

// AllowUsers undoes RefuseUsers, as when an Admin gives the integration
// the capability to read user information.
func (n *Notion) AllowUsers() {
	n.mu.Lock()
	defer n.mu.Unlock()
	clear(n.usersOff)
}

// Put makes or replaces a page of the database, as the user. It is an
// edit.
func (n *Notion) Put(databaseID string, p Page, by User) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.databases[databaseID] == nil {
		panic("fakenotion: no database " + databaseID)
	}
	n.pages[p.ID] = &page{Page: p, database: databaseID, edited: n.minute(), editedBy: by.ID}
}

// SetStatus sets a page's status as the user. It is an edit.
func (n *Notion) SetStatus(id, status string, by User) {
	n.edit(id, by.ID, func(p *page) { p.Status = status })
}

// SetSelect sets a select property of a page as the user. It is an edit.
func (n *Notion) SetSelect(id, property, value string, by User) {
	n.edit(id, by.ID, func(p *page) {
		p.Select = cloneMap(p.Select)
		p.Select[property] = value
	})
}

// Trash moves a page to the trash.
func (n *Notion) Trash(id string) { n.Flag(id, true, true) }

// Flag sets the archived and the in_trash flags of a page one by one, as
// an API version that has only one of the two gives them.
func (n *Notion) Flag(id string, archived, inTrash bool) {
	n.edit(id, "", func(p *page) { p.archived, p.inTrash = archived, inTrash })
}

func (n *Notion) edit(id, by string, f func(*page)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	p := n.pages[id]
	if p == nil {
		panic("fakenotion: no page " + id)
	}
	f(p)
	p.edited = n.minute()
	if by != "" {
		p.editedBy = by
	}
}

// minute is the time of an edit: Notion's last_edited_time has whole
// minutes.
func (n *Notion) minute() time.Time {
	now := time.Now
	if n.Now != nil {
		now = n.Now
	}
	return now().UTC().Truncate(time.Minute)
}

// Status is a page's status now.
func (n *Notion) Status(id string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if p := n.pages[id]; p != nil {
		return p.Status
	}
	return ""
}

// Comments are the comments on the page, oldest first.
func (n *Notion) Comments(pageID string) []Comment {
	n.mu.Lock()
	defer n.mu.Unlock()
	var list []Comment
	for _, c := range n.comments {
		if c.Page == pageID {
			list = append(list, c)
		}
	}
	return list
}

// FailNext makes the next n calls whose "METHOD /path" starts with
// prefix answer with status.
func (n *Notion) FailNext(prefix string, status, count int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.refuse[prefix+"\x00"+strconv.Itoa(status)] = count
}

// Calls are the "METHOD /path" of each call so far.
func (n *Notion) Calls() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.calls)
}

// NewID is a new Notion id (a UUID).
func NewID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// PageURL is the url of a page.
func PageURL(id string) string { return "https://www.notion.so/" + strings.ReplaceAll(id, "-", "") }

// Sign is the X-Notion-Signature of a body.
func Sign(token string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Event is a webhook event of Notion.
func Event(eventID, eventType, entityType, entityID string, data map[string]any) []byte {
	body, _ := json.Marshal(map[string]any{
		"id": eventID, "timestamp": "2026-11-14T09:00:00.000Z", "workspace_id": "w0000000-0000-4000-8000-000000000001",
		"subscription_id": "s0000000-0000-4000-8000-000000000001", "integration_id": "i0000000-0000-4000-8000-000000000001",
		"type": eventType, "authors": []map[string]string{{"id": "u-person", "type": "person"}}, "attempt_number": 1,
		"entity": map[string]string{"id": entityID, "type": entityType}, "data": data,
	})
	return body
}

// PageEvent is a page event, such as page.properties_updated.
func PageEvent(eventID, eventType, pageID string) []byte {
	return Event(eventID, eventType, "page", pageID, map[string]any{
		"parent": map[string]string{"id": "d-parent", "type": "database"}, "updated_properties": []string{"Status"}})
}

// CommentEvent is a comment.created event of a comment on the page.
func CommentEvent(eventID, pageID string) []byte {
	return Event(eventID, "comment.created", "comment", NewID(), map[string]any{
		"page_id": pageID, "parent": map[string]string{"id": pageID, "type": "page"}})
}

// Delivery is the headers of a delivery of body, signed with token.
func Delivery(token string, body []byte) http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set(SignatureHeader, Sign(token, body))
	return h
}

// Verification is the request that Notion sends when an Admin makes a
// webhook subscription: it has the verification token and no signature.
func Verification(token string) (http.Header, []byte) {
	body, _ := json.Marshal(map[string]string{"verification_token": token})
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	return h, body
}

func cloneMap(m map[string]string) map[string]string {
	c := map[string]string{}
	for k, v := range m {
		c[k] = v
	}
	return c
}

func fail(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "error", "status": status, "code": code, "message": message})
}

func answer(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (n *Notion) serve(w http.ResponseWriter, r *http.Request) {
	call := r.Method + " " + r.URL.Path
	n.mu.Lock()
	n.calls = append(n.calls, call)
	for key, count := range n.refuse {
		prefix, status, _ := strings.Cut(key, "\x00")
		if count > 0 && strings.HasPrefix(call, prefix) {
			n.refuse[key] = count - 1
			n.mu.Unlock()
			code, _ := strconv.Atoi(status)
			if code == http.StatusTooManyRequests {
				w.Header().Set("Retry-After", "1")
			}
			fail(w, code, "rate_limited", "fakenotion: a failure that the test asked for")
			return
		}
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch {
	case !ok || !n.tokens[token]:
		n.mu.Unlock()
		fail(w, http.StatusUnauthorized, "unauthorized", "API token is invalid.")
		return
	case r.Header.Get("Notion-Version") != Version:
		n.mu.Unlock()
		fail(w, http.StatusBadRequest, "missing_version", "Notion-Version header failed validation: "+
			strconv.Quote(r.Header.Get("Notion-Version")))
		return
	}
	defer n.mu.Unlock()
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/"), "/")
	switch {
	case r.Method == "GET" && len(parts) == 2 && parts[0] == "databases":
		n.getDatabase(w, parts[1])
	case r.Method == "POST" && len(parts) == 3 && parts[0] == "databases" && parts[2] == "query":
		n.query(w, r, parts[1])
	case r.Method == "GET" && len(parts) == 2 && parts[0] == "pages":
		n.getPage(w, parts[1])
	case r.Method == "PATCH" && len(parts) == 2 && parts[0] == "pages":
		n.patchPage(w, r, parts[1])
	case r.Method == "GET" && len(parts) == 3 && parts[0] == "blocks" && parts[2] == "children":
		n.children(w, r, parts[1])
	case r.Method == "POST" && len(parts) == 1 && parts[0] == "comments":
		n.comment(w, r)
	case r.Method == "GET" && len(parts) == 2 && parts[0] == "users":
		n.user(w, token, parts[1])
	default:
		fail(w, http.StatusBadRequest, "invalid_request_url", "Invalid request URL.")
	}
}

// pageOf finds a page by an id with or without dashes.
func (n *Notion) pageOf(id string) *page {
	for _, p := range n.pages {
		if strings.ReplaceAll(p.ID, "-", "") == strings.ReplaceAll(id, "-", "") {
			return p
		}
	}
	return nil
}

func (n *Notion) databaseOf(id string) *database {
	for _, d := range n.databases {
		if strings.ReplaceAll(d.id, "-", "") == strings.ReplaceAll(id, "-", "") {
			return d
		}
	}
	return nil
}

func notFound(w http.ResponseWriter, id string) {
	fail(w, http.StatusNotFound, "object_not_found", "Could not find object with ID: "+id+
		". Make sure the relevant pages and databases are shared with your integration.")
}

func (n *Notion) getDatabase(w http.ResponseWriter, id string) {
	d := n.databaseOf(id)
	if d == nil {
		notFound(w, id)
		return
	}
	properties := map[string]any{
		"Name":   map[string]any{"id": "title", "name": "Name", "type": "title", "title": map[string]any{}},
		d.status: map[string]any{"id": "st", "name": d.status, "type": "status", "status": map[string]any{"options": options(d.statuses)}},
	}
	for _, s := range d.selects {
		properties[s] = map[string]any{"id": "se-" + s, "name": s, "type": "select", "select": map[string]any{"options": []any{}}}
	}
	answer(w, map[string]any{"object": "database", "id": d.id, "title": []any{text("Tickets")}, "properties": properties})
}

func options(names []string) []map[string]string {
	list := make([]map[string]string, len(names))
	for i, name := range names {
		list[i] = map[string]string{"id": "o" + strconv.Itoa(i), "name": name, "color": "default"}
	}
	return list
}

func text(s string) map[string]any {
	return map[string]any{"type": "text", "text": map[string]any{"content": s, "link": nil}, "plain_text": s, "href": nil}
}

func (n *Notion) pageJSON(p *page) map[string]any {
	d := n.databases[p.database]
	properties := map[string]any{
		"Name": map[string]any{"id": "title", "type": "title", "title": []any{text(p.Title)}},
	}
	var status any
	if p.Status != "" {
		status = map[string]string{"id": "o", "name": p.Status, "color": "default"}
	}
	properties[d.status] = map[string]any{"id": "st", "type": "status", "status": status}
	for _, s := range d.selects {
		var value any
		if v := p.Select[s]; v != "" {
			value = map[string]string{"id": "v", "name": v, "color": "default"}
		}
		properties[s] = map[string]any{"id": "se-" + s, "type": "select", "select": value}
	}
	var public any
	if p.Public {
		public = "https://example.notion.site/" + strings.ReplaceAll(p.ID, "-", "")
	}
	return map[string]any{
		"object": "page", "id": p.ID,
		"created_time":     "2026-11-14T08:00:00.000Z",
		"last_edited_time": p.edited.Format("2006-01-02T15:04:05.000Z"),
		"created_by":       map[string]string{"object": "user", "id": p.editedBy},
		"last_edited_by":   map[string]string{"object": "user", "id": p.editedBy},
		"parent":           map[string]string{"type": "database_id", "database_id": p.database},
		"archived":         p.archived, "in_trash": p.inTrash,
		"properties": properties, "url": PageURL(p.ID), "public_url": public,
	}
}

func (n *Notion) query(w http.ResponseWriter, r *http.Request, id string) {
	d := n.databaseOf(id)
	if d == nil {
		notFound(w, id)
		return
	}
	var q struct {
		Filter struct {
			Property string `json:"property"`
			Status   *struct {
				Equals string `json:"equals"`
			} `json:"status"`
		} `json:"filter"`
		StartCursor string `json:"start_cursor"`
		PageSize    int    `json:"page_size"`
	}
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		fail(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if q.Filter.Property != "" && (q.Filter.Property != d.status || q.Filter.Status == nil) {
		fail(w, http.StatusBadRequest, "validation_error", "Could not find property with name or id: "+q.Filter.Property)
		return
	}
	var ids []string
	for _, p := range n.pages {
		if p.database == d.id && (n.ListTrashed || !p.gone()) && (q.Filter.Status == nil || p.Status == q.Filter.Status.Equals) {
			ids = append(ids, p.ID)
		}
	}
	slices.Sort(ids)
	results, more, next := n.window(ids, q.StartCursor, q.PageSize)
	list := []any{}
	for _, id := range results {
		list = append(list, n.pageJSON(n.pages[id]))
	}
	answer(w, map[string]any{"object": "list", "results": list, "has_more": more, "next_cursor": next})
}

// window is one page of a list of ids, from the cursor (an index).
func (n *Notion) window(ids []string, cursor string, size int) ([]string, bool, any) {
	limit := n.PageSize
	if limit == 0 {
		limit = 100
	}
	if size > 0 && size < limit {
		limit = size
	}
	start, _ := strconv.Atoi(cursor)
	start = min(start, len(ids))
	end := min(start+limit, len(ids))
	if end < len(ids) {
		return ids[start:end], true, strconv.Itoa(end)
	}
	return ids[start:end], false, nil
}

func (n *Notion) getPage(w http.ResponseWriter, id string) {
	p := n.pageOf(id)
	if p == nil {
		notFound(w, id)
		return
	}
	answer(w, n.pageJSON(p))
}

func (n *Notion) patchPage(w http.ResponseWriter, r *http.Request, id string) {
	p := n.pageOf(id)
	if p == nil || p.gone() {
		notFound(w, id)
		return
	}
	var patch struct {
		Properties map[string]struct {
			Status *struct {
				Name string `json:"name"`
			} `json:"status"`
		} `json:"properties"`
	}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		fail(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	d := n.databases[p.database]
	for name, v := range patch.Properties {
		if name != d.status || v.Status == nil {
			fail(w, http.StatusBadRequest, "validation_error", name+" is not a property that exists.")
			return
		}
		if !slices.Contains(d.statuses, v.Status.Name) {
			fail(w, http.StatusBadRequest, "validation_error", "Invalid status option. Status option "+
				strconv.Quote(v.Status.Name)+" does not exist.")
			return
		}
		p.Status = v.Status.Name
	}
	p.edited = n.minute()
	p.editedBy = BotID
	answer(w, n.pageJSON(p))
}

func blockJSON(id string, b Block) map[string]any {
	content := map[string]any{"rich_text": []any{text(b.Text)}}
	if b.Type == "to_do" {
		content["checked"] = b.Checked
	}
	if b.Type == "code" {
		content["language"] = "go"
	}
	if b.Type == "table_row" {
		cells := []any{}
		for _, c := range b.Cells {
			cells = append(cells, []any{text(c)})
		}
		content = map[string]any{"cells": cells}
	}
	return map[string]any{"object": "block", "id": id, "type": b.Type, "has_children": len(b.Children) > 0, b.Type: content}
}

func (n *Notion) children(w http.ResponseWriter, r *http.Request, id string) {
	// A block's id is new at each list; blockKids keeps its children.
	var blocks []Block
	if p := n.pageOf(id); p != nil {
		blocks = p.Blocks
	} else if kids, ok := n.blockKids[id]; ok {
		blocks = kids
	} else {
		notFound(w, id)
		return
	}
	ids := make([]string, len(blocks))
	for i := range blocks {
		ids[i] = fmt.Sprintf("%06d", i)
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	results, more, next := n.window(ids, r.URL.Query().Get("start_cursor"), size)
	list := []any{}
	for _, index := range results {
		i, _ := strconv.Atoi(index)
		blockID := NewID()
		n.blockKids[blockID] = blocks[i].Children
		list = append(list, blockJSON(blockID, blocks[i]))
	}
	answer(w, map[string]any{"object": "list", "results": list, "has_more": more, "next_cursor": next})
}

func (n *Notion) comment(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Parent struct {
			PageID string `json:"page_id"`
		} `json:"parent"`
		RichText []struct {
			Type string `json:"type"`
			Text struct {
				Content string `json:"content"`
			} `json:"text"`
		} `json:"rich_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		fail(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	p := n.pageOf(c.Parent.PageID)
	if p == nil || p.gone() {
		notFound(w, c.Parent.PageID)
		return
	}
	if len(c.RichText) == 0 || len(c.RichText) > 100 {
		fail(w, http.StatusBadRequest, "validation_error", "body.rich_text.length should be ≤ 100 and ≥ 1")
		return
	}
	var sb strings.Builder
	for _, part := range c.RichText {
		if part.Type != "text" || len(utf16.Encode([]rune(part.Text.Content))) > 2000 {
			fail(w, http.StatusBadRequest, "validation_error",
				"body.rich_text[].text.content.length should be ≤ 2000")
			return
		}
		sb.WriteString(part.Text.Content)
	}
	comment := Comment{ID: NewID(), Page: p.ID, Text: sb.String(), By: BotID, Parts: len(c.RichText)}
	n.comments = append(n.comments, comment)
	answer(w, map[string]any{"object": "comment", "id": comment.ID,
		"parent": map[string]string{"type": "page_id", "page_id": p.ID}, "discussion_id": NewID(),
		"created_by": map[string]string{"object": "user", "id": BotID}, "rich_text": []any{text(comment.Text)}})
}

func (n *Notion) user(w http.ResponseWriter, token, id string) {
	if n.usersOff[""] || n.usersOff[token] {
		fail(w, http.StatusForbidden, "restricted_resource", "Insufficient permissions for this endpoint.")
		return
	}
	u, ok := n.users[id]
	if !ok {
		notFound(w, id)
		return
	}
	answer(w, map[string]any{"object": "user", "id": u.ID, "name": u.Name, "type": "person", "avatar_url": nil})
}
