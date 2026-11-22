package dispatcher

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// Comments on a ticket continue its Run (P4). A comment that starts with
// the trigger word (default /agent) on the ticket of a Run is a message
// to the Run, as a person's message from the CLI is. Only an insider's
// comment steers a Run: the kind of the tracker says who that is (a
// member or an invited guest; never the connection's own identity, a
// bot, an integration, an external user, or a page that the public can
// see). The comments go to the ticket's newest Run, and to no Run when
// that Run is Closed. The reconcile step of a ticket reads its comments
// when the ticket's newest Run is not Closed. The step of the whole
// connection (the poll and the backstop) reads the comments of each
// ticket with an Active Run, and, at most every commentsAllEvery, of each
// ticket with a Run that is not Closed. A
// comment goes to a Run one time, and again after an edit.

// commentRun is the Run that a ticket's comments go to.
type commentRun struct {
	id, organizationID string
	createdAt          time.Time
}

// commentRun is the newest Run of the connection's ticket. It is false
// when the ticket has no Run, or when its newest Run is Closed: a
// Closed Run does not continue, and an older Run of the ticket is not
// for the comments.
func (s *store) commentRun(connectionID, ticketID string) (commentRun, bool, error) {
	var r commentRun
	var created int64
	var closed bool
	err := s.db.QueryRow(`SELECT id, organization_id, created_at, closed_at IS NOT NULL FROM runs
		WHERE ticket_connection_id = ? AND ticket_id = ? ORDER BY created_at DESC, id DESC LIMIT 1`,
		connectionID, ticketID).Scan(&r.id, &r.organizationID, &created, &closed)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && closed) {
		return commentRun{}, false, nil
	}
	r.createdAt = time.UnixMicro(created)
	return r, err == nil, err
}

// commentsAllEvery is the shortest time between two steps of a whole
// connection that read the comments of each ticket whose Run is not
// Closed. The steps between them (Notion's poll every 30 seconds) read
// only the tickets whose Run is Active, so that the poll keeps in the
// tracker's rate limit.
const commentsAllEvery = 5 * time.Minute

// commentPasses holds the time when each connection's step last read the
// comments of each ticket whose Run is not Closed.
type commentPasses struct {
	mu sync.Mutex
	at map[string]time.Time
}

// due is true when the connection's step reads the comments of each
// ticket whose Run is not Closed at now; it then keeps now.
func (p *commentPasses) due(connectionID string, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if last, ok := p.at[connectionID]; ok && now.Sub(last) < commentsAllEvery {
		return false
	}
	if p.at == nil {
		p.at = map[string]time.Time{}
	}
	p.at[connectionID] = now
	return true
}

// commentTickets are the tickets of the connection that have a Run that
// is not Closed, or, when all is false, that have an Active Run.
func (s *store) commentTickets(connectionID string, all bool) ([]string, error) {
	where, args := `closed_at IS NULL`, []any{connectionID}
	if !all {
		where = activeRunSQL()
		args = append(args, activeRunArgs()...)
	}
	rows, err := s.db.Query(`SELECT DISTINCT ticket_id FROM runs WHERE ticket_connection_id = ? AND ticket_id != ''
		AND `+where+` ORDER BY ticket_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// acceptTicketComment keeps the edit of a trigger comment as read, and,
// for an insider's comment, accepts it as a message to the Run, in one
// transaction. It gives errCommentRead for an edit that it read before,
// and the refusal of a Run that takes no message (the comment is then
// read all the same).
func (s *store) acceptTicketComment(connectionID, ticketID string, run commentRun, c TicketComment, text string,
	more map[string]any, at time.Time) (dispatcherapi.MessageAccepted, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return dispatcherapi.MessageAccepted{}, err
	}
	defer tx.Rollback()
	var edit string
	err = tx.QueryRow(`SELECT edit FROM ticket_comments WHERE connection_id = ? AND comment_id = ?`, connectionID, c.ID).
		Scan(&edit)
	switch {
	case err == nil && edit == c.Edit:
		return dispatcherapi.MessageAccepted{}, errCommentRead
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return dispatcherapi.MessageAccepted{}, err
	}
	if _, err := tx.Exec(`INSERT INTO ticket_comments (connection_id, comment_id, ticket_id, run_id, edit, read_at)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (connection_id, comment_id) DO UPDATE SET run_id = excluded.run_id,
		edit = excluded.edit, read_at = excluded.read_at`, connectionID, c.ID, ticketID, run.id, c.Edit,
		at.UnixMicro()); err != nil {
		return dispatcherapi.MessageAccepted{}, err
	}
	var accepted dispatcherapi.MessageAccepted
	var refused *notMessageable
	if c.Insider {
		by := actor{kind: actorTracker, id: c.By.ID, name: c.By.Name}
		accepted, err = acceptMessageIn(tx, run.organizationID, run.id, by, text, at, more)
		if err != nil && !errors.As(err, &refused) {
			return accepted, err
		}
	}
	if err := s.commit(tx); err != nil {
		return accepted, err
	}
	if refused != nil {
		return accepted, refused
	}
	return accepted, nil
}

// comments acts on the trigger comments on a ticket that has a Run that
// is not Closed. It is true when a comment became a message to the Run.
func (s *ticketStep) comments(ctx context.Context, ticketID string) (bool, error) {
	d, c := s.d, s.c
	run, ok, err := d.store.commentRun(c.ID, ticketID)
	if err != nil || !ok {
		return false, err
	}
	comments, err := s.t.Comments(ctx, c, ticketID)
	if errors.Is(err, ErrNoTicket) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	word := d.config.triggerWord()
	// A comment from before the Run is not for it. Some trackers give
	// times in whole minutes: a comment of the Run's first minute is for
	// it.
	since := run.createdAt.Truncate(time.Minute)
	acted := false
	for _, comment := range comments {
		if !triggers(comment.Text, word) || comment.CreatedAt.Before(since) {
			continue
		}
		text := messageText(comment.Text)
		more := map[string]any{"message.source": "ticket_comment", "factory.connection": c.Name,
			"factory.connection_kind": c.Kind, "factory.ticket": ticketID, "factory.comment": comment.ID}
		accepted, err := d.store.acceptTicketComment(c.ID, ticketID, run, comment, text, more, d.now().UTC())
		var refused *notMessageable
		args := []any{"connection", c.Name, "ticket", ticketID, "comment", comment.ID, "run", run.id,
			"by", comment.By.Name, "by_id", comment.By.ID}
		switch {
		case errors.Is(err, errCommentRead):
			continue
		case errors.As(err, &refused):
			d.log.Info("a trigger comment on a ticket did not continue its Run", append(args, "reason", refused.Error())...)
			continue
		case err != nil:
			return acted, err
		case !comment.Insider:
			d.log.Info("a trigger comment on a ticket cannot steer its Run", append(args, "reason", comment.Refusal)...)
			continue
		}
		d.log.Info("a trigger comment on a ticket is a message to its Run", append(args, "message", accepted.ID,
			"continued", accepted.Continued)...)
		d.commandsAdded.wake()
		if accepted.Continued {
			d.wakeDispatch()
		}
		acted = true
	}
	return acted, nil
}
