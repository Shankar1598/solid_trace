package fake

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/solidtrace/cli/capture"
)

// files answers what the fake has of an Agent session, as the real
// offsets call: the highest version of each path, its end offset and hash.
func (f *AgentAPI) files(w http.ResponseWriter, r *http.Request) {
	request := Request{Method: r.Method, SessionID: r.PathValue("id"), Query: r.URL.Query()}
	defer f.record(&request)
	org := f.check(w, r)
	if org == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	request.Status = http.StatusOK
	s := f.Sessions[request.SessionID]
	if s == nil {
		writeJSON(w, http.StatusOK, map[string]any{"agent_session": nil, "files": []any{}})
		return
	}
	if s.Organization != org.Slug {
		request.Status = http.StatusForbidden
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "Another Organization's Agent session."})
		return
	}
	if f.OlderServer && s.AnotherPerson {
		request.Status = http.StatusForbidden
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "Agent session " + request.SessionID +
			" was uploaded by another person. Only its person, or a token of its Run, adds to it."})
		return
	}
	paths := make([]string, 0, len(s.Files))
	for p := range s.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	list := []any{}
	for _, p := range paths {
		file := s.Files[p]
		list = append(list, map[string]any{"path": p, "version": file.Version, "end_offset": file.End,
			"content_hash": file.ContentHash})
	}
	session := map[string]any{"id": request.SessionID, "harness": s.Harness, "repository": s.Repository,
		"folder": s.Folder, "run_id": s.RunID, "yours": !s.AnotherPerson && s.RunID == ""}
	if f.OlderServer {
		delete(session, "yours")
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_session": session, "files": list})
}

