package fake

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
	"unicode/utf8"
)

type EvidenceFile struct {
	ID          string
	SessionID   string
	Caption     string
	ContentType string
	Bytes       []byte
}

var evidenceTypes = []struct{ contentType, magic string }{
	{"image/png", "\x89PNG\r\n\x1a\n"},
	{"image/jpeg", "\xff\xd8\xff"},
	{"image/gif", "GIF87a"},
	{"image/gif", "GIF89a"},
	{"image/webp", "RIFF"},
	{"video/webm", "\x1a\x45\xdf\xa3"},
}

func evidenceType(body []byte) (string, bool) {
	if len(body) >= 12 && string(body[4:8]) == "ftyp" {
		return "video/mp4", true
	}
	for _, t := range evidenceTypes {
		if bytes.HasPrefix(body, []byte(t.magic)) {
			return t.contentType, true
		}
	}
	return "", false
}

var sessionIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (f *AgentAPI) addEvidence(w http.ResponseWriter, r *http.Request) {
	org := f.check(w, r)
	if org == nil {
		return
	}
	body, err := io.ReadAll(r.Body)
	f.mu.Lock()
	delay := f.UploadDelay
	f.mu.Unlock()
	time.Sleep(delay)

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.NoEvidence {
		http.NotFound(w, r)
		return
	}
	sessionID, caption := r.PathValue("id"), r.URL.Query().Get("caption")
	if !sessionIDPattern.MatchString(sessionID) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": fmt.Sprintf("%q is not an Agent session id: send the UUID that the harness gives it.", sessionID)})
		return
	}
	limit := cmp.Or(f.EvidenceLimit, BodyLimit)
	if s := f.Sessions[sessionID]; s != nil && s.Organization != org.Slug {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "Agent session " + sessionID + " was uploaded by another person."})
		return
	}
	switch {
	case err != nil:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The body is bad."})
		return
	case utf8.RuneCountInString(caption) > 200:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Send a caption of at most 200 characters, in UTF-8."})
		return
	case len(body) > limit:
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
			"error": fmt.Sprintf("An Evidence file is at most %d MiB; this one has %d bytes.", limit>>20, len(body))})
		return
	case len(body) == 0:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "The body is empty: send the bytes of the file."})
		return
	}
	contentType, known := evidenceType(body)
	if coding := r.Header.Get("Content-Encoding"); !known || (coding != "" && coding != "identity") {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{
			"error": "An Evidence file is a PNG, JPEG, WebP or GIF image, or an MP4 or WebM video, sent as its bytes with no Content-Encoding."})
		return
	}
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(sessionID+"\x00"+string(body))))[:32]
	status := http.StatusCreated
	file := f.evidence[id]
	if file == nil {
		file = &EvidenceFile{ID: id, SessionID: sessionID, Caption: caption, ContentType: contentType, Bytes: body}
		if f.evidence == nil {
			f.evidence = map[string]*EvidenceFile{}
		}
		f.evidence[id] = file
		f.evidenceOrder = append(f.evidenceOrder, id)
	} else {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"id":           file.ID,
		"url":          fmt.Sprintf("%s/%s/evidence/%s", f.ConsoleURL, org.Slug, file.ID),
		"content_type": file.ContentType,
		"byte_size":    len(file.Bytes),
	})
}

// StoredEvidence are copies of the stored Evidence files, in the order of
// their first upload.
func (f *AgentAPI) StoredEvidence() []EvidenceFile {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]EvidenceFile, 0, len(f.evidenceOrder))
	for _, id := range f.evidenceOrder {
		out = append(out, *f.evidence[id])
	}
	return out
}
