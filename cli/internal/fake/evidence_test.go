package fake_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"testing"

	"github.com/solidtrace/cli/internal/fake"
)

func postEvidence(t *testing.T, api *fake.AgentAPI, token string, body []byte) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, api.URL()+"/api/agent/v1/agent-sessions/0b6f3c1e-8d2a-4c5b-9e7f-1a2b3c4d5e6f/evidence", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var answer map[string]any
	if err := json.Unmarshal(data, &answer); err != nil {
		t.Fatalf("%s: %v", data, err)
	}
	return resp.StatusCode, answer
}

func newEvidenceAPI(t *testing.T) (*fake.AgentAPI, string) {
	t.Helper()
	oauth := fake.NewOAuth(fake.Organization{Name: "Acme", Slug: "acme"})
	t.Cleanup(oauth.Close)
	api := fake.NewAgentAPI(oauth)
	t.Cleanup(api.Close)
	return api, oauth.AccessToken("acme")
}

func TestTheFakeRefusesTheFilesThatHaveTheFirstBytesOfAnEvidenceTypeButAreNotOne(t *testing.T) {
	api, token := newEvidenceAPI(t)
	for name, body := range map[string][]byte{
		"WAV audio, RIFF with no WEBP tag":          append([]byte("RIFF\x24\x00\x00\x00WAVEfmt "), make([]byte, 32)...),
		"AVI video, RIFF with no WEBP tag":          append([]byte("RIFF\x24\x00\x00\x00AVI LIST"), make([]byte, 32)...),
		"QuickTime video, ftyp with no MP4 brand":   append([]byte("\x00\x00\x00\x14ftypqt  \x00\x00\x02\x00qt  "), make([]byte, 32)...),
		"HEIC image, ftyp with no MP4 brand":        append([]byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic"), make([]byte, 32)...),
		"Matroska video, EBML with no webm DocType": append([]byte("\x1a\x45\xdf\xa3\x93\x42\x82\x88matroska\x42\x87\x81\x04"), make([]byte, 32)...),
	} {
		if status, answer := postEvidence(t, api, token, body); status != http.StatusUnsupportedMediaType {
			t.Errorf("%s: %d with the type %v, want 415", name, status, answer["content_type"])
		}
	}
}

var consoleEvidenceRoute = regexp.MustCompile(`^https://console\.example\.com/acme/evidence/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestTheFakeLinksEachEvidenceFileToAConsolePageOfAUUID(t *testing.T) {
	api, token := newEvidenceAPI(t)
	api.ConsoleURL = "https://console.example.com"

	status, first := postEvidence(t, api, token, []byte("\x89PNG\r\n\x1a\nxxxx"))
	if link, _ := first["url"].(string); status != http.StatusCreated || !consoleEvidenceRoute.MatchString(link) {
		t.Errorf("%d with the link %q, want 201 and <console>/acme/evidence/<uuid>", status, link)
	}
	if status, again := postEvidence(t, api, token, []byte("\x89PNG\r\n\x1a\nxxxx")); status != http.StatusOK || again["url"] != first["url"] {
		t.Errorf("the same file again: %d with the link %v, want 200 and %v", status, again["url"], first["url"])
	}
}
