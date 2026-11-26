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
	"syscall"
	"time"

	"github.com/solidtrace/cli/agentapi"
)

// WrittenWholeFolder is the folder under an Agent session's own folder
// where solidtrace writes each file with a rename: the capture sends such a
// file at once, as it is never half written.
const WrittenWholeFolder = "solidtrace"

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
	root string
	// dir is the project directory of a Run's upload: each read goes
	// through it (RunUpload.Dir).
	dir    *os.Root
	server map[string]agentapi.AgentSessionFile
	// settledBefore: a file that did not change since then is sent whole
	// (the sweep). Zero in a hook's call.
	settledBefore time.Time
	// pending is true when a file is left for a later call.
	pending bool
	// forRun is set for a Run's upload (UploadRun).
	forRun *RunUpload
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
		if errors.Is(err, errNotRegular) ||
			errors.As(err, &answer) && (answer.Status == http.StatusConflict || answer.Status == http.StatusBadRequest) {
			// The server has other bytes than the offsets call said (the
			// next call asks again), or refuses this path, or a Run's file
			// is not a regular file now: the other paths still go.
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
	lstat := func(name string) (fs.FileInfo, error) { return os.Lstat(filepath.Join(u.root, name)) }
	if u.dir != nil {
		lstat = u.dir.Lstat
	}
	if info, err := lstat(u.in.SessionID + ".jsonl"); err == nil && info.Mode().IsRegular() {
		paths = append(paths, u.in.SessionID+".jsonl")
	}
	dirs := []string{u.in.SessionID}
	if u.forRun != nil {
		dirs = append(dirs, u.forRun.Extra...)
	}
	for _, dir := range dirs {
		var err error
		if paths, err = u.walk(paths, dir); err != nil {
			return nil, err
		}
	}
	// WalkDir gives the files under each directory in lexical order.
	return paths, nil
}

// walk adds each regular file under dir, a directory of the project
// directory, relative to the project directory. A dir that is not there
// adds nothing. A Run's upload walks through u.dir, so that no link takes
// the walk out of the project directory.
func (u *upload) walk(paths []string, dir string) ([]string, error) {
	walk, rel := filepath.WalkDir, func(p string) (string, error) { return filepath.Rel(u.root, p) }
	if u.dir != nil {
		walk = func(root string, fn fs.WalkDirFunc) error { return fs.WalkDir(u.dir.FS(), root, fn) }
		rel = func(p string) (string, error) { return p, nil }
	} else {
		dir = filepath.Join(u.root, dir)
	}
	err := walk(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipDir
			}
			return err
		}
		if d.Type().IsRegular() {
			r, err := rel(p)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(r))
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read the Agent session directory: %w", err)
	}
	return paths, nil
}

func (u *upload) send(ctx context.Context, unit agentapi.Unit) (agentapi.Uploaded, error) {
	unit.Harness = Harness
	unit.Repository = u.repo.Name
	unit.Folder = u.repo.Folder
	if u.forRun != nil {
		unit.RunID, unit.Project = u.forRun.RunID, u.forRun.Project
	}
	return u.c.API.Upload(ctx, u.in.SessionID, unit)
}

// lines sends the whole lines of a .jsonl file after the server's end
// offset. A file shorter than that end was trimmed: it goes again from
// offset 0 as a new version.
func (u *upload) lines(ctx context.Context, path string) error {
	f, err := u.open(path)
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
		body, unitSize, err := nextLines(ctx, f, offset, size)
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
// full size; else size is 0. A line with no newline yet gives nil. The
// search for the end of a long line stops when ctx ends.
func nextLines(ctx context.Context, f *os.File, offset, end uint64) (body []byte, size uint64, err error) {
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
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
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
// attempt or the Agent session ends. A file under
// <id>/WrittenWholeFolder/ goes at once. A new hash is a new version.
func (u *upload) whole(ctx context.Context, path string, previous, next state) error {
	f, err := u.open(path)
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
	} else if seen.Hash, err = hashOf(ctx, f); err != nil {
		return err
	}
	next.Seen[path] = seen
	have, known := u.server[path]
	if known && have.ContentHash == seen.Hash {
		return nil
	}
	settled := !u.settledBefore.IsZero() && info.ModTime().Before(u.settledBefore)
	atOnce := strings.HasPrefix(path, u.in.SessionID+"/"+WrittenWholeFolder+"/")
	if previous.Seen[path] != seen && u.in.HookEventName != SessionEnd && !settled && !atOnce {
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

// errNotRegular is a path of a Run's Agent session that is not a regular
// file when it is open.
var errNotRegular = errors.New("not a regular file")

// open opens a path of the Agent session. A Run's upload opens it inside
// the project directory only, and only a regular file: the agent makes
// these files, and can swap one for a symbolic link to a file outside the
// directory, or for a FIFO, after the walk. O_NONBLOCK stops the open of
// a FIFO with no writer from waiting.
func (u *upload) open(path string) (*os.File, error) {
	if u.forRun == nil {
		return os.Open(filepath.Join(u.root, filepath.FromSlash(path)))
	}
	f, err := u.dir.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		f.Close()
		if err == nil {
			err = fmt.Errorf("%s: %w", path, errNotRegular)
		}
		return nil, err
	}
	return f, nil
}

// hashOf is the content hash of f. It stops when ctx ends.
func hashOf(ctx context.Context, f *os.File) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, ctxReader{ctx, io.NewSectionReader(f, 0, 1<<62)}); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// ctxReader stops a read when its context ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
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
