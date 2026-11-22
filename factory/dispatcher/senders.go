package dispatcher

import (
	"bytes"
	"cmp"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The generic signed webhook (P5): POST /webhooks/generic/<sender id>.
// A sender is a tool that an Admin gave a secret. Its delivery is the
// truth, because the tool has no API that the Dispatcher can read (the
// one exception to W0): a verified delivery starts the Run at once.

// genericKind is the <kind> of a sender's webhook URL. No connection has
// it.
const genericKind = "generic"

// The limits of a sender's delivery.
const (
	// maxPrompt is the longest prompt of a delivery, in bytes.
	maxPrompt = 64 << 10
	// deliveryWindow is the longest time between a delivery's timestamp
	// and the Dispatcher's clock, before or after.
	deliveryWindow = 5 * time.Minute
	maxLink        = 2048
	maxDedupeKey   = 256
	maxAttributes  = 20
	maxAttribute   = 1024
	// maxSenderName is the most runes of a sender's name.
	maxSenderName = 100
)

// attributeName is the form of the name of a Run's attribute.
var attributeName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func (d *Dispatcher) senderURL(id string) string {
	if d.config.WebhookURL == "" {
		return ""
	}
	return d.config.WebhookURL + webhookPath(genericKind, id)
}

func (d *Dispatcher) listSenders(w http.ResponseWriter, r *http.Request, p person) {
	list, err := d.store.senders(p.organization.id)
	if err != nil {
		d.serverError(w, "list the senders", err)
		return
	}
	senders := dispatcherapi.Senders{Senders: []dispatcherapi.Sender{}}
	for _, s := range list {
		s.WebhookURL = d.senderURL(s.ID)
		senders.Senders = append(senders.Senders, s.Sender)
	}
	writeJSON(w, http.StatusOK, senders)
}

// createSender makes a sender with a new secret, which only this answer
// gives.
func (d *Dispatcher) createSender(w http.ResponseWriter, r *http.Request, p person) {
	var save dispatcherapi.SaveSender
	if !decode(w, r, &save, "the body must be a JSON object with name and projects") {
		return
	}
	name := strings.TrimSpace(save.Name)
	if name == "" || utf8.RuneCountInString(name) > maxSenderName || strings.ContainsFunc(name, unicode.IsControl) {
		writeProblem(w, http.StatusBadRequest, "a sender needs a name of 1 to 100 characters on one line")
		return
	}
	sender := storedSender{
		Sender: dispatcherapi.Sender{ID: newRunID(), Name: name, Projects: []string{},
			CreatedAt: d.now().UTC().Truncate(time.Microsecond), CreatedBy: dispatcherapi.Person{ID: p.id, Name: p.name}},
		organization: p.organization,
		factory:      save.Factory,
	}
	if len(save.Projects) > 0 {
		projects, err := d.listProjects(r.Context(), p.organization)
		if err != nil {
			d.solidTraceFailure(w, p.organization, err)
			return
		}
		sender.projects = []senderProject{}
		for _, slug := range save.Projects {
			slug = strings.TrimSpace(slug)
			i := slices.IndexFunc(projects, func(sp solidTraceProject) bool { return sp.slug == slug })
			if i < 0 {
				writeProblem(w, http.StatusUnprocessableEntity, noProject(slug, p.organization.slug))
				return
			}
			if !slices.ContainsFunc(sender.projects, func(sp senderProject) bool { return sp.ID == projects[i].id }) {
				sender.projects = append(sender.projects, senderProject{ID: projects[i].id, Slug: slug})
				sender.Projects = append(sender.Projects, slug)
			}
		}
	}
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	secret := hex.EncodeToString(key)
	if err := d.store.insertSender(sender, secret); err != nil {
		d.serverError(w, "make the sender", err)
		return
	}
	sender.WebhookURL = d.senderURL(sender.ID)
	writeJSON(w, http.StatusCreated, dispatcherapi.NewSender{Sender: sender.Sender, Secret: secret})
}

func (d *Dispatcher) deleteSender(w http.ResponseWriter, r *http.Request, p person) {
	err := d.store.deleteSender(p.organization.id, r.PathValue("id"))
	if errors.Is(err, errNotFound) {
		writeProblem(w, http.StatusNotFound, "no sender "+r.PathValue("id")+" in this Organization")
		return
	}
	if err != nil {
		d.serverError(w, "delete the sender", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// readWebhookBody reads a webhook's raw body, at most MaxWebhookBody. It
// answers the request itself when it cannot.
func readWebhookBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.ContentLength > MaxWebhookBody {
		writeProblem(w, http.StatusRequestEntityTooLarge, "a webhook body is at most 1 MiB")
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxWebhookBody))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeProblem(w, http.StatusRequestEntityTooLarge, "a webhook body is at most 1 MiB")
		return nil, false
	}
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "the Dispatcher could not read the body")
		return nil, false
	}
	return body, true
}

