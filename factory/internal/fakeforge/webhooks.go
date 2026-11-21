package fakeforge

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// Bitbucket Cloud's webhooks. Bitbucket signs the raw body with the
// webhook's secret (HMAC-SHA256, X-Hub-Signature: sha256=<hex>), names
// the event in X-Event-Key and the delivery in X-Request-UUID.

// The webhook events of Bitbucket Cloud that the tests send.
const (
	EventPRFulfilled = "pullrequest:fulfilled"
	EventPRRejected  = "pullrequest:rejected"
	EventPRCreated   = "pullrequest:created"
	EventPRComment   = "pullrequest:comment_created"
	EventPush        = "repo:push"
)

// Sign is Bitbucket's X-Hub-Signature of body with secret.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Delivery is the headers of a Bitbucket webhook of the event, with the
// request UUID, signed with secret.
func Delivery(secret, requestUUID, event string, body []byte) http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("User-Agent", "Bitbucket-Webhooks/2.0")
	h.Set("X-Event-Key", event)
	h.Set("X-Request-UUID", requestUUID)
	h.Set("X-Hook-UUID", "{00000000-0000-4000-8000-000000000077}")
	h.Set("X-Attempt-Number", "1")
	h.Set("X-Hub-Signature", Sign(secret, body))
	return h
}

// repositoryJSON is Bitbucket's repository object in a webhook.
func repositoryJSON(repo string) map[string]any {
	_, slug, _ := strings.Cut(repo, "/")
	return map[string]any{"type": "repository", "full_name": repo, "name": slug,
		"uuid": "{" + repo + "}", "is_private": true}
}

// PRWebhook is the body of a pullrequest webhook of the PR as the forge
// has it now.
func (f *Forge) PRWebhook(repo string, id int) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.findPR(repo, id)
	if p == nil {
		f.t.Fatalf("fakeforge: no PR %d in %s", id, repo)
	}
	body, _ := json.Marshal(map[string]any{"actor": account("Rita"), "pullrequest": f.prJSON(repo, p),
		"repository": repositoryJSON(repo)})
	return body
}

// CommentWebhook is the body of the pullrequest:comment_created webhook
// of a comment on the PR, as the forge has them now.
func (f *Forge) CommentWebhook(repo string, pr, comment int) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, c := f.findPR(repo, pr), f.findComment(repo, pr, comment)
	if p == nil || c == nil {
		f.t.Fatalf("fakeforge: no comment %d on PR %d in %s", comment, pr, repo)
	}
	repository := repositoryJSON(repo)
	repository["is_private"] = !f.public[repo]
	body, _ := json.Marshal(map[string]any{"actor": commentJSON(pr, c)["user"], "comment": commentJSON(pr, c),
		"pullrequest": f.prJSON(repo, p), "repository": repository})
	return body
}

// PushWebhook is the body of the repo:push webhook of a push of commit to
// the full ref name in repo.
func PushWebhook(repo, ref, commit string) []byte {
	kind, name := "branch", strings.TrimPrefix(ref, "refs/heads/")
	if name == ref {
		kind = "ref"
	}
	body, _ := json.Marshal(map[string]any{"actor": account("factory"), "repository": repositoryJSON(repo),
		"push": map[string]any{"changes": []map[string]any{{
			"new":     map[string]any{"type": kind, "name": name, "target": map[string]any{"type": "commit", "hash": commit}},
			"old":     nil,
			"created": true, "forced": false, "closed": false,
		}}}})
	return body
}

// APICalls is the number of REST API calls with the forge's token to a
// repository that it has.
func (f *Forge) APICalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.apiCalls
}
