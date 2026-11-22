package dispatcher

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/forge"
)

// Bitbucket Cloud's webhooks (W5). A forge connection is a webhook
// source: its webhook URL goes into the repository's webhook, with the
// "Pull request: Merged" and "Declined" events and the connection's
// webhook secret. pullrequest:fulfilled and pullrequest:rejected queue a
// reconcile of the PR, which reads the PR's state from Bitbucket, records
// the outcome, and closes the Run. Each other event, such as a push to a
// work-in-progress ref, does nothing. With the "Comment created" event,
// pullrequest:comment_created of a comment that starts with the trigger
// word queues a reconcile of the PR's comments (pr_comments.go). Bitbucket's webhooks have no time,
// so the signature is the only check (W2), and the request UUID is the
// delivery id. The close loop's PR check every 5 minutes is the backstop.

// The headers of Bitbucket's webhooks.
const (
	bitbucketSignatureHeader = "X-Hub-Signature"
	bitbucketEventHeader     = "X-Event-Key"
	bitbucketDeliveryHeader  = "X-Request-UUID"
)

// The events that give a PR outcome.
const (
	bitbucketPRFulfilled = "pullrequest:fulfilled"
	bitbucketPRRejected  = "pullrequest:rejected"
	// bitbucketPRComment is a new comment on a PR, general or inline.
	bitbucketPRComment = "pullrequest:comment_created"
)

// bitbucketWebhooks is the WebhookKind of the Bitbucket forge
// connections.
type bitbucketWebhooks struct{ d *Dispatcher }

func (bitbucketWebhooks) Kind() string { return dispatcherapi.ForgeBitbucket }

// Signature is X-Hub-Signature, sha256=<hex>, on the raw body.
func (bitbucketWebhooks) Signature(header http.Header, body []byte) ([]byte, []byte, error) {
	value, ok := strings.CutPrefix(header.Get(bitbucketSignatureHeader), "sha256=")
	if !ok {
		return nil, nil, errors.New("the webhook has no " + bitbucketSignatureHeader + " of the form sha256=<hex>")
	}
	signature, err := hex.DecodeString(value)
	return body, signature, err
}

// Delivery gives the PR of a PR outcome event as the entity
// "<workspace>/<slug>#<id>", and the PR of a comment that starts with the
// trigger word as the entity "comments:<workspace>/<slug>#<id>". Each
// other event, and a comment with no trigger word, is ignored.
func (k bitbucketWebhooks) Delivery(header http.Header, body []byte, _ time.Time) (Delivery, error) {
	delivery := Delivery{ID: header.Get(bitbucketDeliveryHeader)}
	event := header.Get(bitbucketEventHeader)
	switch event {
	case bitbucketPRFulfilled, bitbucketPRRejected, bitbucketPRComment:
	default:
		delivery.Ignored = true
		return delivery, nil
	}
	var payload struct {
		PullRequest struct {
			ID int `json:"id"`
		} `json:"pullrequest"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Comment struct {
			Content struct {
				Raw string `json:"raw"`
			} `json:"content"`
		} `json:"comment"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Delivery{}, err
	}
	workspace, slug, ok := strings.Cut(payload.Repository.FullName, "/")
	if payload.PullRequest.ID <= 0 || !ok || workspace == "" || slug == "" || strings.Contains(slug, "/") {
		return Delivery{}, errors.New("the event has no pullrequest.id and repository.full_name")
	}
	delivery.Entity = prEntity(payload.Repository.FullName, payload.PullRequest.ID)
	if event == bitbucketPRComment {
		// The body only tells if the comment can be a trigger: the
		// reconcile reads the comments from Bitbucket.
		if !triggers(payload.Comment.Content.Raw, k.d.config.triggerWord()) {
			return Delivery{ID: delivery.ID, Ignored: true}, nil
		}
		delivery.Entity = commentsEntityPrefix + delivery.Entity
	}
	return delivery, nil
}

// prEntity is the entity of the PR with id in the repository
// <workspace>/<slug>.
func prEntity(fullName string, id int) string {
	return strings.ToLower(fullName) + "#" + strconv.Itoa(id)
}

// splitPREntity gives the <workspace>/<slug> and the id of a PR entity.
func splitPREntity(entity string) (string, int, error) {
	fullName, number, ok := strings.Cut(entity, "#")
	id, err := strconv.Atoi(number)
	if !ok || err != nil || id <= 0 {
		return "", 0, fmt.Errorf("the entity %q is not a PR", entity)
	}
	return fullName, id, nil
}

// Backstop: the whole connection's step runs again each PR's comments
// step that failed (the close loop checks the PRs every prCheckEvery),
// so its interval is that check's.
func (bitbucketWebhooks) Backstop(WebhookSource) time.Duration { return prCheckEvery }

// Reconcile reads the state of the PR from Bitbucket for each Run of the
// connection's Organization whose PR it is and that has no PR outcome. It
// records a merged or declined outcome, and closes the Run, unless it has
// an Execution queued or live (the close loop closes it after its end).
// The webhook's body is only a hint: the outcome is the state that
// Bitbucket gives, with the Run's own connection.
func (k bitbucketWebhooks) Reconcile(ctx context.Context, source WebhookSource, entity string) ([]string, error) {
	if entity == "" {
		k.retryComments(ctx, source)
		return nil, nil
	}
	if pr, ok := strings.CutPrefix(entity, commentsEntityPrefix); ok {
		return k.commentsStep(ctx, source, pr)
	}
	fullName, id, err := splitPREntity(entity)
	if err != nil {
		return nil, err
	}
	d := k.d
	runs, err := d.store.closingRuns(`organization_id = ? AND closed_at IS NULL AND pr_id = ? AND pr_outcome = ''`,
		source.organizationID, id)
	if err != nil {
		return nil, err
	}
	var changed []string
	var errs []error
	for _, r := range runs {
		f, repository, err := d.runForge(r)
		if err != nil {
			errs = append(errs, fmt.Errorf("the Run %s: %w", r.id, err))
			continue
		}
		if !strings.EqualFold(repository, forge.BitbucketHost+"/"+fullName) {
			continue
		}
		outcome, err := prOutcomeOn(ctx, f, repository, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("the Run %s: %w", r.id, err))
			continue
		}
		if outcome == "" {
			continue
		}
		now := d.now()
		if err := d.store.savePROutcome(r.id, outcome, now); err != nil {
			errs = append(errs, fmt.Errorf("the Run %s: %w", r.id, err))
			continue
		}
		d.log.Info("a Bitbucket webhook gave the outcome of a Run's PR", "run", r.id, "outcome", outcome)
		reason := closePRDeclined
		if outcome == dispatcherapi.PRMerged {
			reason = closePRMerged
		}
		d.close(ctx, r, reason, now)
		changed = append(changed, entity)
	}
	return changed, errors.Join(errs...)
}
