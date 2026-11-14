package capture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/solidtrace/cli/agentapi"
)

// UnitLimit is the largest unit: a longer line or file is sent as its
// first UnitLimit bytes with its full size.
const UnitLimit = 32 << 20

// state is what capture keeps of an Agent session between calls: each
// file that is not .jsonl as it was at the last attempt.
type state struct {
	Seen map[string]seenFile `json:"seen"`
}

type seenFile struct {
	Size    int64  `json:"size"`
	ModTime int64  `json:"mtime_ns"`
	Hash    string `json:"hash"`
}

type upload struct {
	c    *Capturer
	in   Input
	repo Repository
	// root is the Claude project directory: the paths are relative to it.
	root   string
	server map[string]agentapi.AgentSessionFile
	// settledBefore: a file that did not change since then is sent whole
	// (the sweep). Zero in a hook's call.
	settledBefore time.Time
	// pending is true when a file is left for a later call.
	pending bool
}

func (u *upload) run(ctx context.Context) error {
	files, err := u.c.API.AgentSessionFiles(ctx, u.in.SessionID)
	if err != nil {
		return fmt.Errorf("ask what the server has: %w", err)
	}
	u.server = map[string]agentapi.AgentSessionFile{}
	for _, f := range files {
		u.server[f.Path] = f
	}
	paths, err := u.paths()
	if err != nil {
		return err
	}
	previous := u.readState()
	next := state{Seen: map[string]seenFile{}}
	var failed error
	for _, path := range paths {
		var err error
		if strings.HasSuffix(path, ".jsonl") {
			err = u.lines(ctx, path)
		} else {
			err = u.whole(ctx, path, previous, next)
		}
		var answer *agentapi.Error
		if errors.As(err, &answer) && (answer.Status == http.StatusConflict || answer.Status == http.StatusBadRequest) {
			// The server has other bytes than the offsets call said (the
			// next call asks again), or refuses this path: the other paths
			// still go.
			u.c.Log("%s: %s: %v", u.in.SessionID, path, err)
			u.pending = true
			continue
		}
		if err != nil {
			failed = fmt.Errorf("upload %s: %w", path, err)
			break
		}
	}
	if err := u.writeState(next, previous, failed != nil); err != nil {
		u.c.Log("%s: keep the state: %v", u.in.SessionID, err)
	}
	return failed
}

// paths are the Agent session's files: <id>.jsonl and each regular file
// under <id>/ (subagents, workflow agents, tool results), relative to the
// project directory, in a fixed order.
func (u *upload) paths() ([]string, error) {
	paths := []string{}
	if info, err := os.Lstat(filepath.Join(u.root, u.in.SessionID+".jsonl")); err == nil && info.Mode().IsRegular() {
		paths = append(paths, u.in.SessionID+".jsonl")
	}
	dir := filepath.Join(u.root, u.in.SessionID)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir && errors.Is(err, fs.ErrNotExist) {
				return filepath.SkipDir
			}
			return err
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(u.root, p)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read the Agent session directory: %w", err)
	}
	// WalkDir gives the files under <id>/ in lexical order.
	return paths, nil
}

func (u *upload) send(ctx context.Context, unit agentapi.Unit) (agentapi.Uploaded, error) {
	unit.Harness = Harness
	unit.Repository = u.repo.Name
	unit.Folder = u.repo.Folder
	return u.c.API.Upload(ctx, u.in.SessionID, unit)
}

// lines sends the whole lines of a .jsonl file after the server's end
// offset. A file shorter than that end was trimmed: it goes again from
// offset 0 as a new version.
func (u *upload) lines(ctx context.Context, path string) error {
	f, err := os.Open(filepath.Join(u.root, filepath.FromSlash(path)))
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := uint64(info.Size())
	version, offset := uint32(1), uint64(0)
	if have, ok := u.server[path]; ok {
		version, offset = have.Version, have.EndOffset
		if size < offset {
			u.c.Log("%s: %s is shorter than the server's %d bytes: it goes again as version %d", u.in.SessionID, path,
				offset, version+1)
			version, offset = version+1, 0
		}
	}
	for offset < size {
		body, unitSize, err := nextLines(f, offset, size)
		if err != nil || body == nil {
			return err
		}
		answer, err := u.send(ctx, agentapi.Unit{Path: path, Version: version, Offset: offset, Size: unitSize, Bytes: body})
		if err != nil {
			return err
		}
		version, offset = answer.Version, answer.EndOffset
	}
	return nil
}

