package handler

import (
	"archive/tar"
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// ScopeSessionsRead lets a token download the Agent sessions in its list
// of Agent session ids. It always has a list.
const ScopeSessionsRead = "sessions:read"

// ScopeSessionsDownload lets a token download, and read the files call of,
// each Agent session of its Organization's Agent Project: also another
// person's and a Run's. It writes nothing. It gives no new access, as each
// member reads these Agent sessions in Console.
const ScopeSessionsDownload = "sessions:download"

// agentDownloadTimeout is the longest time that one download reads. The
// prototype's largest Agent session (434 MB) streams in about 2 seconds
// on the server; this lets a slow client have 1 MB a second.
const agentDownloadTimeout = 10 * time.Minute

// agentDownloadsAtOnce is the most downloads by people's tokens that read
// at the same time, and each person has at most 1 of them. Each download
// keeps a ClickHouse connection of the pool until its stream ends, at the
// pace of its client. Without a limit, slow downloads can take the whole
// pool, and then /health and ingest fail. Without the limit of 1 for each
// person, one person's slow downloads can take all of them, and each
// other person's resume fails. A Run's token is not in the limit: a
// member's slow downloads must not stop the restores of Runs, and a Run
// downloads only its own Agent sessions.
const agentDownloadsAtOnce = 2

// agentDownloadRetryAfter is the Retry-After, in seconds, of a download
// that the limit refuses.
const agentDownloadRetryAfter = "5"

// AgentSessionFlagsHeader carries the flags of the downloaded Agent
// session: cut, partial and trimmed. A resume of a cut or partial Agent
// session can fail.
const AgentSessionFlagsHeader = "SolidTrace-Agent-Session-Flags"

// The PAX records of each file in a download's tar archive.
const (
	// paxVersion is the version of the path in the archive: its highest.
	paxVersion = "SOLIDTRACE.version"
	// paxStartOffset is the offset in the file of the first byte in the
	// archive: more than 0 when the file is partial.
	paxStartOffset = "SOLIDTRACE.start_offset"
	// paxEndOffset is the end of the file on the client's disk.
	paxEndOffset = "SOLIDTRACE.end_offset"
	// paxFlags are the file's flags, between commas: partial (no start)
	// and cut (missing ranges). No record when it has none.
	paxFlags = "SOLIDTRACE.flags"
	// paxMissing are the ranges after the start with no bytes in the
	// archive, as from-to between commas, to not included: the rest of
	// each cut unit. No record when there is none.
	paxMissing = "SOLIDTRACE.missing"
)

// AgentDownloads reads the stored bytes of Agent sessions
// (*storage.ClickHouse).
type AgentDownloads interface {
	AgentDownloadFiles(ctx context.Context, projectID uint32, sessionID uuid.UUID) ([]storage.AgentDownloadFile, error)
	ReadAgentDownload(ctx context.Context, projectID uint32, sessionID uuid.UUID, files []storage.AgentDownloadFile, write func(file int, bytes []byte) error) error
}

// DownloadAgentSession answers the Agent session directory as a tar
// archive, for a resume on another machine: for each path its highest
// version, as the server stored it (after Redaction). Only a token with
// sessions:read that lists the Agent session, or with sessions:download,
// gets it. A file whose start Retention dropped is marked partial with the
// offset where it starts; a file with a cut unit is marked cut with its
// missing ranges. The server does not promise a resume from a partial or
// cut file.
//
// Until Agent file processing has inserted each stored unit of the Agent
// session, it answers 503 with Retry-After: a 200 of an upload means that
// the bytes are stored, and the download must not leave them out. A read
// that fails after the first byte stops the answer with no end, so the
// client sees an error and not a short archive. When agentDownloadsAtOnce
// downloads by people's tokens read, or a download by the same person
// reads, it answers 503 with Retry-After to a person's token.
func (h *AgentHandler) DownloadAgentSession(c *fiber.Ctx) error {
	token := agentToken(c)
	if token.AgentSessionIDs == nil && !token.Has(ScopeSessionsDownload) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "A download needs a token with sessions:download, or with sessions:read for a list of Agent session ids, and this token has neither."})
	}
	projectID, sessionID, ok, err := h.agentSessionOf(c, token)
	if !ok {
		return err
	}
	session, known, states, err := h.files.AgentSessionFiles(projectID, sessionID)
	if err != nil {
		logger.L.Error("Failed to read an Agent session's files", "project_id", projectID, "session_id", sessionID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The Agent session could not be read. Try again later."})
	}
	if !known {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": fmt.Sprintf("The server has no Agent session %s.", sessionID)})
	}
	inserted, err := h.files.AgentFilesInserted(projectID, states)
	if err != nil {
		logger.L.Error("Failed to read Agent file processing's cursor", "project_id", projectID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The Agent session could not be read. Try again later."})
	}
	if !inserted {
		c.Set(fiber.HeaderRetryAfter, "1")
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "The last uploads of this Agent session are stored but not yet ready to download. Try again in a second."})
	}
	release := func() {}
	// A Run's child token is not in the limit, as before people could
	// download. A person's token with a list of ids is in it.
	if !token.Child {
		var refusal string
		if release, refusal = h.reading.take(token.UserID); refusal != "" {
			c.Set(fiber.HeaderRetryAfter, agentDownloadRetryAfter)
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": refusal})
		}
	}
	files, err := h.downloads.AgentDownloadFiles(c.UserContext(), projectID, sessionID)
	if err != nil {
		release()
		logger.L.Error("Failed to read an Agent session's files", "project_id", projectID, "session_id", sessionID, "error", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "The Agent session could not be read. Try again later."})
	}
	files = withDroppedFiles(files, states)

	flags := storage.AgentFlagNames(session.Flags)
	for _, f := range files {
		if f.Partial() {
			flags = append(flags, "partial")
			break
		}
	}
	c.Set(fiber.HeaderContentType, "application/x-tar")
	c.Set(AgentSessionFlagsHeader, strings.Join(flags, ", "))

	reader, writer := io.Pipe()
	go h.sendDownload(writer, agentDownloadTimeout, release, projectID, sessionID, files)
	c.Context().SetBodyStream(reader, -1)
	return nil
}

