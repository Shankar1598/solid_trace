package dispatcher

import (
	"bufio"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The pool of subscription accounts is the MVP's Credential source. The
// four verbs of the interface are here: list the accounts (accounts), pick
// one for an Execution (pickAccount, in the same transaction that adds
// the Execution), mark one at its limit until a reset time (MarkAtLimit),
// and give the token to the wrapper (accountToken, in the task call).

// Account is one subscription account of the pool, with no token.
type Account struct {
	Name    string
	AddedAt time.Time
	// AtLimitUntil is the reset time of an account at its usage limit
	// (nil: not at its limit).
	AtLimitUntil *time.Time
	// LiveExecutions counts the Executions on the account that have not
	// ended.
	LiveExecutions int
}

// accountName is the form of an account's name, such as an email
// address or "pool-1".
var accountName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._+-]{0,99}$`)

func accountPurpose(name string) string { return "account " + name }

// addAccount adds an account to the pool, or replaces the token of the
// account with that name (its limit stays). It is true for a replace.
func (s *store) addAccount(name, token string, at time.Time) (bool, error) {
	var replaced bool
	err := s.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM accounts WHERE name = ?)`, name).Scan(&replaced)
	if err != nil {
		return false, err
	}
	_, err = s.db.Exec(`INSERT INTO accounts (name, token, added_at) VALUES (?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET token = excluded.token`,
		name, s.sealer.seal(accountPurpose(name), []byte(token)), at.UnixMicro())
	return replaced, err
}

