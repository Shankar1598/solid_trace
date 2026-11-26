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

var evidenceTypes = []struct {
	contentType string
	matches     func([]byte) bool
}{
	{"image/png", prefix("\x89PNG\r\n\x1a\n")},
	{"image/jpeg", prefix("\xff\xd8\xff")},
	{"image/gif", func(b []byte) bool { return prefix("GIF87a")(b) || prefix("GIF89a")(b) }},
	{"image/webp", func(b []byte) bool { return len(b) >= 12 && prefix("RIFF")(b) && string(b[8:12]) == "WEBP" }},
	{"video/mp4", isMP4},
	{"video/webm", isWebM},
}

func prefix(magic string) func([]byte) bool {
	return func(b []byte) bool { return bytes.HasPrefix(b, []byte(magic)) }
}

var mp4Brands = map[string]bool{
	"isom": true, "iso2": true, "iso4": true, "iso5": true, "iso6": true,
	"mp41": true, "mp42": true, "avc1": true, "dash": true, "M4V ": true,
}

func isMP4(b []byte) bool {
	if len(b) < 12 || string(b[4:8]) != "ftyp" {
		return false
	}
	size := int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	if size < 12 || size > len(b) {
		return false
	}
	if mp4Brands[string(b[8:12])] {
		return true
	}
	compatibleBrands := b[min(16, size):size]
	for i := 0; i+4 <= len(compatibleBrands); i += 4 {
		if mp4Brands[string(compatibleBrands[i:i+4])] {
			return true
		}
	}
	return false
}

func isWebM(b []byte) bool {
	head := b[:min(len(b), 64)]
	docType := bytes.Index(head, []byte("\x42\x82"))
	return prefix("\x1a\x45\xdf\xa3")(head) && docType > 0 &&
		bytes.Contains(head[docType:min(len(head), docType+12)], []byte("webm"))
}

func evidenceType(body []byte) (string, bool) {
	for _, t := range evidenceTypes {
		if t.matches(body) {
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
	id := evidenceID(sessionID, body)
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

func evidenceID(sessionID string, body []byte) string {
	sum := sha256.Sum256([]byte(sessionID + "\x00" + string(body)))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
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