// genericSignature is the hex HMAC-SHA256 of a delivery with the sender's
// secret: of the timestamp, a "." and the raw body. It is false when the
// delivery's signature is not that one.
func genericSignature(secret []byte, header http.Header, body []byte) (string, bool) {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(header.Get(dispatcherapi.HeaderTimestamp) + "."))
	mac.Write(body)
	want := mac.Sum(nil)
	given, ok := strings.CutPrefix(header.Get(dispatcherapi.HeaderSignature), "sha256=")
	signature, err := hex.DecodeString(given)
	return hex.EncodeToString(want), ok && err == nil && len(secret) > 0 && hmac.Equal(want, signature)
}

// genericWebhook serves a sender's delivery. The order: 404 for no such
// sender, 413, 401 (signature, then the timestamp window), 429 (the
// rate limit of verified webhooks), 400 (the body), the Run of a
// repeated delivery, 403 or 422 (the Project), and in one transaction the
// dedupe_key, the sender's limits (429) and the start (202).
func (d *Dispatcher) genericWebhook(w http.ResponseWriter, r *http.Request, id string) {
	sender, secret, err := d.store.sender(id)
	if errors.Is(err, errNotFound) {
		writeProblem(w, http.StatusNotFound, "no such path")
		return
	}
	if err != nil {
		d.serverError(w, "read the sender", err)
		return
	}
	body, ok := readWebhookBody(w, r)
	if !ok {
		return
	}
	signature, ok := genericSignature(secret, r.Header, body)
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "the webhook has no valid signature: "+dispatcherapi.HeaderSignature+
			" must be sha256= and the hex HMAC-SHA256 of "+dispatcherapi.HeaderTimestamp+", a '.' and the raw body")
		return
	}
	now := d.now().UTC()
	seconds, err := strconv.ParseInt(r.Header.Get(dispatcherapi.HeaderTimestamp), 10, 64)
	if err != nil || now.Sub(time.Unix(seconds, 0)).Abs() > deliveryWindow {
		writeProblem(w, http.StatusUnauthorized, dispatcherapi.HeaderTimestamp+
			" must be the Unix time in seconds, at most 5 minutes from the Dispatcher's clock")
		return
	}
	if wait, ok := d.webhookLimits.take(sender.ID, now); !ok {
		w.Header().Set("Retry-After", retryAfter(wait))
		writeProblem(w, http.StatusTooManyRequests, "this sender sent more than "+strconv.Itoa(WebhookRateLimit)+
			" webhooks in a minute")
		return
	}
	if err := d.store.senderDelivered(sender.ID, now); err != nil {
		d.serverError(w, "keep the delivery", err)
		return
	}
	delivery := r.Header.Get(dispatcherapi.HeaderDelivery)
	if delivery == "" || len(delivery) > maxDeliveryID {
		writeProblem(w, http.StatusBadRequest, dispatcherapi.HeaderDelivery+" must be a delivery id of 1 to "+
			strconv.Itoa(maxDeliveryID)+" bytes")
		return
	}
	var start dispatcherapi.GenericWebhook
	if problem := readGeneric(body, &start); problem != "" {
		writeProblem(w, http.StatusBadRequest, problem)
		return
	}
	if name, ok := factoryAttribute(start.Attributes); ok && !sender.factory {
		writeProblem(w, http.StatusBadRequest, "the attribute "+name+" is for the sender of a 'Factory' Integration "+
			"only: attribute names that start with "+factoryAttributePrefix+" start an Issue or a Monitor Run")
		return
	}
	served, err := d.tokens.organization(r.Context())
	if err != nil {
		d.ownOrganizationFailure(w, err)
		return
	}
	if served.id != sender.organization.id {
		writeProblem(w, http.StatusForbidden, "this sender belongs to an Organization that this Dispatcher no longer serves")
		return
	}
	sender.organization = served
	runID, err := deliveryRun(d.store.db, sender.ID, delivery, signature, now)
	if err != nil {
		d.serverError(w, "read the delivery", err)
		return
	}
	if runID != "" {
		d.writeGeneric(w, served, senderStart{runID: runID})
		return
	}
	starter := sender.starter()
	settings, ok := d.triggerSettings(w, r, starter, start.Project)
	if !ok {
		return
	}
	run := storedRun{
		Run: dispatcherapi.Run{
			ID:           newRunID(),
			State:        dispatcherapi.StateQueued,
			Project:      settings.Project,
			Prompt:       start.Prompt,
			Title:        start.Title,
			Link:         start.Link,
			Attributes:   start.Attributes,
			Organization: served.slug,
			StartedBy:    dispatcherapi.Person{Kind: actorSender, ID: sender.ID, Name: sender.Name},
			CreatedAt:    now.Truncate(time.Microsecond),
		},
		organizationID: served.id,
		projectID:      settings.projectID,
		repository:     settings.Repository,
		folder:         settings.Folder,
		baseBranch:     cmp.Or(start.Branch, settings.BaseBranch),
		trigger:        triggerWebhook,
		dedupeKey:      start.DedupeKey,
	}
	starts, active := d.config.senderLimits()
	result, err := d.store.startForSender(run, delivery, signature, now, starts, active)
	if err != nil {
		d.serverError(w, "start a Run", err)
		return
	}
	if result.limit != "" {
		w.Header().Set("Retry-After", retryAfter(result.wait))
		writeProblem(w, http.StatusTooManyRequests, result.limit+": the Run is refused")
		return
	}
	if result.started {
		d.wakeDispatch()
	}
	d.writeGeneric(w, served, result)
}

