package dispatcher

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/forge"
)

// maxReportText is the longest summary or question that a report keeps.
// The full text also goes to evidence/report.md (the wrapper writes it).
const maxReportText = 64 << 10

type executionHandler func(w http.ResponseWriter, r *http.Request, e storedExecution)

// wrapper checks that the request comes from the wrapper of the
// Execution in the path: its Bearer token must be a workload identity JWT
// that the Runner signed, of the workload that the Runner gave for that
// Execution. Each other token gets 401, with the same answer, so a caller
// learns nothing of other Executions.
func (d *Dispatcher) wrapper(next executionHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		refuse := func(why string) {
			d.log.Warn("a wrapper call was refused", "path", r.URL.Path, "reason", why)
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			writeProblem(w, http.StatusUnauthorized, "the workload identity is not valid for this Execution")
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			refuse("no token")
			return
		}
		identity, err := d.identity.check(r.Context(), strings.TrimSpace(token))
		if errors.Is(err, errBadIdentity) {
			refuse(err.Error())
			return
		}
		if err != nil {
			d.log.Error("could not check a workload identity", "error", err)
			writeProblem(w, http.StatusBadGateway, "the Dispatcher could not read the Runner's keys")
			return
		}
		id := r.PathValue("id")
		// The wrapper can call before the Runner's Dispatch returned the
		// workload: wait for it.
		if ready, ok := d.dispatching.Load(id); ok {
			select {
			case <-ready.(chan struct{}):
			case <-r.Context().Done():
				return
			}
		}
		e, err := d.store.execution(id)
		if errors.Is(err, errNotFound) {
			refuse("no Execution " + id)
			return
		}
		if err != nil {
			d.serverError(w, "read the Execution", err)
			return
		}
		if e.workload == "" {
			refuse("the Execution has no workload")
			return
		}
		// The subject is not enough: on Nomad, each dispatch of a job has
		// the same subject. The Runner finds the JWT's own workload.
		workload, err := d.runner.Workload(r.Context(), identity)
		if errors.Is(err, ErrUnknownWorkload) {
			refuse(err.Error())
			return
		}
		if err != nil {
			d.log.Error("could not find the workload of a workload identity", "error", err)
			writeProblem(w, http.StatusBadGateway, "the Dispatcher could not reach the Runner")
			return
		}
		if workload != e.workload {
			refuse("the JWT is of another workload")
			return
		}
		next(w, r, e)
	}
}

// task is the wrapper's task and secrets call. It starts the Execution:
// the Run is running.
func (d *Dispatcher) task(w http.ResponseWriter, r *http.Request, e storedExecution) {
	if e.state == dispatcherapi.ExecutionEnded {
		writeProblem(w, http.StatusConflict, errEnded.Error())
		return
	}
	settings, err := d.store.settings(e.organizationID, e.projectID)
	if errors.Is(err, errNotFound) {
		d.endByDispatcher(e.id, "the Project has no Factory settings", nil)
		writeProblem(w, http.StatusConflict, "the Project has no Factory settings")
		return
	}
	if err != nil {
		d.serverError(w, "read the Factory settings", err)
		return
	}
	var token string
	if settings.Connection != "" {
		if token, err = d.store.connectionToken(e.organizationID, settings.Connection); err != nil {
			d.serverError(w, "open the connection's token", err)
			return
		}
	}
	accountToken, err := d.store.accountToken(e.account)
	if errors.Is(err, errNotFound) {
		// An operator removed the account after the dispatch. The Run is
		// not lost: it goes back in the queue, and its next Execution gets
		// a free account.
		why := "the account " + e.account + " is no longer in the pool"
		if _, err := d.store.requeueExecution(e.id, why, d.now()); err != nil {
			d.serverError(w, "put the Run back in the queue", err)
			return
		}
		d.wakeDispatch()
		writeProblem(w, http.StatusConflict, why)
		return
	}
	if err != nil {
		d.serverError(w, "open the account's token", err)
		return
	}
	file, err := d.store.factoryFile(e.id)
	if err != nil {
		d.serverError(w, "read the Execution's factory file", err)
		return
	}
	run, err := d.store.startExecution(e.id, d.now(), file.limits())
	if errors.Is(err, errEnded) {
		writeProblem(w, http.StatusConflict, errEnded.Error())
		return
	}
	if err != nil {
		d.serverError(w, "start the Execution", err)
		return
	}
	// Each part of the checkout is the Run's. A Run from before the
	// Dispatcher kept the repository uses the settings' repository.
	repository := run.repository
	if repository == "" {
		repository = settings.Repository
	}
	// The clone URL is on the Dispatcher's Bitbucket Cloud (BitbucketGitURL).
	gitURL := strings.TrimRight(d.config.BitbucketGitURL, "/")
	if gitURL == "" {
		gitURL = forge.BitbucketGitURL
	}
	cloneURL, err := (&forge.Bitbucket{GitURL: gitURL}).CloneURL(repository)
	if err != nil {
		d.endByDispatcher(e.id, err.Error(), nil)
		writeProblem(w, http.StatusConflict, err.Error())
		return
	}
	uploads, err := d.sessionToken(r.Context(), e, run.startedAt)
	if errors.Is(err, errEnded) {
		writeProblem(w, http.StatusConflict, errEnded.Error())
		return
	}
	if err != nil {
		d.log.Error("could not make an Execution's token", "execution", e.id, "error", err)
		d.endByDispatcher(e.id, "the Dispatcher could not make the Execution's token at SolidTrace", nil)
		writeProblem(w, http.StatusBadGateway, "the Dispatcher could not make the Execution's token at SolidTrace")
		return
	}
	writeJSON(w, http.StatusOK, dispatcherapi.Task{
		RunID:         e.runID,
		ExecutionID:   e.id,
		Prompt:        run.prompt,
		Repository:    repository,
		CloneURL:      cloneURL,
		BaseBranch:    run.baseBranch,
		Branch:        dispatcherapi.RunBranch(e.runID),
		GitURL:        gitURL,
		Project:       run.project,
		Folder:        run.folder,
		ForgeUsername: forge.BitbucketTokenUser,
		ForgeToken:    token,
		AccountToken:  accountToken,
		SessionID:     run.sessionID,
		Resume:        run.resume,
		IngestURL:     d.config.ingestURL(),
		Limits:        run.limits,
		Uploads:       uploads,
		FactoryFile:   file.Path,
		Setup:         file.Setup,
		Checks:        file.Checks,
		SaveExclude:   file.SaveExclude,
		Messages:      run.messages,
	})
}

