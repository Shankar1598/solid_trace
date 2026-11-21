package recorder

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The paths of SolidTrace that the factory's Executions call: Console's
// child tokens (with the Dispatcher's token) and the agent ingest API
// (with a child token).
const (
	ChildTokensPath   = "/api/child_tokens"
	AgentSessionsPath = "/api/agent/v1/agent-sessions/"
)

// ChildToken is one child token that the Dispatcher made.
type ChildToken struct {
	ID              string
	Name            string
	Scopes          []string
	ExpiresAt       time.Time
	AgentSessionIDs []string
	// ProjectID is the token's Project limit, as the Dispatcher sent it
	// ("" is none).
	ProjectID string
	Secret    string
	Revoked   bool
}

// Upload is one upload to the agent ingest API.
type Upload struct {
	Session string
	Path    string
	Version uint32
	Offset  uint64
	// Token is the child token's secret that sent it.
	Token   string
	RunID   string
	Project string
	// Repository and Folder are as the query names them.
	Repository, Folder string
	Bytes              []byte
}

// storedFile is the highest version of one path of an Agent session.
type storedFile struct {
	version uint32
	hash    string
	bytes   []byte
}

// RefuseChildTokens makes each later make of a child token get status (0
// accepts again), as SolidTrace gives when it is down or refuses.
func (r *Recorder) RefuseChildTokens(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refuseChildren = status
}

// RefusePath makes each later upload of path (as "evidence/notes.txt")
// get 400, as SolidTrace gives for a path that it refuses.
func (r *Recorder) RefusePath(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refusePaths == nil {
		r.refusePaths = map[string]bool{}
	}
	r.refusePaths[path] = true
}

// RefuseChildTokensWith makes each later make of a child token with scope
// get status (0 accepts again). Other child tokens are made.
func (r *Recorder) RefuseChildTokensWith(scope string, status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if status == 0 {
		delete(r.refuseScopes, scope)
		return
	}
	r.refuseScopes[scope] = status
}

// ChildTokens are the child tokens that the Dispatcher made, oldest first.
func (r *Recorder) ChildTokens() []ChildToken {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ChildToken, len(r.children))
	for i, c := range r.children {
		out[i] = *c
		out[i].Scopes = slices.Clone(c.Scopes)
		out[i].AgentSessionIDs = slices.Clone(c.AgentSessionIDs)
	}
	return out
}

// Uploads are the uploads that the recorder took, oldest first.
func (r *Recorder) Uploads() []Upload {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.uploads)
}

// Stored is what the recorder has of one path of an Agent session: the
// bytes of its highest version (nil: none).
func (r *Recorder) Stored(session, path string) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if f := r.files[session][path]; f != nil {
		return slices.Clone(f.bytes)
	}
	return nil
}

func answerJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// serveChildTokens answers POST /api/child_tokens and DELETE
// /api/child_tokens/{id} for the Dispatcher's token only, in the shapes
// of Console's ChildTokensController.
func (r *Recorder) serveChildTokens(w http.ResponseWriter, req *http.Request, call Call) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, active := r.answers[r.CallerToken]; call.Authorization != "Bearer "+r.CallerToken || !active {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		answerJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
		return
	}
	if req.Method == http.MethodDelete {
		id := strings.TrimPrefix(req.URL.Path, ChildTokensPath+"/")
		for _, c := range r.children {
			if c.ID == id && !c.Revoked {
				c.Revoked = true
				delete(r.answers, c.Secret)
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		answerJSON(w, http.StatusNotFound, map[string]string{"error": "That token is not a child token of this token."})
		return
	}
	if r.refuseChildren != 0 {
		answerJSON(w, r.refuseChildren, map[string]any{"errors": map[string][]string{"base": {"refused by the test"}}})
		return
	}
	var body struct {
		Name            string   `json:"name"`
		Scopes          []string `json:"scopes"`
		ExpiresAt       string   `json:"expires_at"`
		AgentSessionIDs []string `json:"agent_session_ids"`
		ProjectID       string   `json:"project_id"`
	}
	expires, err := time.Time{}, json.Unmarshal(call.Body, &body)
	if err == nil {
		expires, err = time.Parse(time.RFC3339, body.ExpiresAt)
	}
	if err != nil {
		answerJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": map[string][]string{"base": {err.Error()}}})
		return
	}
	for _, scope := range body.Scopes {
		if status := r.refuseScopes[scope]; status != 0 {
			answerJSON(w, status, map[string]any{"errors": map[string][]string{"scopes": {"refused by the test"}}})
			return
		}
	}
	n := len(r.children) + 1
	child := &ChildToken{ID: strconv.Itoa(n), Name: body.Name, Scopes: body.Scopes, ExpiresAt: expires,
		AgentSessionIDs: body.AgentSessionIDs, ProjectID: body.ProjectID, Secret: fmt.Sprintf("stk_child_%d", n)}
	r.children = append(r.children, child)
	parent := r.answers[r.CallerToken]
	r.answers[child.Secret] = Answer{Sub: parent.Sub, Username: parent.Username, Scope: strings.Join(body.Scopes, " "),
		Exp: expires.Unix(), Organizations: parent.Organizations}
	answerJSON(w, http.StatusCreated, map[string]any{"api_token": map[string]any{"id": n, "name": body.Name,
		"scopes": body.Scopes, "expires_at": body.ExpiresAt}, "secret": child.Secret})
}

// serveAgentSessions answers the agent ingest API's files call, upload
// and download for a live child token that is limited to the Agent session: 401 for any
// other token or a revoked one, 403 for an Agent session out of its
// limits. It keeps the highest version of each path: a higher version
// starts it again, and the same version takes the bytes at its end.
func (r *Recorder) serveAgentSessions(w http.ResponseWriter, req *http.Request, call Call) {
	session, rest, _ := strings.Cut(strings.TrimPrefix(req.URL.Path, AgentSessionsPath), "/")
	token, _ := strings.CutPrefix(call.Authorization, "Bearer ")
	r.mu.Lock()
	defer r.mu.Unlock()
	var child *ChildToken
	for _, c := range r.children {
		// Revoke(secret) also stops a child token, as SolidTrace's own
		// revocation (or the Admin leaving) does.
		if _, active := r.answers[c.Secret]; c.Secret == token && !c.Revoked && active {
			child = c
		}
	}
	switch {
	case child == nil:
		answerJSON(w, http.StatusUnauthorized, map[string]string{"error": "the token is unknown or revoked"})
		return
	case !slices.Contains(child.AgentSessionIDs, session):
		answerJSON(w, http.StatusForbidden, map[string]string{"error": "the token is not for the Agent session " + session})
		return
	case rest == "download" && req.Method == http.MethodGet:
		r.serveDownload(w, child, session)
		return
	case rest != "files":
		answerJSON(w, http.StatusNotFound, map[string]string{"error": "no such path"})
		return
	}
	files := r.files[session]
	if req.Method == http.MethodGet {
		list := []map[string]any{}
		for path, f := range files {
			list = append(list, map[string]any{"path": path, "version": f.version, "end_offset": len(f.bytes),
				"content_hash": f.hash})
		}
		answerJSON(w, http.StatusOK, map[string]any{"files": list})
		return
	}
	q := req.URL.Query()
	body := call.Body
	if req.Header.Get("Content-Encoding") == "gzip" {
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err == nil {
			body, err = io.ReadAll(zr)
		}
		if err != nil {
			answerJSON(w, http.StatusBadRequest, map[string]string{"error": "the body is not gzip"})
			return
		}
	}
	version, _ := strconv.ParseUint(q.Get("version"), 10, 32)
	offset, _ := strconv.ParseUint(q.Get("offset"), 10, 64)
	up := Upload{Session: session, Path: q.Get("path"), Version: uint32(version), Offset: offset, Token: token,
		RunID: q.Get("run_id"), Project: q.Get("project"), Repository: q.Get("repository"), Folder: q.Get("folder"),
		Bytes: body}
	if r.refusePaths[up.Path] {
		answerJSON(w, http.StatusBadRequest, map[string]string{"error": "the path is refused"})
		return
	}
	if up.RunID == "" || up.Project == "" {
		answerJSON(w, http.StatusBadRequest, map[string]string{"error": "A token of a Run sends run_id and project."})
		return
	}
	if files == nil {
		files = map[string]*storedFile{}
		r.files[session] = files
	}
	f := files[up.Path]
	stored := true
	switch {
	case f == nil || up.Version > f.version:
		f = &storedFile{version: up.Version, hash: q.Get("content_hash"), bytes: slices.Clone(body)}
		files[up.Path] = f
	case up.Version == f.version && offset == uint64(len(f.bytes)):
		f.bytes = append(f.bytes, body...)
	case up.Version == f.version && offset+uint64(len(body)) <= uint64(len(f.bytes)):
		stored = false
	default:
		answerJSON(w, http.StatusConflict, map[string]any{"error": "a gap or an overlap", "end_offset": len(f.bytes)})
		return
	}
	r.uploads = append(r.uploads, up)
	answerJSON(w, http.StatusOK, map[string]any{"path": up.Path, "version": f.version, "end_offset": len(f.bytes),
		"stored": stored})
}

// DownloadsNotReady makes the next n downloads get 503 with Retry-After
// 1, as EventStore answers before Agent file processing has the last
// uploads.
func (r *Recorder) DownloadsNotReady(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notReady = n
}

// DownloadsFail makes the next n downloads get 500, as EventStore
// answers when it cannot read the Agent session.
func (r *Recorder) DownloadsFail(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failing = n
}

// Downloads counts the downloads that got an archive.
func (r *Recorder) Downloads() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.downloads
}