func (d *Dispatcher) writeGeneric(w http.ResponseWriter, org organization, result senderStart) {
	writeJSON(w, http.StatusAccepted, dispatcherapi.GenericWebhookAnswer{RunID: result.runID,
		URL: d.config.SolidTraceURL + "/" + org.slug + "/runs/" + result.runID, Started: result.started})
}

// retryAfter is wait in whole seconds, rounded up, at least 1.
func retryAfter(wait time.Duration) string {
	return strconv.Itoa(max(int(math.Ceil(wait.Seconds())), 1))
}

// ownOrganizationFailure answers a request when the Dispatcher could not
// find the Organization of its own token.
func (d *Dispatcher) ownOrganizationFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errCallerRefused):
		d.log.Error("SolidTrace refuses the Dispatcher's own token")
		writeProblem(w, http.StatusServiceUnavailable, callerRefused)
	case errors.Is(err, errOwnOrganization):
		d.log.Error(err.Error())
		writeProblem(w, http.StatusServiceUnavailable, err.Error())
	default:
		d.log.Warn("could not check the Dispatcher's own token", "error", err)
		writeProblem(w, http.StatusBadGateway, "the Dispatcher could not check its own token at SolidTrace")
	}
}

// factoryAttributePrefix starts the names of the attributes that only the
// sender of a 'Factory' Integration sends (issue_runs.go). With them, a
// Run gets an mcp:read token for its Project and comments on an Issue.
const factoryAttributePrefix = "solidtrace."

// factoryAttribute gives the first name, in order, of the attributes that
// start with factoryAttributePrefix.
func factoryAttribute(attributes map[string]string) (string, bool) {
	names := slices.Sorted(maps.Keys(attributes))
	i := slices.IndexFunc(names, func(name string) bool { return strings.HasPrefix(name, factoryAttributePrefix) })
	if i < 0 {
		return "", false
	}
	return names[i], true
}

// readGeneric reads and checks a delivery's body into start, in its
// stored form, or says what is wrong.
func readGeneric(body []byte, start *dispatcherapi.GenericWebhook) string {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(start); err != nil {
		return "the body must be a JSON object with project, prompt, and optional branch, title, link, " +
			"dedupe_key and attributes (a map of strings): " + err.Error()
	}
	start.Project = strings.TrimSpace(start.Project)
	if start.Project == "" || strings.TrimSpace(start.Prompt) == "" {
		return "a delivery needs a project and a prompt"
	}
	if len(start.Prompt) > maxPrompt {
		return "the prompt is " + strconv.Itoa(len(start.Prompt)) + " bytes; it is at most 64 KiB (65536 bytes)"
	}
	start.Branch = strings.TrimSpace(start.Branch)
	if start.Branch != "" && !validBranch(start.Branch) {
		return "the branch must be a branch name, such as main or release/2.0, not " + quote(start.Branch)
	}
	if start.Title != "" {
		start.Title = title(start.Title)
	}
	if start.Link != "" {
		u, err := url.Parse(start.Link)
		if len(start.Link) > maxLink || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return "the link must be an http:// or https:// URL of at most 2048 bytes"
		}
	}
	if len(start.DedupeKey) > maxDedupeKey {
		return "the dedupe_key is at most " + strconv.Itoa(maxDedupeKey) + " bytes"
	}
	if len(start.Attributes) > maxAttributes {
		return "a delivery has at most " + strconv.Itoa(maxAttributes) + " attributes"
	}
	for name, value := range start.Attributes {
		if !attributeName.MatchString(name) {
			return "an attribute name is 1 to 64 letters, digits, '_', '.' or '-', not " + quote(name)
		}
		if len(value) > maxAttribute {
			return "the attribute " + name + " is more than " + strconv.Itoa(maxAttribute) + " bytes"
		}
	}
	return ""
}
