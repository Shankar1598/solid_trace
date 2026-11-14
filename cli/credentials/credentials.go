// Package credentials keeps the CLI's sign-in: the instance, the ingest URL
// and the OAuth tokens, in one file that only the person's own user can
// read and write (mode 0600, in a directory with mode 0700).
//
// A refresh token works once, so two processes must not refresh with the
// same one: Lock gives one process at a time the right to refresh, and a
// Save replaces the file in one step (a rename), so a reader never sees
// half a file and the old refresh token is never kept beside the new one.
package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// FileName is the name of the file in the config directory.
const FileName = "credentials.json"

const lockName = "credentials.lock"

// ErrNotSignedIn is the error of Load when there is no sign-in.
var ErrNotSignedIn = errors.New("not signed in: run 'solidtrace login <Console URL>'")

// Organization is the Organization that the tokens act in.
type Organization struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// Credentials is one sign-in.
type Credentials struct {
	// InstanceURL is the Console URL, the OAuth issuer.
	InstanceURL string `json:"instance_url"`
	// IngestURL is where the agent ingest API is.
	IngestURL     string `json:"ingest_url"`
	TokenEndpoint string `json:"token_endpoint"`
	ClientID      string `json:"client_id"`
	// Resource is the resource that the tokens are for.
	Resource             string        `json:"resource"`
	Scope                string        `json:"scope,omitempty"`
	AccessToken          string        `json:"access_token,omitempty"`
	AccessTokenExpiresAt time.Time     `json:"access_token_expires_at,omitzero"`
	RefreshToken         string        `json:"refresh_token,omitempty"`
	Organization         *Organization `json:"organization,omitempty"`
}

// SignedIn is true when the credentials hold a refresh token.
func (c Credentials) SignedIn() bool {
	return c.RefreshToken != ""
}

// Store is the credentials file in a directory.
type Store struct {
	Dir string
}

// Path is the credentials file.
func (s Store) Path() string {
	return filepath.Join(s.Dir, FileName)
}

// Load reads the credentials. It refuses a file that another user can
// read or write: the tokens act as the person.
func (s Store) Load() (Credentials, error) {
	var c Credentials
	f, err := os.Open(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		return c, ErrNotSignedIn
	}
	if err != nil {
		return c, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return c, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return c, fmt.Errorf("%s can be read or written by other users (mode %o): remove it and run 'solidtrace login' again",
			s.Path(), info.Mode().Perm())
	}
	if err := json.NewDecoder(f).Decode(&c); err != nil {
		return c, fmt.Errorf("read %s: %w: run 'solidtrace login' again", s.Path(), err)
	}
	return c, nil
}

// Save writes the credentials in place of the file, in one step.
func (s Store) Save(c Credentials) error {
	if err := s.makeDir(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// CreateTemp makes the file with mode 0600, whatever the umask.
	tmp, err := os.CreateTemp(s.Dir, ".credentials-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.Path())
}

// Lock waits until this process alone holds the credentials, then returns
// the function that lets them go. It stops waiting when ctx ends.
func (s Store) Lock(ctx context.Context) (func(), error) {
	if err := s.makeDir(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			f.Close()
			return nil, fmt.Errorf("lock %s: %w", f.Name(), err)
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// makeDir makes the directory with mode 0700. A directory that exists
// keeps its mode: the file's own mode protects the tokens.
func (s Store) makeDir() error {
	if s.Dir == "" {
		return errors.New("no config directory")
	}
	return os.MkdirAll(s.Dir, 0o700)
}