// download answers the Agent session directory as a tar archive, as the
// real download: the highest version of each path, with its PAX records.
func (f *AgentAPI) download(w http.ResponseWriter, r *http.Request) {
	org := f.check(w, r)
	if org == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.Sessions[r.PathValue("id")]
	if s == nil || s.Organization != org.Slug {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "No such Agent session."})
		return
	}
	if f.DownloadNotReady > 0 {
		f.DownloadNotReady--
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "The last uploads are not ready yet."})
		return
	}
	f.Downloads++
	paths := make([]string, 0, len(s.Files))
	for p := range s.Files {
		if p != f.DownloadLeavesOut {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	w.Header().Set("Content-Type", "application/x-tar")
	tw := tar.NewWriter(w)
	for _, p := range paths {
		file := s.Files[p]
		flags := ""
		switch {
		case file.Partial:
			flags = "partial"
		case file.Cut:
			flags = "cut"
		}
		records := map[string]string{"SOLIDTRACE.version": strconv.FormatUint(uint64(file.Version), 10),
			"SOLIDTRACE.start_offset": "0", "SOLIDTRACE.end_offset": strconv.FormatUint(file.End, 10)}
		if flags != "" {
			records["SOLIDTRACE.flags"] = flags
		}
		copies := [][]byte{file.Bytes}
		if p == f.DownloadTwice {
			copies = [][]byte{[]byte("an earlier copy\n"), file.Bytes}
		}
		for _, bytes := range copies {
			h := &tar.Header{Typeflag: tar.TypeReg, Name: p, Mode: 0o600, Size: int64(len(bytes)),
				Format: tar.FormatPAX, PAXRecords: records}
			if tw.WriteHeader(h) != nil {
				return
			}
			if _, err := tw.Write(bytes); err != nil {
				return
			}
		}
	}
	_ = tw.Close()
}

func (f *AgentAPI) record(request *Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Requests = append(f.Requests, *request)
}

// upload follows the rules of the real upload: whole lines for a .jsonl
// file, a whole file with a hash for another, an offset at the stored end,
// a new version from offset 0, a unit over 32 MiB as its first 32 MiB with
// its size, and the captured repositories check.
func (f *AgentAPI) upload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	request := Request{Method: r.Method, SessionID: r.PathValue("id"), Query: q}
	defer f.record(&request)
	fail := func(status int, message string) {
		request.Status = status
		writeJSON(w, status, map[string]any{"error": message})
	}
	org := f.check(w, r)
	if org == nil {
		return
	}
	var reader io.Reader = r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			fail(http.StatusBadRequest, "The gzip body is bad.")
			return
		}
		reader = zr
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		fail(http.StatusBadRequest, "The body is bad.")
		return
	}
	request.Body = body

	f.mu.Lock()
	delay := f.UploadDelay
	f.mu.Unlock()
	time.Sleep(delay)

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.UploadsDown {
		fail(http.StatusInternalServerError, "The unit could not be stored. Try again later.")
		return
	}
	path, hash, repository := q.Get("path"), q.Get("content_hash"), q.Get("repository")
	if status := f.Refuse[path]; status != 0 {
		fail(status, "This fake refuses "+path+".")
		return
	}
	version, errVersion := strconv.ParseUint(q.Get("version"), 10, 32)
	offset, errOffset := strconv.ParseUint(q.Get("offset"), 10, 64)
	size := uint64(len(body))
	if v := q.Get("size"); v != "" {
		size, _ = strconv.ParseUint(v, 10, 64)
	}
	jsonl := strings.HasSuffix(path, ".jsonl")
	switch {
	case errVersion != nil || errOffset != nil || version == 0 || path == "" || strings.HasPrefix(path, "/") ||
		strings.Contains("/"+path+"/", "/../") || q.Get("harness") == "" || len(strings.Split(repository, "/")) < 3:
		fail(http.StatusBadRequest, "The query is bad.")
		return
	case q.Get("run_id") != "" || q.Get("project") != "":
		// The fake's tokens are a person's sign-in, not a Run's.
		fail(http.StatusBadRequest, "Only a token of a Run sends run_id and project.")
		return
	case len(body) == 0:
		fail(http.StatusBadRequest, "The body is empty: send the bytes of the unit.")
		return
	case len(body) > BodyLimit || size < uint64(len(body)) || (size > uint64(len(body)) && len(body) != BodyLimit):
		fail(http.StatusBadRequest, "The size does not fit the body.")
		return
	case jsonl && size == uint64(len(body)) && body[len(body)-1] != '\n':
		fail(http.StatusBadRequest, "A unit of a .jsonl file is whole lines: send the bytes up to the last newline.")
		return
	case !jsonl && (offset != 0 || hash == ""):
		fail(http.StatusBadRequest, "A file that is not .jsonl is uploaded whole: send it at offset 0 with its content_hash.")
		return
	case !captured(org.Repositories, repository):
		fail(http.StatusForbidden, "The repository "+repository+" is not in the captured repositories of "+org.Name+".")
		return
	}

	if f.Sessions == nil {
		f.Sessions = map[string]*Session{}
	}
	s := f.Sessions[request.SessionID]
	if s == nil {
		s = &Session{Harness: q.Get("harness"), Repository: repository, Folder: q.Get("folder"),
			Organization: org.Slug, Files: map[string]*File{}}
		f.Sessions[request.SessionID] = s
	}
	file := s.Files[path]
	answer := func(stored bool) {
		request.Status = http.StatusOK
		writeJSON(w, http.StatusOK, map[string]any{"path": path, "version": file.Version, "end_offset": file.End,
			"stored": stored})
	}
	conflict := func(reason string) {
		request.Status = http.StatusConflict
		writeJSON(w, http.StatusConflict, map[string]any{"error": reason, "path": path, "version": file.Version,
			"end_offset": file.End})
	}
	switch {
	case file == nil || uint32(version) > file.Version:
		if offset != 0 {
			if file == nil {
				file = &File{}
			}
			conflict("A new version of a path starts at offset 0.")
			return
		}
		versions := 1
		if file != nil {
			versions = file.Versions + 1
		}
		file = &File{Version: uint32(version), Versions: versions}
		s.Files[path] = file
	case uint32(version) < file.Version:
		conflict("A newer version of the path is stored.")
		return
	case !jsonl && hash == file.ContentHash:
		answer(false)
		return
	case !jsonl:
		conflict("Another hash is stored for this version.")
		return
	case offset+size <= file.End:
		answer(false)
		return
	case offset != file.End:
		conflict("The unit leaves a gap or overlaps the stored end.")
		return
	}
	file.Bytes = append(file.Bytes, body...)
	file.End = offset + size
	file.ContentHash = hash
	file.Cut = file.Cut || size > uint64(len(body))
	answer(true)
}

// captured is the server's match of a repository with the list.
func captured(entries []string, repository string) bool {
	return slices.ContainsFunc(entries, func(entry string) bool { return capture.Matches(entry, repository) })
}
