package dispatcher

import (
	"bufio"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
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
func (s *store) markAtLimit(name string, until time.Time) error {
	res, err := s.db.Exec(`UPDATE accounts SET limited_until = ? WHERE name = ?`, until.UnixMicro(), name)
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

// pickAccount picks the account for a new Execution: the free account (not
// at its usage limit at now) with the fewest live Executions. With no free
// account, it gives "" and the reason that the Run's page shows.
func pickAccount(tx *sql.Tx, now time.Time) (name, reason string, err error) {
	err = tx.QueryRow(`SELECT a.name FROM accounts a WHERE a.limited_until IS NULL OR a.limited_until <= ?
		ORDER BY (SELECT COUNT(*) FROM executions e WHERE e.account = a.name AND e.state != ?), a.added_at, a.name
		LIMIT 1`, now.UnixMicro(), dispatcherapi.ExecutionEnded).Scan(&name)
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		return name, "", err
	}
	var first sql.NullInt64
	if err := tx.QueryRow(`SELECT MIN(limited_until) FROM accounts`).Scan(&first); err != nil {
		return "", "", err
	}
	if !first.Valid {
		return "", "no free account: the pool has no account (an operator adds one with 'dispatcher accounts add')", nil
	}
	return "", "no free account: each account is at its usage limit; the first is free again at " +
		time.UnixMicro(first.Int64).UTC().Format("2006-01-02 15:04 UTC"), nil
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