// removeAccount takes an account out of the pool. Its live Executions
// keep the token that they got. It is false when there is no such
// account.
func (s *store) removeAccount(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM accounts WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// accounts lists the pool, oldest first. A limit that ended before now is
// not shown.
func (s *store) accounts(now time.Time) ([]Account, error) {
	rows, err := s.db.Query(`SELECT a.name, a.added_at, a.limited_until, (SELECT COUNT(*) FROM executions e
		WHERE e.account = a.name AND e.state != ?) FROM accounts a ORDER BY a.added_at, a.name`,
		dispatcherapi.ExecutionEnded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Account
	for rows.Next() {
		var a Account
		var added int64
		var limited sql.NullInt64
		if err := rows.Scan(&a.Name, &added, &limited, &a.LiveExecutions); err != nil {
			return nil, err
		}
		a.AddedAt = time.UnixMicro(added).UTC()
		if limited.Valid && limited.Int64 > now.UnixMicro() {
			a.AtLimitUntil = timeOf(limited)
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// markAtLimit marks an account at its usage limit until the reset time.
// It keeps a later reset time that the account has: a limit end with no
// reset time from the harness (the one-hour default) does not make a
// reset time that the harness gave before shorter. A limit that ended is
// always earlier than a new reset time.
func (s *store) markAtLimit(name string, until time.Time) error {
	res, err := s.db.Exec(`UPDATE accounts SET limited_until = MAX(COALESCE(limited_until, 0), ?) WHERE name = ?`,
		until.UnixMicro(), name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return nil
}

// accountToken opens the token of an account. It is errNotFound when the
// account is no longer in the pool.
func (s *store) accountToken(name string) (string, error) {
	var sealed []byte
	err := s.db.QueryRow(`SELECT token FROM accounts WHERE name = ?`, name).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNotFound
	}
	if err != nil {
		return "", err
	}
	token, err := s.sealer.open(accountPurpose(name), sealed)
	return string(token), err
}

// limitLayout is how a reason and the operator command show a reset time.
const limitLayout = "2006-01-02 15:04 UTC"

func limitText(at time.Time) string { return at.UTC().Format(limitLayout) }

// pickAccount picks the account for a new Execution: the free account (not
// at its usage limit at now) with the fewest live Executions, other than
// except ("" is none). With no free account, it gives "" and the reason
// that the Run's page shows.
func pickAccount(tx *sql.Tx, now time.Time, except string) (name, reason string, err error) {
	err = tx.QueryRow(`SELECT a.name FROM accounts a WHERE (a.limited_until IS NULL OR a.limited_until <= ?)
		AND a.name != ?
		ORDER BY (SELECT COUNT(*) FROM executions e WHERE e.account = a.name AND e.state != ?), a.added_at, a.name
		LIMIT 1`, now.UnixMicro(), except, dispatcherapi.ExecutionEnded).Scan(&name)
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		return name, "", err
	}
	var others int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM accounts WHERE name != ?`, except).Scan(&others); err != nil {
		return "", "", err
	}
	var first sql.NullInt64
	if err := tx.QueryRow(`SELECT MIN(limited_until) FROM accounts WHERE limited_until > ?`, now.UnixMicro()).
		Scan(&first); err != nil {
		return "", "", err
	}
	switch {
	case others == 0 && except == "":
		return "", "no free account: the pool has no account (an operator adds one with 'dispatcher accounts add')", nil
	case others == 0:
		return "", "no other account is free: the pool has no other account (an operator adds one with " +
			"'dispatcher accounts add')", nil
	case except == "":
		return "", "no free account: each account is at its usage limit; the first is free again at " +
			limitText(time.UnixMicro(first.Int64)), nil
	}
	return "", "no other account is free: each account is at its usage limit; the first is free again at " +
		limitText(time.UnixMicro(first.Int64)), nil
}

// lastAccount is the account of the Run's last Execution ("" is none).
func lastAccount(tx *sql.Tx, runID string) (string, error) {
	var name string
	err := tx.QueryRow(`SELECT account FROM executions WHERE run_id = ? AND account != ''
		ORDER BY dispatched_at DESC, rowid DESC LIMIT 1`, runID).Scan(&name)
	return name, ignoreNoRows(err)
}

// limitOf is the reset time of an account at its usage limit at now (nil:
// it is free, or it is not in the pool).
func limitOf(tx *sql.Tx, name string, now time.Time) (*time.Time, error) {
	var limited sql.NullInt64
	err := tx.QueryRow(`SELECT limited_until FROM accounts WHERE name = ?`, name).Scan(&limited)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (!limited.Valid || limited.Int64 <= now.UnixMicro())) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return timeOf(limited), nil
}

// accountFor gives the account for the Run's next Execution at now, or ""
// and the reason when there is none. The account that a switch chose
// (runs.next_account) comes first when it is free. With the wait setting,
// the Run stays on its account (the switch's, else its last Execution's)
// and waits for its reset time. An account that is no longer in the pool
// does not hold a Run.
func (s *store) accountFor(tx *sql.Tx, runID string, now time.Time) (name, reason string, err error) {
	var want string
	if err := tx.QueryRow(`SELECT next_account FROM runs WHERE id = ?`, runID).Scan(&want); err != nil {
		return "", "", err
	}
	if want == "" && s.waitAtLimit {
		if want, err = lastAccount(tx, runID); err != nil {
			return "", "", err
		}
	}
	if want != "" {
		var exists bool
		if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM accounts WHERE name = ?)`, want).Scan(&exists); err != nil {
			return "", "", err
		}
		until, err := limitOf(tx, want, now)
		switch {
		case err != nil:
			return "", "", err
		case exists && until == nil:
			return want, "", nil
		case exists && s.waitAtLimit:
			return "", "the Run's account " + want + " is at its usage limit until " + limitText(*until) +
				": the Run waits for it", nil
		}
	}
	return pickAccount(tx, now, "")
}

// The reset time of an account at its usage limit, when the harness gives
// none, or one that is not in the future: an hour from the end. A reset
// time after maxLimitWait is cut to it, so that a wrong time does not take
// an account out of the pool for longer than the weekly window.
const (
	defaultLimitWait = time.Hour
	maxLimitWait     = 7 * 24 * time.Hour
)

// limitResetTime is the reset time of the account of an Execution that
// ended at its usage limit at now, from the time that the harness gave
// (nil: none).
func limitResetTime(given *time.Time, now time.Time) time.Time {
	switch {
	case given == nil || !given.After(now):
		return now.Add(defaultLimitWait)
	case given.After(now.Add(maxLimitWait)):
		return now.Add(maxLimitWait)
	}
	return *given
}

// eventAccountSwitched is the event of a Run that moves to another
// account: account.old is the account at its usage limit, account.new
// the account of the next Execution. Its actor is the person who switched
// it, or the Dispatcher for the automatic switch.
const eventAccountSwitched = "account_switched"

// reasonAccountFree is the reason of a Run that waited on an account
// limit and continues.
const reasonAccountFree = "an account is free again: a new Execution resumes the Agent session"

// atUsageLimit decides, in the end's transaction, where a Run goes when
// its Execution on account ended at the usage limit: queued on another
// free account (next is that account), or waiting on an account limit.
// With the wait setting, it always waits for its own account.
func (s *store) atUsageLimit(tx *sql.Tx, account string, now time.Time) (state, reason, next string, err error) {
	at := "the account " + account + " is at its usage limit"
	if account == "" {
		at = "the account is at its usage limit"
	}
	until, err := limitOf(tx, account, now)
	if err != nil {
		return "", "", "", err
	}
	if until != nil {
		at += " until " + limitText(*until)
	}
	if !s.waitAtLimit {
		if next, _, err = pickAccount(tx, now, account); err != nil {
			return "", "", "", err
		}
		if next != "" {
			return dispatcherapi.StateQueued, at + ": the Run continues on the account " + next, next, nil
		}
		return dispatcherapi.StateWaitingOnAccountLimit, at + ", and no other account is free: the Run continues " +
			"when an account is free again", "", nil
	}
	return dispatcherapi.StateWaitingOnAccountLimit, at + ": the Run waits for it (the operator's setting " +
		EnvAtAccountLimit + "=" + AtAccountLimitWait + "); a person can switch its account", "", nil
}

// continueWaitingRuns queues each Run that waits on an account limit and
// has an account at now: a new Execution resumes its Agent session.
func (s *store) continueWaitingRuns(now time.Time) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id FROM runs WHERE state = ? AND closed_at IS NULL AND pr_outcome = ''
		ORDER BY created_at, id`, dispatcherapi.StateWaitingOnAccountLimit)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	continued := 0
	for _, id := range ids {
		account, _, err := s.accountFor(tx, id, now)
		if err != nil {
			return 0, err
		}
		if account == "" {
			continue
		}
		if _, err := tx.Exec(`UPDATE runs SET state = ?, reason = ? WHERE id = ?`, dispatcherapi.StateQueued,
			reasonAccountFree, id); err != nil {
			return 0, err
		}
		details := actor{kind: actorDispatcher, name: "Dispatcher"}.details()
		details["run.continued_from"] = dispatcherapi.StateWaitingOnAccountLimit
		if err := recordEvent(tx, now, id, eventQueued, details); err != nil {
			return 0, err
		}
		continued++
	}
	if continued == 0 {
		return 0, nil
	}
	return continued, s.commit(tx)
}

// notSwitchable is the error of a switch of a Run's account that cannot
// be done.
type notSwitchable struct{ why string }

func (e *notSwitchable) Error() string { return e.why }

// switchAccount moves a Run of the Organization that waits on an account
// limit to another free account, in one transaction: the Run is queued,
// and its next Execution gets that account. It gives the old and the new
// account, errNotFound or a *notSwitchable.
func (s *store) switchAccount(organizationID, runID string, by actor, at time.Time) (old, next string, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	var state string
	var closed bool
	err = tx.QueryRow(`SELECT state, closed_at IS NOT NULL FROM runs WHERE id = ? AND organization_id = ?`, runID,
		organizationID).Scan(&state, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", errNotFound
	}
	if err != nil {
		return "", "", err
	}
	if state != dispatcherapi.StateWaitingOnAccountLimit || closed {
		return "", "", &notSwitchable{"the Run is " + state + ": only a Run that waits on an account limit can switch " +
			"its account"}
	}
	if old, err = lastAccount(tx, runID); err != nil {
		return "", "", err
	}
	next, reason, err := pickAccount(tx, at, old)
	if err != nil {
		return "", "", err
	}
	if next == "" {
		return "", "", &notSwitchable{reason}
	}
	if _, err := tx.Exec(`UPDATE runs SET state = ?, reason = ?, next_account = ? WHERE id = ?`,
		dispatcherapi.StateQueued, by.name+" switched the Run from the account "+old+" to "+next, next, runID); err != nil {
		return "", "", err
	}
	details := by.details()
	details["account.old"], details["account.new"] = old, next
	if err := recordEvent(tx, at, runID, eventAccountSwitched, details); err != nil {
		return "", "", err
	}
	return old, next, s.commit(tx)
}

// switchAccount is a person's switch of the account of a Run that waits
// on an account limit.
func (d *Dispatcher) switchAccount(w http.ResponseWriter, r *http.Request, p person) {
	id := r.PathValue("id")
	old, next, err := d.store.switchAccount(p.organization.id, id, actor{kind: actorPerson, id: p.id, name: p.name},
		d.now())
	var refused *notSwitchable
	switch {
	case errors.Is(err, errNotFound):
		writeProblem(w, http.StatusNotFound, "no Run "+id+" in this Organization")
		return
	case errors.As(err, &refused):
		writeProblem(w, http.StatusConflict, refused.Error())
		return
	case err != nil:
		d.serverError(w, "switch the Run's account", err)
		return
	}
	d.wakeDispatch()
	run, err := d.store.run(p.organization.id, id)
	if err != nil {
		d.serverError(w, "read the Run", err)
		return
	}
	writeJSON(w, http.StatusOK, dispatcherapi.AccountSwitched{RunID: id, Old: old, New: next,
		URL: d.withURL(run, p).URL})
}

// MarkAtLimit marks an account at its usage limit until the reset time: no
// new Execution gets it before then.
func (d *Dispatcher) MarkAtLimit(name string, until time.Time) error {
	if err := d.store.markAtLimit(name, until); errors.Is(err, errNotFound) {
		return fmt.Errorf("no account %q in the pool", name)
	} else if err != nil {
		return err
	}
	d.wakeDispatch()
	return nil
}

// AccountsCommand is the operator command 'dispatcher accounts' on the
// server VM: add, list and remove the subscription accounts of the pool.
// It opens the Dispatcher's database itself, so it needs only the
// database and the encryption key of the operator configuration (from
// -config or DISPATCHER_CONFIG, and the environment). The Dispatcher sees
// a change at its next look at the queue. It returns the exit code.
func AccountsCommand(args []string, getenv func(string) string, now func() time.Time, stdin io.Reader,
	stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("dispatcher accounts", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprint(stderr, "usage: dispatcher accounts [-config FILE] add NAME   (the token from 'claude setup-token' on stdin)\n"+
			"       dispatcher accounts [-config FILE] list\n"+
			"       dispatcher accounts [-config FILE] remove NAME\n")
	}
	configFile := flags.String("config", getenv(EnvConfigFile), "the operator configuration file")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	verb, rest := flags.Arg(0), flags.Args()
	if len(rest) > 0 {
		rest = rest[1:]
	}
	wantName := verb == "add" || verb == "remove"
	if (verb != "add" && verb != "list" && verb != "remove") || (wantName && len(rest) != 1) ||
		(!wantName && len(rest) != 0) {
		flags.Usage()
		return 2
	}
	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "dispatcher accounts: "+format+"\n", a...)
		return 1
	}
	var name string
	if wantName {
		name = rest[0]
		if !accountName.MatchString(name) {
			return fail("an account name has letters, digits and @ . _ + - (at most 100), not %q", name)
		}
	}
	var token string
	if verb == "add" {
		// Read to the end of the first line only: an operator pastes the
		// token and presses Enter, and stdin stays open.
		line, err := bufio.NewReader(io.LimitReader(stdin, 64<<10)).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fail("read the token from stdin: %v", err)
		}
		token = strings.TrimSpace(line)
		switch {
		case token == "":
			return fail("give the account's token on stdin: the token that 'claude setup-token' prints")
		case strings.HasPrefix(token, "sk-ant-api"):
			return fail("this is an API key: the pool takes the subscription token that 'claude setup-token' prints")
		case strings.ContainsAny(token, " \t\r\x00"):
			return fail("a token is one word with no spaces")
		}
	}

	config, err := readConfig(*configFile, getenv)
	if err != nil {
		return fail("%v", err)
	}
	if problems := config.storeProblems(); len(problems) > 0 {
		return fail("the operator configuration is not complete: %s", strings.Join(problems, "; "))
	}
	key, err := config.encryptionKey()
	if err != nil {
		return fail("%v", err)
	}
	// SQLite makes a database file that is not there. A wrong path would
	// then give a new, empty pool that the Dispatcher never reads.
	if _, err := os.Stat(config.Database); errors.Is(err, os.ErrNotExist) {
		return fail("there is no Dispatcher database at %s (%s): give the path of the Dispatcher's database; "+
			"the Dispatcher makes it at its first start", config.Database, EnvDatabase)
	} else if err != nil {
		return fail("%v", err)
	}
	s, err := openStore(config.Database, key)
	if err != nil {
		return fail("%v", err)
	}
	defer s.close()

	switch verb {
	case "add":
		replaced, err := s.addAccount(name, token, now())
		if err != nil {
			return fail("add the account: %v", err)
		}
		if replaced {
			fmt.Fprintf(stdout, "The token of the account %s is replaced.\n", name)
		} else {
			fmt.Fprintf(stdout, "The account %s is in the pool.\n", name)
		}
	case "remove":
		removed, err := s.removeAccount(name)
		if err != nil {
			return fail("remove the account: %v", err)
		}
		if !removed {
			return fail("there is no account %s in the pool", name)
		}
		fmt.Fprintf(stdout, "The account %s is out of the pool. Its live Executions keep their token.\n", name)
	case "list":
		list, err := s.accounts(now())
		if err != nil {
			return fail("list the accounts: %v", err)
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, "The pool has no account. Add one with 'dispatcher accounts add NAME', "+
				"with the token from 'claude setup-token' on stdin.")
			return 0
		}
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tADDED\tLIVE EXECUTIONS\tAT ITS LIMIT UNTIL")
		for _, a := range list {
			limit := "-"
			if a.AtLimitUntil != nil {
				limit = a.AtLimitUntil.Format("2006-01-02 15:04 UTC")
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", a.Name, a.AddedAt.Format("2006-01-02 15:04 UTC"), a.LiveExecutions, limit)
		}
		tw.Flush()
	}
	return 0
}
