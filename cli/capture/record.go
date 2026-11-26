package capture

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Status is what the last capture of an Agent session did.
type Status string

const (
	// Uploaded: the server has each byte that the last call could send.
	Uploaded Status = "uploaded"
	// NotInList: the Agent session's repository is not a captured
	// repository, so no byte of it left the machine.
	NotInList Status = "not in the list"
	// NotYours: SolidTrace has the Agent session as another person's or a
	// Run's (a transcript that resume downloaded), so no byte of it left
	// the machine.
	NotYours Status = "not yours"
	// Failed: the last call stopped before it sent each byte.
	Failed Status = "failed"
)

// Record is the result of the last capture of one Agent session, for
// 'capture status' and for the sweep.
type Record struct {
	Status     Status    `json:"status"`
	At         time.Time `json:"at"`
	Repository string    `json:"repository,omitempty"`
	StartDir   string    `json:"start_dir,omitempty"`
	Error      string    `json:"error,omitempty"`
	// Fingerprint is the files as the uploaded call found them. The sweep
	// does not ask the server again while they stay the same.
	Fingerprint string `json:"fingerprint,omitempty"`
	// Pending is true when the call left a file for a later call: a file
	// that still changed, or a path that the server refused.
	Pending bool `json:"pending,omitempty"`
}

func (c *Capturer) recordPath(sessionID string) string {
	return filepath.Join(c.Dir, "capture", sessionID+".status.json")
}

// RecordOf is the record of the last capture of an Agent session; ok is
// false when no capture looked at it. Then r can still have the StartDir
// that SetStartDir kept.
func RecordOf(dir, sessionID string) (r Record, ok bool) {
	data, err := os.ReadFile((&Capturer{Dir: dir}).recordPath(sessionID))
	if err != nil || json.Unmarshal(data, &r) != nil {
		return Record{}, false
	}
	return r, r.Status != ""
}

// SetStartDir keeps startDir as the start directory in the record of an
// Agent session, with the other fields of the record. resume calls it for
// a transcript that it downloaded: the first cwd of the transcript can be
// a folder of another machine, and the sweep has no hook cwd.
func SetStartDir(dir, sessionID, startDir string) error {
	c := &Capturer{Dir: dir}
	r, _ := RecordOf(dir, sessionID)
	r.StartDir = startDir
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return writeFile(c.recordPath(sessionID), data)
}

func (c *Capturer) record(sessionID string, r Record) {
	r.At = c.Now()
	data, err := json.Marshal(r)
	if err == nil {
		err = writeFile(c.recordPath(sessionID), data)
	}
	if err != nil {
		c.Log("%s: keep the status: %v", sessionID, err)
	}
}

func (c *Capturer) recordFailed(sessionID string, r Record, err error) {
	r.Status, r.Error = Failed, err.Error()
	c.record(sessionID, r)
}

// fingerprint sums up the files of an Agent session: their number, their
// sizes and the latest change time.
func fingerprint(root, sessionID string) string {
	var n, size, latest int64
	add := func(info fs.FileInfo) {
		if info.Mode().IsRegular() {
			n++
			size += info.Size()
			latest = max(latest, info.ModTime().UnixNano())
		}
	}
	if info, err := os.Lstat(filepath.Join(root, sessionID+".jsonl")); err == nil {
		add(info)
	}
	_ = filepath.WalkDir(filepath.Join(root, sessionID), func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil {
			add(info)
		}
		return nil
	})
	return fmt.Sprintf("%d:%d:%d", n, size, latest)
}

// FoundSession is an Agent session on the machine.
type FoundSession struct {
	ID             string
	TranscriptPath string
	ModTime        time.Time
}

// Sessions are the Agent sessions under Claude Code's projects directory:
// each <id>.jsonl in a project directory.
func Sessions(projectsDir string) ([]FoundSession, error) {
	projects, err := os.ReadDir(projectsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var found []FoundSession
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		dir := filepath.Join(projectsDir, project.Name())
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			id, ok := strings.CutSuffix(e.Name(), ".jsonl")
			if !ok || !sessionIDShape.MatchString(id) || !e.Type().IsRegular() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			found = append(found, FoundSession{ID: id, TranscriptPath: filepath.Join(dir, e.Name()), ModTime: info.ModTime()})
		}
	}
	return found, nil
}
