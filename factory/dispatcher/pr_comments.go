package dispatcher

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/forge"
)

// Comments on a Run's PR continue the Run (P1 to P3, P4). A comment that
// starts with the trigger word queues a reconcile of the PR's comments.
// The reconcile reads the truth from the forge with the Run's own
// connection, and acts only when:
//   - the repository is private (a comment on a public repository never
//     continues a Run);
//   - the trigger comment is not resolved or deleted, and is by a
//     person: not by a bot, an app or an access token, and not by the
//     account that opened the PR, which is the factory's own identity.
//
// Each person who can comment on the private repository can continue the
// Run: the message's actor is the Bitbucket account. The message has the
// trigger comment first, then each other person's comment that is not
// resolved, with its author, file, line and thread. The Run keeps the
// edit of each comment that it got: a comment goes to the Run one time,
// and again after an edit. On a Closed Run, the factory answers the
// trigger comment one time: at most one answer on the PR in a day.

// commentsEntityPrefix is the start of the entity of a PR's comments.
const commentsEntityPrefix = "comments:"

// actorForge is the actor kind of a forge account (actor.id is its
// account id, actor.name its name).
const actorForge = "forge"

// closedAnswerEvery is the shortest time between two answer comments on
// the PR of a Closed Run.
const closedAnswerEvery = 24 * time.Hour

// prComments reconciles the comments of the PR entity "<workspace>/<slug>#<id>".
func (k bitbucketWebhooks) prComments(ctx context.Context, source WebhookSource, entity string) ([]string, error) {
	d := k.d
	fullName, id, err := splitPREntity(entity)
	if err != nil {
		return nil, err
	}
	run, f, repository, err := d.prRun(source.organizationID, fullName, id)
	if err != nil || run.id == "" {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, forgeCallBudget)
	defer cancel()
	log := []any{"run", run.id, "repository", repository, "pr", id}
	private, err := f.Private(ctx, repository)
	if err != nil {
		return nil, fmt.Errorf("the Run %s: %w", run.id, err)
	}
	if !private {
		d.log.Info("a comment on a PR of a public repository never continues a Run", log...)
		return nil, nil
	}
	pr, err := f.PR(ctx, repository, id)
	if err != nil {
		return nil, fmt.Errorf("the Run %s: %w", run.id, err)
	}
	if pr.Author.ID == "" && pr.Author.UUID == "" {
		// With no author, the factory cannot tell its own comments.
		return nil, fmt.Errorf("the Run %s: Bitbucket gives no author of the PR %d", run.id, id)
	}
	comments, err := f.PRComments(ctx, repository, id)
	if err != nil {
		return nil, fmt.Errorf("the Run %s: %w", run.id, err)
	}
	review := readReview(comments, pr.Author, d.config.triggerWord())
	if len(review.triggers) == 0 {
		return nil, nil
	}
	more := map[string]any{"message.source": "pr_comment", "factory.connection": source.Name,
		"factory.repository": repository, "factory.pr": id}
	got, err := d.store.acceptPRComments(run, review, more, d.now().UTC())
	if errors.Is(err, errCommentRead) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("the Run %s: %w", run.id, err)
	}
	log = append(log, "comment", got.trigger.ID, "by", got.trigger.Author.Name, "by_id", got.trigger.Author.ID)
	switch {
	case got.refused != nil && got.answer:
		return []string{commentsEntityPrefix + entity}, d.answerClosed(ctx, f, repository, id, run.id, got, log)
	case got.refused != nil:
		d.log.Info("a trigger comment on a PR did not continue its Run", append(log, "reason", got.refused.Error())...)
		return nil, nil
	}
	d.log.Info("a trigger comment on a PR is a message to its Run", append(log, "message", got.accepted.ID,
		"comments", got.sent, "continued", got.accepted.Continued)...)
	d.commandsAdded.wake()
	if got.accepted.Continued {
		d.wakeDispatch()
	}
	return []string{commentsEntityPrefix + entity}, nil
}

// prRun is the newest Run of the Organization whose PR is the PR with id
// in the repository <workspace>/<slug>, with its Forge and repository.
// The Run id is "" when no Run has the PR.
func (d *Dispatcher) prRun(organizationID, fullName string, id int) (closingRun, forge.Forge, string, error) {
	runs, err := d.store.closingRuns(`organization_id = ? AND pr_id = ?`, organizationID, id)
	if err != nil {
		return closingRun{}, nil, "", err
	}
	var errs []error
	for _, r := range slices.Backward(runs) {
		f, repository, err := d.runForge(r)
		if err != nil {
			errs = append(errs, fmt.Errorf("the Run %s: %w", r.id, err))
			continue
		}
		if strings.EqualFold(repository, forge.BitbucketHost+"/"+fullName) {
			return r, f, repository, nil
		}
	}
	return closingRun{}, nil, "", errors.Join(errs...)
}

