package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// operator runs 'dispatcher accounts args...' on the server VM, with stdin.
func operator(t *testing.T, database, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errs bytes.Buffer
	code = command(context.Background(), append([]string{"accounts"}, args...), env(map[string]string{
		"DISPATCHER_DATABASE":       database,
		"DISPATCHER_ENCRYPTION_KEY": testKey,
	}), strings.NewReader(stdin), &out, &errs)
	return code, out.String(), errs.String()
}

// newDatabase makes the Dispatcher's database, as the Dispatcher's first
// start does, and gives its path.
func newDatabase(t *testing.T) string {
	t.Helper()
	db := filepath.Join(t.TempDir(), "d.sqlite3")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stderr bytes.Buffer
	if code := run(ctx, nil, env(map[string]string{
		"DISPATCHER_SOLIDTRACE_URL":   "https://console.example.com",
		"DISPATCHER_SOLIDTRACE_TOKEN": "stk_x",
		"DISPATCHER_DATABASE":         db,
		"DISPATCHER_ENCRYPTION_KEY":   testKey,
		"DISPATCHER_LISTEN":           freeAddress(t),
	}), &stderr); code != 0 {
		t.Fatalf("the first start: exit %d: %s", code, stderr.String())
	}
	return db
}

const anaToken = "sk-ant-oat01-ana-token-from-setup-token"

func TestTheOperatorAddsListsAndRemovesAccountsAndTheListShowsNoToken(t *testing.T) {
	db := newDatabase(t)

	if code, out, errs := operator(t, db, anaToken+"\n", "add", "ana"); code != 0 || !strings.Contains(out, "ana") {
		t.Fatalf("add: exit %d\n%s%s", code, out, errs)
	}
	if code, out, errs := operator(t, db, "sk-ant-oat01-ben-token\n", "add", "ben"); code != 0 {
		t.Fatalf("add: exit %d\n%s%s", code, out, errs)
	}
	code, out, errs := operator(t, db, "", "list")
	if code != 0 || !strings.Contains(out, "ana") || !strings.Contains(out, "ben") {
		t.Fatalf("list: exit %d\n%s%s", code, out, errs)
	}
	if strings.Contains(out, "sk-ant") {
		t.Errorf("the list shows a token:\n%s", out)
	}
	// The token is stored encrypted: no file of the database holds it.
	files, _ := filepath.Glob(db + "*")
	for _, f := range files {
		if b, _ := os.ReadFile(f); bytes.Contains(b, []byte("ana-token")) {
			t.Errorf("%s holds the token in clear text", filepath.Base(f))
		}
	}

	if code, out, errs := operator(t, db, "", "remove", "ana"); code != 0 {
		t.Fatalf("remove: exit %d\n%s%s", code, out, errs)
	}
	_, out, _ = operator(t, db, "", "list")
	if strings.Contains(out, "ana") || !strings.Contains(out, "ben") {
		t.Errorf("the list after the remove:\n%s", out)
	}
	if code, _, errs := operator(t, db, "", "remove", "ana"); code != 1 || !strings.Contains(errs, "ana") {
		t.Errorf("a remove of an absent account: exit %d, %q", code, errs)
	}
}

func TestTheOperatorCommandRefusesAnAbsentTokenAndAnAPIKey(t *testing.T) {
	db := newDatabase(t)
	for stdin, want := range map[string]string{
		"":                          "claude setup-token",
		"sk-ant-api03-an-api-key\n": "API key",
	} {
		code, _, errs := operator(t, db, stdin, "add", "ana")
		if code != 1 || !strings.Contains(errs, want) {
			t.Errorf("token %q: exit %d, stderr %q, want %q in it", stdin, code, errs, want)
		}
	}
	if _, out, _ := operator(t, db, "", "list"); strings.Contains(out, "ana") {
		t.Errorf("a refused account is in the list:\n%s", out)
	}
}

func TestTheOperatorCommandNeedsOnlyTheDatabaseAndTheKey(t *testing.T) {
	var out, errs bytes.Buffer
	code := command(context.Background(), []string{"accounts", "list"}, env(nil), strings.NewReader(""), &out, &errs)
	if code != 1 || !strings.Contains(errs.String(), "DISPATCHER_DATABASE") ||
		strings.Contains(errs.String(), "DISPATCHER_SOLIDTRACE_URL") {
		t.Errorf("exit %d, stderr %q", code, errs.String())
	}
}

func TestTheOperatorCommandRefusesADatabaseThatIsNotThere(t *testing.T) {
	db := filepath.Join(t.TempDir(), "typo.sqlite3")
	code, out, errs := operator(t, db, anaToken+"\n", "add", "ana")
	if code != 1 || !strings.Contains(errs, db) || !strings.Contains(errs, "first start") {
		t.Errorf("exit %d, stdout %q, stderr %q, want 1 and an error that names the path", code, out, errs)
	}
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Errorf("the command made a database at %s", db)
	}
}

func TestAddReadsTheTokenToTheEndOfItsLineAndDoesNotWaitForTheEndOfStdin(t *testing.T) {
	db := newDatabase(t)
	// As an operator who pastes the token and presses Enter: stdin stays
	// open.
	stdin, paste := io.Pipe()
	defer paste.Close()
	go paste.Write([]byte(anaToken + "\n"))
	ended := make(chan int, 1)
	go func() {
		var out, errs bytes.Buffer
		ended <- command(context.Background(), []string{"accounts", "add", "ana"}, env(map[string]string{
			"DISPATCHER_DATABASE":       db,
			"DISPATCHER_ENCRYPTION_KEY": testKey,
		}), stdin, &out, &errs)
	}()
	select {
	case code := <-ended:
		if code != 0 {
			t.Errorf("exit %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("add waits for the end of stdin after the token's line")
	}
}