// Forget drops what the recorder has of an Agent session, as Retention
// drops it.
func (r *Recorder) Forget(session string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.files, session)
}

// FlagFile makes the download mark the path of the Agent session with
// flags ("partial" or "cut"), as EventStore marks a file whose start
// Retention dropped or that has missing ranges.
func (r *Recorder) FlagFile(session, path, flags string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.flagged == nil {
		r.flagged = map[string]map[string]string{}
	}
	if r.flagged[session] == nil {
		r.flagged[session] = map[string]string{}
	}
	r.flagged[session][path] = flags
}

// serveDownload answers the download of an Agent session as EventStore's
// DownloadAgentSession does: a tar archive of the highest version of each
// path, with the SOLIDTRACE PAX records; 403 with no sessions:read, 404
// for an Agent session with no files. The caller holds mu.
func (r *Recorder) serveDownload(w http.ResponseWriter, child *ChildToken, session string) {
	files := r.files[session]
	switch {
	case !slices.Contains(child.Scopes, "sessions:read"):
		answerJSON(w, http.StatusForbidden, map[string]string{"error": "the token has no sessions:read"})
		return
	case len(files) == 0:
		answerJSON(w, http.StatusNotFound, map[string]string{"error": "The server has no Agent session " + session + "."})
		return
	case r.failing > 0:
		r.failing--
		answerJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read the Agent session"})
		return
	case r.notReady > 0:
		r.notReady--
		w.Header().Set("Retry-After", "1")
		answerJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not yet ready to download"})
		return
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for _, p := range paths {
		f := files[p]
		records := map[string]string{"SOLIDTRACE.version": strconv.FormatUint(uint64(f.version), 10),
			"SOLIDTRACE.start_offset": "0", "SOLIDTRACE.end_offset": strconv.Itoa(len(f.bytes))}
		if flags := r.flagged[session][p]; flags != "" {
			records["SOLIDTRACE.flags"] = flags
		}
		h := &tar.Header{Typeflag: tar.TypeReg, Name: p, Size: int64(len(f.bytes)), Mode: 0o600,
			Format: tar.FormatPAX, PAXRecords: records}
		if tw.WriteHeader(h) != nil {
			answerJSON(w, http.StatusInternalServerError, map[string]string{"error": "tar"})
			return
		}
		_, _ = tw.Write(f.bytes)
	}
	_ = tw.Close()
	r.downloads++
	w.Header().Set("Content-Type", "application/x-tar")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive.Bytes())
}
