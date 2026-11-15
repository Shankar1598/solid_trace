package dispatcher

import (
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"
)

const testEncryptionKey = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="

func testStore(t *testing.T, path string) *store {
	t.Helper()
	key, _ := base64.StdEncoding.DecodeString(testEncryptionKey)
	s, err := openStore(path, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.close() })
	return s
}

// The Forge reads a connection's token for a Run (ticket 37): the token
// that an Admin saved comes back after a restart, for its Organization
// only.
func TestAConnectionsTokenOpensAfterARestartForItsOrganizationOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.sqlite3")
	s := testStore(t, path)
	dana := person{id: "1", name: "dana@example.com"}
	if _, err := s.saveConnection("7", "bitbucket", "bitbucket", "first-token", dana, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.saveConnection("7", "bitbucket", "bitbucket", "second-token", dana, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.close()

	s = testStore(t, path)
	if token, err := s.connectionToken("7", "bitbucket"); err != nil || token != "second-token" {
		t.Errorf("the token is %q, %v; want second-token", token, err)
	}
	if _, err := s.connectionToken("8", "bitbucket"); err != errNotFound {
		t.Errorf("another Organization's read: %v, want not found", err)
	}
}

// A sealed token that is copied to another connection's row does not
// open there.
func TestASealedTokenDoesNotOpenInAnotherRow(t *testing.T) {
	s := testStore(t, filepath.Join(t.TempDir(), "d.sqlite3"))
	dana := person{id: "1", name: "dana@example.com"}
	for _, name := range []string{"one", "two"} {
		if _, err := s.saveConnection("7", name, "bitbucket", "token-of-"+name, dana, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`UPDATE connections SET token = (SELECT token FROM connections WHERE name = 'one')
		WHERE name = 'two'`); err != nil {
		t.Fatal(err)
	}
	if token, err := s.connectionToken("7", "two"); err == nil {
		t.Errorf("a copied token opened as %q", token)
	}
}