// downloadsBusy starts each refusal of downloadLimit. The solidtrace CLI
// tells it from the 503 of uploads that are not ready by this start.
const downloadsBusy = "The server sends as many downloads as it can now."

// downloadLimit is the set of people (user ids) with a download that
// reads now. As each person has at most 1, its size is the number of
// downloads that read.
type downloadLimit struct {
	mu     sync.Mutex
	people map[int64]struct{}
}

// take adds user to the set and gives the release that removes it. When
// user already has a download that reads, or agentDownloadsAtOnce people
// have one, it adds nothing and gives the refusal.
func (l *downloadLimit) take(user int64) (release func(), refusal string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.people[user]; ok {
		return nil, downloadsBusy + " It sends 1 download at a time to each person, and another download " +
			"of yours reads now. Try again when it ends."
	}
	if len(l.people) >= agentDownloadsAtOnce {
		return nil, downloadsBusy + " Try again in a few seconds."
	}
	l.people[user] = struct{}{}
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.people, user)
	}, ""
}

// sendDownload writes the tar archive of files to w and then closes w,
// with the error if there is one. The close of the reader (fasthttp closes
// it at the end of the answer, also when the client goes away) makes the
// next write fail: the read stops, and so does its ClickHouse query. A
// client that stops reading but keeps its connection open must not keep
// the query or this goroutine, because the ingest app has no write
// timeout: after timeout, the close of w stops a write that waits for the
// client. When the read ends, it calls release, before the close of w.
func (h *AgentHandler) sendDownload(w *io.PipeWriter, timeout time.Duration, release func(), projectID uint32, sessionID uuid.UUID, files []storage.AgentDownloadFile) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { w.CloseWithError(ctx.Err()) })
	defer stop()
	err := h.writeDownload(ctx, w, projectID, sessionID, files)
	release()
	if err != nil {
		logger.L.Error("Failed to send an Agent session's download", "project_id", projectID, "session_id", sessionID, "error", err)
	}
	w.CloseWithError(err)
}

// writeDownload writes the tar archive of files to w. On an error it
// writes no end of the archive.
func (h *AgentHandler) writeDownload(ctx context.Context, w io.Writer, projectID uint32, sessionID uuid.UUID, files []storage.AgentDownloadFile) error {
	buffered := bufio.NewWriterSize(w, 256<<10)
	archive := tar.NewWriter(buffered)
	next := 0
	headersUpTo := func(file int) error {
		for ; next <= file; next++ {
			if err := archive.WriteHeader(downloadHeader(files[next])); err != nil {
				return err
			}
		}
		return nil
	}
	err := h.downloads.ReadAgentDownload(ctx, projectID, sessionID, files, func(file int, bytes []byte) error {
		if err := headersUpTo(file); err != nil {
			return err
		}
		_, err := archive.Write(bytes)
		return err
	})
	if err != nil {
		return err
	}
	if err := headersUpTo(len(files) - 1); err != nil {
		return err
	}
	if err := archive.Close(); err != nil {
		return err
	}
	return buffered.Flush()
}

func downloadHeader(f storage.AgentDownloadFile) *tar.Header {
	records := map[string]string{
		paxVersion:     strconv.FormatUint(uint64(f.Version), 10),
		paxStartOffset: strconv.FormatUint(f.StartOffset, 10),
		paxEndOffset:   strconv.FormatUint(f.EndOffset, 10),
	}
	var flags, missing []string
	if f.Partial() {
		flags = append(flags, "partial")
	}
	if len(f.Missing) > 0 {
		flags = append(flags, "cut")
		for _, r := range f.Missing {
			missing = append(missing, fmt.Sprintf("%d-%d", r.From, r.To))
		}
		records[paxMissing] = strings.Join(missing, ",")
	}
	if len(flags) > 0 {
		records[paxFlags] = strings.Join(flags, ",")
	}
	return &tar.Header{
		Typeflag:   tar.TypeReg,
		Name:       f.Path,
		Size:       int64(f.Size),
		Mode:       0o600,
		ModTime:    time.Unix(0, 0),
		Format:     tar.FormatPAX,
		PAXRecords: records,
	}
}

// withDroppedFiles adds to files, which ClickHouse holds, each path that
// the server has a state of but no rows: Retention dropped all of it. Such
// a file is in the archive with no bytes, partial at its end offset. Both
// lists are in path order, and so is the result.
func withDroppedFiles(files []storage.AgentDownloadFile, states []storage.AgentFileState) []storage.AgentDownloadFile {
	held := make(map[string]bool, len(files))
	for _, f := range files {
		held[f.Path] = true
	}
	var all []storage.AgentDownloadFile
	i := 0
	for _, s := range states {
		if held[s.Path] {
			continue
		}
		for ; i < len(files) && files[i].Path < s.Path; i++ {
			all = append(all, files[i])
		}
		all = append(all, storage.AgentDownloadFile{Path: s.Path, Version: s.Version, StartOffset: s.EndOffset, EndOffset: s.EndOffset})
	}
	return append(all, files[i:]...)
}
