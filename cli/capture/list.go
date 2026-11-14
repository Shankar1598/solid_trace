package capture

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// ListFile is the local cache of the captured repositories list in the
// config directory.
const ListFile = "captured-repositories.json"

type cachedList struct {
	Signin       string    `json:"signin"`
	FetchedAt    time.Time `json:"fetched_at"`
	Organization string    `json:"organization"`
	Repositories []string  `json:"captured_repositories"`
}

// list is the captured repositories list: the cached one while it is
// younger than ListLasts and of the same sign-in, else a new one from the
// server. With no list in hand it is an error, and nothing is sent.
func (c *Capturer) list(ctx context.Context) ([]string, error) {
	path := filepath.Join(c.Dir, ListFile)
	now := c.Now()
	if data, err := os.ReadFile(path); err == nil {
		var cached cachedList
		if json.Unmarshal(data, &cached) == nil && cached.Signin == c.Signin && cached.Repositories != nil {
			if age := now.Sub(cached.FetchedAt); age >= 0 && age < ListLasts {
				return cached.Repositories, nil
			}
		}
	}
	answer, err := c.API.CapturedRepositories(ctx)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(cachedList{Signin: c.Signin, FetchedAt: now, Organization: answer.Organization.Slug,
		Repositories: answer.Repositories})
	if err == nil {
		if err := writeFile(path, data); err != nil {
			c.Log("keep the captured repositories list: %v", err)
		}
	}
	return answer.Repositories, nil
}

// CachedList is the list kept on the machine for the sign-in: the
// Organization's slug, the entries and when they came. ok is false when
// none is kept.
func CachedList(dir, signin string) (organization string, entries []string, fetchedAt time.Time, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, ListFile))
	var cached cachedList
	if err != nil || json.Unmarshal(data, &cached) != nil || cached.Signin != signin || cached.Repositories == nil {
		return "", nil, time.Time{}, false
	}
	return cached.Organization, cached.Repositories, cached.FetchedAt, true
}

// writeFile replaces a file in one step, with mode 0600.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
