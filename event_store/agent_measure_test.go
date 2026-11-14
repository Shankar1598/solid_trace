package main_test

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/storage"
)

// The measured load of Agent sessions (spec, Storage), on real Claude Code
// transcripts, as in the prototype. Real transcripts are not in the
// repository, so the test runs only when SOLIDTRACE_AGENT_TRANSCRIPTS names
// a directory of them, as ~/.claude/projects/<project>, for example:
//
//	SOLIDTRACE_AGENT_TRANSCRIPTS=~/.claude/projects/<project> go test -run TestTheMeasuredLoad .
//
// SOLIDTRACE_AGENT_TRANSCRIPTS_MB is how many MB of .jsonl files it reads
// (default 300), so that the run takes less than a minute.
func TestTheMeasuredLoadOfAgentSessionsOnRealTranscripts(t *testing.T) {
	root := os.Getenv("SOLIDTRACE_AGENT_TRANSCRIPTS")
	if root == "" {
		t.Skip("SOLIDTRACE_AGENT_TRANSCRIPTS names no directory of real transcripts")
	}
	limit := int64(300)
	if mb := os.Getenv("SOLIDTRACE_AGENT_TRANSCRIPTS_MB"); mb != "" {
		var err error
		if limit, err = strconv.ParseInt(mb, 10, 64); err != nil {
			t.Fatalf("SOLIDTRACE_AGENT_TRANSCRIPTS_MB: %v", err)
		}
	}
	limit <<= 20

	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	projectID := uint32(o.project)

	// Each .jsonl file of an Agent session, in units of whole lines.
	var raw int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") || raw >= limit {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		sessionID, err := uuid.Parse(strings.TrimSuffix(strings.SplitN(rel, string(filepath.Separator), 2)[0], ".jsonl"))
		if err != nil {
			return nil // not an Agent session
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		data = data[:bytes.LastIndexByte(data, '\n')+1] // a file still being written
		raw += int64(len(data))
		for offset := 0; offset < len(data); {
			n := min(len(data)-offset, 8<<20)
			if cut := bytes.LastIndexByte(data[offset:offset+n], '\n'); cut >= 0 {
				n = cut + 1
			} else {
				n = bytes.IndexByte(data[offset:], '\n') + 1
			}
			unit := storage.AgentFileUnit{SessionID: sessionID, Path: filepath.ToSlash(rel), Version: 1, Offset: uint64(offset),
				Bytes: data[offset : offset+n], StoredAt: env.Clock.Now(),
				Session: storage.AgentSession{UserID: o.user, Harness: "claude-code", Repository: "github.com/acme/shop"}}
			if _, _, err := env.PebbleWriter.WriteAgentFileUnit(projectID, storage.AgentUpload{Unit: unit, AnyPerson: true}); err != nil {
				return err
			}
			offset += n
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Read the transcripts: %v", err)
	}
	if raw == 0 {
		t.Fatalf("No .jsonl file of an Agent session in %s", root)
	}

	env.AgentFileProcessor.Notify(projectID)
	if err := env.AgentFileProcessor.ProcessUntilCaughtUp(); err != nil {
		t.Fatal(err)
	}
	start, began := cpuTime(t), time.Now()
	if err := env.AgentParser.ProcessUntilCaughtUp(); err != nil {
		t.Fatal(err)
	}
	cpu, wall := cpuTime(t)-start, time.Since(began)

	var stored, logs uint64
	ctx := context.Background()
	if err := env.ClickHouse.Conn().QueryRow(ctx, `SELECT sum(bytes_on_disk) FROM system.parts
		WHERE database = currentDatabase() AND active AND table = 'agent_session_files'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if err := env.ClickHouse.Conn().QueryRow(ctx, "SELECT count() FROM logs WHERE project_id = ?", projectID).Scan(&logs); err != nil {
		t.Fatal(err)
	}
	gb := float64(raw) / 1e9
	ratio := float64(raw) / float64(stored)
	perGB := cpu.Seconds() / gb
	t.Logf("%.1f MB of .jsonl: raw layer %.1f MB (%.2f times smaller); %d Logs; the parse took %.1f CPU s (%.1f CPU s a GB), %.1f s",
		float64(raw)/1e6, float64(stored)/1e6, ratio, logs, cpu.Seconds(), perGB, wall.Seconds())
	// The spec's measured load: 6.6 times compression on the full set (5.4
	// on a sample with more of one large Agent session), and 12.5 to 17.3
	// CPU seconds a GB for the parse.
	if ratio < 5 {
		t.Errorf("Expected the raw layer about 6 times smaller than the transcripts, got %.2f times", ratio)
	}
	if perGB > 20 {
		t.Errorf("Expected the parse to use less than about 20 CPU seconds a GB, got %.1f", perGB)
	}
}

// cpuTime is the CPU time that this process has used, user and system.
func cpuTime(t *testing.T) time.Duration {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatal(err)
	}
	return time.Duration(usage.Utime.Nano() + usage.Stime.Nano())
}