// nextLines reads the whole lines from offset, at most UnitLimit bytes.
// A line longer than UnitLimit gives its first UnitLimit bytes and its
// full size; else size is 0. A line with no newline yet gives nil.
func nextLines(f *os.File, offset, end uint64) (body []byte, size uint64, err error) {
	buf := make([]byte, min(end-offset, UnitLimit))
	if _, err := f.ReadAt(buf, int64(offset)); err != nil {
		return nil, 0, err
	}
	if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
		return buf[:i+1], 0, nil
	}
	if len(buf) < UnitLimit {
		return nil, 0, nil
	}
	// One line over UnitLimit: find its end.
	chunk := make([]byte, 1<<20)
	for at := offset + UnitLimit; at < end; {
		n, err := f.ReadAt(chunk[:min(uint64(len(chunk)), end-at)], int64(at))
		if i := bytes.IndexByte(chunk[:n], '\n'); i >= 0 {
			return buf, at + uint64(i) + 1 - offset, nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, 0, err
		}
		if n == 0 {
			break
		}
		at += uint64(n)
	}
	return nil, 0, nil
}

// whole sends a file that is not .jsonl with its hash, when the server
// does not have that hash, and the file did not change since the last
// attempt or the Agent session ends. A new hash is a new version.
func (u *upload) whole(ctx context.Context, path string, previous, next state) error {
	full := filepath.Join(u.root, filepath.FromSlash(path))
	f, err := os.Open(full)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return nil
	}
	seen := seenFile{Size: info.Size(), ModTime: info.ModTime().UnixNano()}
	if before, ok := previous.Seen[path]; ok && before.Size == seen.Size && before.ModTime == seen.ModTime {
		seen.Hash = before.Hash
	} else if seen.Hash, err = hashOf(f); err != nil {
		return err
	}
	next.Seen[path] = seen
	have, known := u.server[path]
	if known && have.ContentHash == seen.Hash {
		return nil
	}
	settled := !u.settledBefore.IsZero() && info.ModTime().Before(u.settledBefore)
	if previous.Seen[path] != seen && u.in.HookEventName != SessionEnd && !settled {
		// It may still change: it goes at the next call if it does not.
		u.pending = true
		return nil
	}
	body := make([]byte, min(info.Size(), UnitLimit))
	if _, err := f.ReadAt(body, 0); err != nil {
		return err
	}
	unit := agentapi.Unit{Path: path, Version: 1, ContentHash: seen.Hash, Bytes: body}
	if known {
		unit.Version = have.Version + 1
	}
	if info.Size() > UnitLimit {
		unit.Size = uint64(info.Size())
	}
	_, err = u.send(ctx, unit)
	return err
}

func hashOf(f *os.File) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(f, 0, 1<<62)); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func (u *upload) readState() state {
	s := state{Seen: map[string]seenFile{}}
	data, err := os.ReadFile(u.c.statePath(u.in.SessionID))
	if err == nil {
		_ = json.Unmarshal(data, &s)
	}
	if s.Seen == nil {
		s.Seen = map[string]seenFile{}
	}
	return s
}

// writeState keeps what this call saw. A call that stopped early keeps
// what it did not reach as the previous call saw it.
func (u *upload) writeState(next, previous state, stopped bool) error {
	if stopped {
		for path, seen := range previous.Seen {
			if _, ok := next.Seen[path]; !ok {
				next.Seen[path] = seen
			}
		}
	}
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	return writeFile(u.c.statePath(u.in.SessionID), data)
}