// review is what the comments on a PR give a Run: the trigger comments
// and the other comments of persons, each not resolved and not deleted,
// and the thread of each comment.
type review struct {
	// triggers are the trigger comments, the oldest first.
	triggers []forge.Comment
	// others are the other comments, by thread and then the oldest first.
	others []forge.Comment
	// thread is the first comment of each comment's thread.
	thread map[int]int
}

// readReview sorts the comments of a PR whose author is own (the
// factory's identity).
func readReview(comments []forge.Comment, own forge.Account, word string) review {
	byID := make(map[int]forge.Comment, len(comments))
	for _, c := range comments {
		byID[c.ID] = c
	}
	r := review{thread: map[int]int{}}
	for _, c := range comments {
		// The thread is the first comment of the chain of parents; a
		// resolution on any comment of the chain resolves the comment.
		root, resolved := c, c.Resolved
		for seen := 0; root.Parent != 0 && seen < len(comments); seen++ {
			parent, ok := byID[root.Parent]
			if !ok {
				break
			}
			root, resolved = parent, resolved || parent.Resolved
		}
		r.thread[c.ID] = root.ID
		if resolved || c.Deleted || c.Pending || !c.Author.Person() || c.Author.Same(own) ||
			strings.TrimSpace(c.Text) == "" {
			continue
		}
		if triggers(c.Text, word) {
			r.triggers = append(r.triggers, c)
		} else {
			r.others = append(r.others, c)
		}
	}
	slices.SortStableFunc(r.triggers, func(a, b forge.Comment) int { return cmp.Compare(a.ID, b.ID) })
	r.sortByThread(r.others)
	return r
}

func (r review) sortByThread(list []forge.Comment) {
	slices.SortStableFunc(list, func(a, b forge.Comment) int {
		return cmp.Or(cmp.Compare(r.thread[a.ID], r.thread[b.ID]), cmp.Compare(a.ID, b.ID))
	})
}

// heading names a comment, its author, its place and its thread.
func (r review) heading(c forge.Comment) string {
	place := "a general comment"
	switch {
	case c.File != "" && c.Line > 0:
		place = "on " + c.File + " line " + strconv.Itoa(c.Line)
	case c.File != "":
		place = "on " + c.File
	}
	return fmt.Sprintf("Comment %d by %s, %s, in thread %d", c.ID, c.Author.Name, place, r.thread[c.ID])
}

// The parts of a message from PR comments.
const (
	othersHeading = "The other comments on the PR that are not resolved, and that the Run did not get before:"
	// moreRoom is the room that the message keeps for the line that
	// says how many comments did not fit.
	moreRoom = 200
)

// message is the text of a message from the trigger comment and the
// other comments: the trigger's text first, then each other comment that
// fits in dispatcherapi.MaxMessage. It gives the comments that are in it.
func (r review) message(trigger forge.Comment, others []forge.Comment) (string, []forge.Comment) {
	var b strings.Builder
	b.WriteString(messageText(trigger.Text))
	footer := "\n\n(" + r.heading(trigger) + ".)"
	if b.Len()+len(footer) <= dispatcherapi.MaxMessage-moreRoom {
		b.WriteString(footer)
	}
	var in []forge.Comment
	for k, c := range others {
		part := "\n\n" + r.heading(c) + ":\n" + strings.TrimSpace(c.Text)
		if k == 0 {
			part = "\n\n" + othersHeading + part
		}
		if b.Len()+len(part) > dispatcherapi.MaxMessage-moreRoom {
			fmt.Fprintf(&b, "\n\n(%d more comments did not fit in this message: the next trigger comment gives them.)",
				len(others)-k)
			break
		}
		b.WriteString(part)
		in = append(in, c)
	}
	return b.String(), in
}

// errCommentRead is a trigger comment whose edit the Dispatcher read
// before.
var errCommentRead = errors.New("the comment was read before")

// acceptedComments is what acceptPRComments did.
type acceptedComments struct {
	trigger  forge.Comment
	accepted dispatcherapi.MessageAccepted
	// sent is the number of other comments in the message.
	sent int
	// refused is the refusal of a Run that takes no message.
	refused *notMessageable
	// answer is true when the Closed Run's PR gets an answer comment at
	// answeredAt, and lastAnswer is the time of the answer before it
	// (Unix microseconds).
	answer                 bool
	answeredAt, lastAnswer int64
}

