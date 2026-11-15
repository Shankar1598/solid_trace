package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/solidtrace/cli/command"
	"github.com/solidtrace/cli/dispatcherapi"
)

func runReport(socket string, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := command.Run(context.Background(), append([]string{"report"}, args...), command.Env{
		Stdout: &stdout, Stderr: &stderr, AgentSocket: socket})
	return code, stdout.String(), stderr.String()
}

// wrapperSocket is a wrapper's socket that answers each report with
// status and problem, and keeps what it got.
func wrapperSocket(t *testing.T, status int, problem string) (string, *[]dispatcherapi.Report) {
	t.Helper()
	dir, err := os.MkdirTemp("", "report-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "agent.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	var got []dispatcherapi.Report
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report dispatcherapi.Report
		json.NewDecoder(r.Body).Decode(&report)
		got = append(got, report)
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(dispatcherapi.Problem{Error: problem})
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return path, &got
}

func TestReportSendsTheOutcomeToTheWrapper(t *testing.T) {
	socket, got := wrapperSocket(t, http.StatusOK, "")

	code, stdout, stderr := runReport(socket, "--outcome", "needs-a-person", "--summary", " Two ways ",
		"--question", "Which one?")

	if code != 0 || !strings.Contains(stdout, "Reported needs-a-person.") {
		t.Fatalf("exit %d\n%s%s", code, stdout, stderr)
	}
	want := dispatcherapi.Report{Outcome: dispatcherapi.OutcomeNeedsAPerson, Summary: "Two ways", Question: "Which one?"}
	if len(*got) != 1 || !reflect.DeepEqual((*got)[0], want) {
		t.Errorf("the wrapper got %+v", *got)
	}
}

func TestReportGivesTheRefusalOfTheWrapper(t *testing.T) {
	socket, _ := wrapperSocket(t, http.StatusBadRequest, "a report of done needs a summary")

	code, _, stderr := runReport(socket, "--outcome", "done")

	if code != 1 || !strings.Contains(stderr, "a report of done needs a summary") {
		t.Errorf("exit %d: %s", code, stderr)
	}
}

func TestReportWorksOnlyInsideARun(t *testing.T) {
	code, _, stderr := runReport("", "--outcome", "done", "--summary", "Fixed")

	if code != 1 || !strings.Contains(stderr, "only inside a Run") {
		t.Errorf("exit %d: %s", code, stderr)
	}
}

func TestReportRefusesAnOutcomeThatItDoesNotKnow(t *testing.T) {
	socket, got := wrapperSocket(t, http.StatusOK, "")

	code, _, stderr := runReport(socket, "--outcome", "finished", "--summary", "Fixed")

	if code != 2 || !strings.Contains(stderr, "done, needs-a-person or failed") || len(*got) != 0 {
		t.Errorf("exit %d, sent %d: %s", code, len(*got), stderr)
	}
}
