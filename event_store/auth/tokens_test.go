package auth

import (
	"testing"
	"time"
)

// Console's factory token (Oauth::ConsoleToken::PREFIX "stc_") is for the
// Dispatcher only. Check refuses it, so it cannot be used on the agent ingest
// API. If Console's prefix changes, or Check reads a secret with another
// prefix, this test must still hold.
func TestCheckRefusesConsolesFactoryToken(t *testing.T) {
	tokens := &Tokens{} // a secret with no known prefix does not read the database

	for _, secret := range []string{"stc_eyJfcmFpbHMiOnsiZGF0YSI6eyJ1c2VyX2lkIjoxfX19--abc", "stc_"} {
		token, ok, err := tokens.Check(secret, time.Now())
		if ok || err != nil || token.ID != 0 {
			t.Errorf("Check(%q) = %+v, %v, %v; want no token, false, nil", secret, token, ok, err)
		}
	}
}