// acceptPRComments makes the message of the trigger comments and other
// comments that the Run did not read, and accepts it, in one
// transaction. The newest trigger comment is the message's first part;
// the other new trigger comments are among the other comments. Each
// comment in the message is then read. When the Run takes no message,
// only the trigger comments are read, and a Closed Run whose PR had no
// answer comment in the last day gets one. It gives errCommentRead when
// each trigger comment was read before.
func (s *store) acceptPRComments(run closingRun, r review, more map[string]any, at time.Time) (acceptedComments,
	error) {
	var got acceptedComments
	tx, err := s.db.Begin()
	if err != nil {
		return got, err
	}
	defer tx.Rollback()
	read, err := readPRComments(tx, run.id)
	if err != nil {
		return got, err
	}
	unread := func(c forge.Comment) bool { e, ok := read[c.ID]; return !ok || e != c.Edited }
	triggers := slices.DeleteFunc(slices.Clone(r.triggers), func(c forge.Comment) bool { return !unread(c) })
	if len(triggers) == 0 {
		return got, errCommentRead
	}
	got.trigger = triggers[len(triggers)-1]
	others := slices.DeleteFunc(slices.Clone(r.others), func(c forge.Comment) bool { return !unread(c) })
	others = append(others, triggers[:len(triggers)-1]...)
	r.sortByThread(others)
	text, in := r.message(got.trigger, others)

	by := actor{kind: actorForge, id: cmp.Or(got.trigger.Author.ID, got.trigger.Author.UUID), name: got.trigger.Author.Name}
	more["factory.comment"] = got.trigger.ID
	more["factory.comments"] = len(in)
	got.accepted, err = acceptMessageIn(tx, run.organizationID, run.id, by, text, at, more)
	switch {
	case errors.As(err, &got.refused):
		in = nil
		if got.refused.closed {
			if err := tx.QueryRow(`SELECT closed_answer_at FROM runs WHERE id = ?`, run.id).Scan(&got.lastAnswer); err != nil {
				return got, err
			}
			got.answer = at.Sub(time.UnixMicro(got.lastAnswer)) >= closedAnswerEvery
			if got.answer {
				got.answeredAt = at.UnixMicro()
				if _, err := tx.Exec(`UPDATE runs SET closed_answer_at = ? WHERE id = ?`, got.answeredAt, run.id); err != nil {
					return got, err
				}
			}
		}
	case err != nil:
		return got, err
	}
	got.sent = len(in)
	// An older trigger that did not fit stays unread: the next trigger
	// gives it.
	done := append([]forge.Comment{got.trigger}, in...)
	if got.refused != nil {
		done = triggers
	}
	for _, c := range done {
		if _, err := tx.Exec(`INSERT INTO pr_comments (run_id, comment_id, edit, read_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (run_id, comment_id) DO UPDATE SET edit = excluded.edit, read_at = excluded.read_at`,
			run.id, c.ID, c.Edited, at.UnixMicro()); err != nil {
			return got, err
		}
	}
	return got, s.commit(tx)
}

// readPRComments gives the edit of each comment that the Run read.
func readPRComments(tx *sql.Tx, runID string) (map[int]string, error) {
	rows, err := tx.Query(`SELECT comment_id, edit FROM pr_comments WHERE run_id = ?`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	read := map[int]string{}
	for rows.Next() {
		var id int
		var edit string
		if err := rows.Scan(&id, &edit); err != nil {
			return nil, err
		}
		read[id] = edit
	}
	return read, rows.Err()
}

// answerClosed posts the answer comment of a Closed Run, as a reply to
// the trigger comment. When the post fails, the next trigger comment can
// get the answer.
func (d *Dispatcher) answerClosed(ctx context.Context, f forge.Forge, repository string, id int, runID string,
	got acceptedComments, log []any) error {
	link, err := d.store.runLink(d.config, runID)
	if err != nil {
		return err
	}
	text := "Run " + runID + " is closed: its PR is merged or declined, or it had no activity for 30 days. " +
		"A comment cannot continue a Closed Run, so the factory did not act on this comment. To continue the work, " +
		"start a new Run (for example with 'solidtrace start', or from a ticket). The factory gives this answer " +
		"at most one time a day on this PR.\n\nThe Run: " + link
	_, err = f.PostPRComment(ctx, repository, id, forge.NewComment{Text: text, Parent: got.trigger.ID})
	if err != nil {
		if _, undo := d.store.db.Exec(`UPDATE runs SET closed_answer_at = ? WHERE id = ? AND closed_answer_at = ?`,
			got.lastAnswer, runID, got.answeredAt); undo != nil {
			d.log.Error("could not undo the time of an answer comment", "run", runID, "error", undo)
		}
		return fmt.Errorf("the Run %s: %w", runID, err)
	}
	d.log.Info("a trigger comment on the PR of a Closed Run got the answer that the Run is closed", log...)
	return nil
}
