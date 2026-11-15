//go:build factorystack

package nomadrunner_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"


	"github.com/hashicorp/nomad/api"
	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/nomadrunner"
)

// printIdentity is the task of these tests in place of the wrapper: it
// prints the JWT that Nomad gives the allocation, and waits.
const printIdentity = `echo "jwt $(cat "$WRAPPER_IDENTITY_FILE")"; exec sleep 600`

// identityOf gives the workload identity JWT that the allocation of the
// dispatched job got from Nomad.
func (s *stack) identityOf(t *testing.T, job string) (jwt, alloc string) {
	t.Helper()
	alloc = s.alloc(t, job).ID
	out := s.waitForStdout(t, alloc, "jwt ")
	_, jwt, _ = strings.Cut(out, "jwt ")
	return strings.TrimSpace(jwt), alloc
}

// part decodes one part of a JWT.
func part(t *testing.T, jwt string, i int) map[string]any {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(strings.Split(jwt, ".")[i])
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func encode(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// signed is header.claims signed with key (RS256).
func signed(t *testing.T, key *rsa.PrivateKey, header, claims map[string]any) string {
	t.Helper()
	input := encode(t, header) + "." + encode(t, claims)
	digest := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func TestTheTaskCallNeedsTheWorkloadIdentityOfTheAllocationOfItsExecution(t *testing.T) {
	s := theStack(t)
	r := s.runner(t, printIdentity)
	env := factorytest.New(t, factorytest.WithRunner(r))
	// Nomad signs with the real time.
	env.Clock.Advance(time.Since(env.Clock.Now()))
	env.Project("shop")
	ana := env.Member("Ana")

	// Two Runs, so two allocations of the same job: their JWTs have the
	// same subject.
	var runs, executions, jwts, allocs [2]string
	for i := range runs {
		a := ana.API("POST", "/api/v1/runs", dispatcherapi.StartRun{Project: "shop", Prompt: "Fix the cart"})
		if a.Status/100 != 2 {
			t.Fatalf("start a Run: %d %s", a.Status, a.Body)
		}
		var run dispatcherapi.Run
		a.JSON(t, &run)
		runs[i] = run.ID
		for deadline := time.Now().Add(30 * time.Second); len(run.Executions) == 0; time.Sleep(100 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("the Run %s has no Execution after 30 s:\n%s", run.ID, env.Logs())
			}
			ana.API("GET", "/api/v1/runs/"+runs[i], nil).JSON(t, &run)
		}
		executions[i] = run.Executions[0].ID
	}
	jobs, _, err := s.api.Jobs().PrefixList(r.JobID("default") + "/dispatch-")
	if err != nil || len(jobs) != 2 {
		t.Fatalf("%d dispatched jobs, %v", len(jobs), err)
	}
	for _, j := range jobs {
		job := s.job(t, j.ID)
		for i := range executions {
			if job.Meta[nomadrunner.MetaExecution] == executions[i] {
				jwts[i], allocs[i] = s.identityOf(t, j.ID)
			}
		}
	}
	a, b := executions[0], executions[1]
	if jwts[0] == "" || jwts[1] == "" {
		t.Fatalf("no JWT of an Execution: %q", jwts)
	}
	// So the Dispatcher cannot tell them apart by the subject.
	if sa, sb := part(t, jwts[0], 1)["sub"], part(t, jwts[1], 1)["sub"]; sa != sb {
		t.Logf("Nomad gave the two allocations different subjects: %v, %v", sa, sb)
	}

	// A JWT of a Nomad job that is not the Runner's, for the same
	// audience.
	other := s.otherJob(t)

	header, claims := part(t, jwts[0], 0), part(t, jwts[0], 1)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	withAllocOfB := map[string]any{}
	for k, v := range claims {
		withAllocOfB[k] = v
	}
	withAllocOfB["nomad_allocation_id"] = allocs[1]
	parts := strings.Split(jwts[0], ".")
	refused := map[string]string{
		"the JWT of the other Execution's allocation": jwts[1],
		"a JWT of a job that is not the Runner's":     other,
		"a JWT with Nomad's kid and another key":      signed(t, key, header, claims),
		"the JWT with the allocation of the other Execution and the first signature": parts[0] + "." +
			encode(t, withAllocOfB) + "." + parts[2],
		"the JWT with alg none": encode(t, map[string]any{"alg": "none", "typ": "JWT"}) + "." + parts[1] + ".",
		"no JWT":                "",
	}
	for name, token := range refused {
		for _, verb := range []string{"task", "report", "end"} {
			got := env.API(token, "POST", "/api/v1/executions/"+a+"/"+verb, map[string]string{})
			if got.Status != http.StatusUnauthorized {
				t.Errorf("%s call with %s: %d %s", verb, name, got.Status, got.Body)
			}
		}
	}

	for i, execution := range executions {
		got := env.API(jwts[i], "POST", "/api/v1/executions/"+execution+"/task", map[string]string{})
		if got.Status != http.StatusOK {
			t.Fatalf("the task call of Execution %d with its allocation's JWT: %d %s\n%s", i, got.Status, got.Body, env.Logs())
		}
		var task dispatcherapi.Task
		got.JSON(t, &task)
		if task.ExecutionID != execution || task.RunID != runs[i] {
			t.Errorf("the task of Execution %s is for %s of Run %s", execution, task.ExecutionID, task.RunID)
		}
	}
	// The JWT of b is still refused for a, after the two task calls.
	if got := env.API(jwts[1], "POST", "/api/v1/executions/"+a+"/report", map[string]string{}); got.Status != http.StatusUnauthorized {
		t.Errorf("the report call of %s with the JWT of %s: %d", a, b, got.Status)
	}

	// The Dispatcher sees the end: a wrapper that is gone before its end
	// call fails its Run.
	if err := r.Stop(t.Context(), jobs[0].ID); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		var stopped dispatcherapi.Run
		for i := range runs {
			var run dispatcherapi.Run
			ana.API("GET", "/api/v1/runs/"+runs[i], nil).JSON(t, &run)
			if run.State == dispatcherapi.StateFailed {
				stopped = run
			}
		}
		if stopped.ID != "" {
			if !strings.Contains(stopped.Reason, "before the end of the Execution") {
				t.Errorf("the Run failed with %q", stopped.Reason)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no Run failed 60 s after its wrapper stopped:\n%s", env.Logs())
		}
	}
}

// otherJob runs a Nomad job that is not the Runner's, with a workload
// identity for the Dispatcher's audience, and gives its JWT.
func (s *stack) otherJob(t *testing.T) string {
	t.Helper()
	id := "test-other-" + random(t)
	job := api.NewBatchJob(id, id, "", api.JobDefaultPriority)
	job.Datacenters = []string{"*"}
	task := api.NewTask(nomadrunner.TaskName, "raw_exec")
	task.Config = map[string]any{"command": "/bin/sh",
		"args": []string{"-c", `echo "jwt $(cat "$NOMAD_SECRETS_DIR/nomad_dispatcher.jwt")"; exec sleep 600`}}
	memory := 64
	task.Resources = &api.Resources{MemoryMB: &memory}
	task.Identities = []*api.WorkloadIdentity{{Name: nomadrunner.IdentityName, Audience: []string{"solidtrace-dispatcher"}, File: true, TTL: time.Hour}}
	group := api.NewTaskGroup(nomadrunner.GroupName, 1)
	group.AddTask(task)
	job.AddTaskGroup(group)
	if _, _, err := s.api.Jobs().Register(job, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.api.Jobs().Deregister(id, true, nil) })
	jwt, _ := s.identityOf(t, id)
	return jwt
}