// report keeps the agent's report as the Execution's last valid report. A
// report that is not valid is refused and changes nothing.
func (d *Dispatcher) report(w http.ResponseWriter, r *http.Request, e storedExecution) {
	var report dispatcherapi.Report
	if !decode(w, r, &report, "the body must be a JSON object with outcome, summary and question") {
		return
	}
	report.Summary = strings.TrimSpace(report.Summary)
	report.Question = strings.TrimSpace(report.Question)
	problem := checkReport(report)
	if problem == "" {
		problem = checkEvidence(report)
	}
	if problem != "" {
		writeProblem(w, http.StatusBadRequest, problem)
		return
	}
	err := d.store.saveReport(e.id, report, d.now())
	if errors.Is(err, errEnded) {
		writeProblem(w, http.StatusConflict, "the Execution is not running: "+errEnded.Error()+" or has not started")
		return
	}
	if err != nil {
		d.serverError(w, "keep the report", err)
		return
	}
	d.afterReport(e)
	writeJSON(w, http.StatusOK, report)
}

func checkReport(r dispatcherapi.Report) string {
	switch {
	case r.Outcome != dispatcherapi.OutcomeDone && r.Outcome != dispatcherapi.OutcomeNeedsAPerson &&
		r.Outcome != dispatcherapi.OutcomeFailed:
		return "the outcome must be done, needs_a_person or failed"
	case len(r.Summary) > maxReportText || len(r.Question) > maxReportText:
		return "a summary or a question can have at most 64 KiB; put the full text in evidence/"
	case !utf8.ValidString(r.Summary) || !utf8.ValidString(r.Question):
		return "a summary and a question must be UTF-8 text"
	case r.Outcome == dispatcherapi.OutcomeNeedsAPerson && r.Question == "":
		return "a report of needs a person needs a question"
	case r.Outcome != dispatcherapi.OutcomeNeedsAPerson && r.Question != "":
		return "only a report of needs a person has a question"
	case r.Outcome != dispatcherapi.OutcomeNeedsAPerson && r.Summary == "":
		return "a report of " + r.Outcome + " needs a summary"
	}
	return ""
}

// end is the wrapper's end call: the Execution ended, and the Run's state
// is the outcome of its last valid report, or failed with the wrapper's
// reason.
func (d *Dispatcher) end(w http.ResponseWriter, r *http.Request, e storedExecution) {
	var end dispatcherapi.ExecutionEnd
	if !decode(w, r, &end, "the body must be a JSON object with reason, error, exit_code and task_delivered_at") {
		return
	}
	switch end.Reason {
	case dispatcherapi.EndReported, dispatcherapi.EndNoReport, dispatcherapi.EndHarnessExited,
		dispatcherapi.EndSetupFailed, dispatcherapi.EndStopped, dispatcherapi.EndTimeout, dispatcherapi.EndInterrupted:
	default:
		writeProblem(w, http.StatusBadRequest, "no end reason "+quote(end.Reason))
		return
	}
	if len(end.Error) > maxReportText {
		end.Error = strings.ToValidUTF8(end.Error[:maxReportText], "")
	}
	err := d.store.endExecution(e.id, end, d.now())
	if errors.Is(err, errEnded) {
		writeProblem(w, http.StatusConflict, "the Execution is not running: "+errEnded.Error()+" or has not started")
		return
	}
	if err != nil {
		d.serverError(w, "end the Execution", err)
		return
	}
	// The end step: the wrapper uploaded the last bytes before its end
	// call, so the Execution's tokens go now.
	d.revokeTokens(e.id)
	// A message that was not delivered can queue the Run again.
	d.wakeDispatch()
	w.WriteHeader(http.StatusNoContent)
}
