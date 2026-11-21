package factory

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The Autoscaler's own binary reads the reference policies. Its image is pinned by tag and
// digest: the digest of a tag can move, so a new version of the Autoscaler is a change here.
const autoscalerImage = "hashicorp/nomad-autoscaler:0.6.0@sha256:1140c079acaa8cfc0769f7add3553ea06f589673c4500d2139e01aa447575cad"

// The fallback policy reads Prometheus: the operator adds this block to autoscaler.hcl
// (docs/factory.md).
const prometheusBlock = `
apm "prometheus" {
  driver = "prometheus"
  config = {
    address = "http://127.0.0.1:9090"
  }
}
`

// autoscalerLogs starts the Autoscaler agent with config and the policy files, with no
// network (a Nomad that does not answer: the agent loads its policies before it needs
// Nomad), and gives its debug log when the log has want lines (or after 30 seconds).
func autoscalerLogs(t *testing.T, config string, policies map[string]string, want func(log string) bool) string {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available")
	}
	if err := exec.Command("docker", "image", "inspect", autoscalerImage).Run(); err != nil {
		if out, err := exec.Command("docker", "pull", autoscalerImage).CombinedOutput(); err != nil {
			t.Skipf("the image %s is not pulled, and docker pull fails: %v: %s", autoscalerImage, err, out)
		}
	}
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "policies"), 0o755))
	// The gce-mig target needs credentials when it loads. On the server VM it finds them at the
	// metadata server; here it reads a file of a user that has no access.
	config = strings.Replace(config, `driver = "gce-mig"`, "driver = \"gce-mig\"\n  config = {\n    credentials = \"/gce.json\"\n  }", 1)
	must(t, os.WriteFile(filepath.Join(dir, "gce.json"),
		[]byte(`{"type": "authorized_user", "client_id": "x", "client_secret": "y", "refresh_token": "z"}`), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "agent.hcl"), []byte(config), 0o644))
	for name, text := range policies {
		must(t, os.WriteFile(filepath.Join(dir, "policies", name), []byte(text), 0o644))
	}
	name := fmt.Sprintf("factory-autoscaler-reference-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	out, err := exec.Command("docker", "run", "-d", "--name", name, "--network", "none", "--memory", "256m",
		"-v", filepath.Join(dir, "agent.hcl")+":/agent.hcl:ro",
		"-v", filepath.Join(dir, "gce.json")+":/gce.json:ro",
		"-v", filepath.Join(dir, "policies")+":/etc/nomad-autoscaler/policies:ro",
		autoscalerImage, "agent", "-config=/agent.hcl", "-log-level=DEBUG").CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v: %s", err, out)
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })
	deadline := time.Now().Add(30 * time.Second)
	for {
		log, _ := exec.Command("docker", "logs", name).CombinedOutput()
		if want(string(log)) || time.Now().After(deadline) {
			return string(log)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// reference reads a file of factory/reference, with the placeholder of the server's address
// set.
func reference(t *testing.T, path ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{"reference"}, path...)...))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "SERVER_IP", "127.0.0.1")
}

// handlers counts the policies that the Autoscaler accepted: it makes a handler for each.
func handlers(log string) int { return strings.Count(log, "creating new policy handler") }

func rejected(log string) bool {
	return strings.Contains(log, "failed to decode file") || strings.Contains(log, "failed to validate file") ||
		strings.Contains(log, "latest version for policy is invalid") ||
		strings.Contains(log, "error getting the target for the policy handler") || strings.Contains(log, "failed to setup plugins")
}

func TestTheReferenceScalingPolicyPassesTheAutoscalersOwnValidation(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("reference", "autoscaler", "policies", "*.hcl"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no reference policy: %v", err)
	}
	policies := map[string]string{}
	for _, f := range files {
		policies[filepath.Base(f)] = reference(t, "autoscaler", "policies", filepath.Base(f))
	}

	log := autoscalerLogs(t, reference(t, "autoscaler", "autoscaler.hcl"), policies,
		func(log string) bool { return handlers(log) >= len(policies) || rejected(log) })

	if rejected(log) || handlers(log) != len(policies) {
		t.Errorf("the Autoscaler accepted %d of %d policies:\n%s", handlers(log), len(policies), log)
	}
}

func TestTheFallbackScalingPolicyPassesTheAutoscalersOwnValidation(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("reference", "autoscaler", "fallback", "*.hcl"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fallback policy: %v", err)
	}
	policies := map[string]string{}
	for _, f := range files {
		policies[filepath.Base(f)] = reference(t, "autoscaler", "fallback", filepath.Base(f))
	}

	log := autoscalerLogs(t, reference(t, "autoscaler", "autoscaler.hcl")+prometheusBlock, policies,
		func(log string) bool { return handlers(log) >= len(policies) || rejected(log) })

	if rejected(log) || handlers(log) != len(policies) {
		t.Errorf("the Autoscaler accepted %d of %d policies:\n%s", handlers(log), len(policies), log)
	}
}

// A check of the check: the Autoscaler's log shows a policy that it refuses, so a pass above
// means that the Autoscaler read the policies.
func TestTheAutoscalersValidationRefusesAMinimumOverTheMaximum(t *testing.T) {
	bad := strings.Replace(reference(t, "autoscaler", "policies", "standard.hcl"), "min     = 1", "min     = 11", 1)

	log := autoscalerLogs(t, reference(t, "autoscaler", "autoscaler.hcl"), map[string]string{"bad.hcl": bad},
		func(log string) bool { return handlers(log) > 0 || rejected(log) })

	if !strings.Contains(log, "policy Min must not be greater Max") || handlers(log) != 0 {
		t.Errorf("the Autoscaler did not refuse a minimum of 11 with a maximum of 10:\n%s", log)
	}
}
