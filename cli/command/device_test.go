package command_test

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLoginWithDevicePrintsACodeAndFinishesWhenThePersonApproves(t *testing.T) {
	w := newWorld(t, acme)
	w.oauth.ApproveAfterPolls = 2

	if code := w.login("--device"); code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, w.stdout, w.stderr)
	}

	if len(w.opened) != 0 {
		t.Errorf("--device opened the browser")
	}
	if len(w.oauth.DeviceRequests) != 1 {
		t.Fatalf("device codes asked: %d", len(w.oauth.DeviceRequests))
	}
	form := w.oauth.DeviceRequests[0]
	if form.Get("client_id") != "solidtrace-cli" || form.Get("scope") != "sessions:write sessions:download factory" ||
		form.Get("resource") != w.oauth.Resource() {
		t.Errorf("device authorization form: %v", form)
	}
	out := w.stdout.String()
	for _, want := range []string{w.oauth.URL() + "/device", "WDJB-0001", "Acme", "bitbucket.org/acme/shop"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout has no %q:\n%s", want, out)
		}
	}
	// Two pending answers, then the tokens: three polls, 5 seconds apart.
	if len(w.oauth.Polls) != 3 {
		t.Errorf("polls: %d, want 3", len(w.oauth.Polls))
	}
	for i, d := range w.slept {
		if d != 5*time.Second {
			t.Errorf("wait %d: %s, want 5s", i, d)
		}
	}
	if _, err := w.signedInToken(); err != nil {
		t.Errorf("no sign-in after the device flow: %v", err)
	}
}

func TestLoginWithDeviceWaitsLongerAfterSlowDown(t *testing.T) {
	w := newWorld(t, acme)
	w.oauth.ApproveAfterPolls = 3
	w.oauth.SlowDownAt = 2

	if code := w.login("--device"); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}
	want := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second, 10 * time.Second}
	if len(w.slept) != len(want) {
		t.Fatalf("waits %v, want %v", w.slept, want)
	}
	for i := range want {
		if w.slept[i] != want[i] {
			t.Errorf("waits %v, want %v", w.slept, want)
			break
		}
	}
}

func TestLoginWithDeviceStopsWhenThePersonDenies(t *testing.T) {
	w := newWorld(t, acme)
	w.oauth.Deny = true

	if code := w.login("--device"); code == 0 {
		t.Fatal("login passed after Deny")
	}
	if !strings.Contains(w.stderr.String(), "denied") {
		t.Errorf("stderr: %s", w.stderr)
	}
	w.assertNotSignedIn()
}

func TestLoginWithDeviceStopsWhenTheCodeExpires(t *testing.T) {
	w := newWorld(t, acme)
	w.oauth.ApproveAfterPolls = 1000

	if code := w.login("--device"); code == 0 {
		t.Fatal("login passed with no approval")
	}
	if !strings.Contains(w.stderr.String(), "expired") {
		t.Errorf("stderr: %s", w.stderr)
	}
	// 10 minutes at 5 seconds: about 120 polls, never more.
	if n := len(w.oauth.Polls); n > 121 {
		t.Errorf("polled %d times after the code expired", n)
	}
	w.assertNotSignedIn()
}

func TestLoginUsesTheDeviceFlowOnAMachineWithNoBrowser(t *testing.T) {
	w := newWorld(t, acme)
	w.browser = nil

	if code := w.login(); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}
	if len(w.oauth.DeviceRequests) != 1 || len(w.oauth.Authorizations) != 0 {
		t.Errorf("device requests %d, approval pages %d", len(w.oauth.DeviceRequests), len(w.oauth.Authorizations))
	}
}

func TestLoginUsesTheDeviceFlowWhenTheBrowserDoesNotOpen(t *testing.T) {
	w := newWorld(t, acme)
	w.browser = func(string) error { return errors.New("xdg-open: no method available") }

	if code := w.login(); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}
	if len(w.oauth.DeviceRequests) != 1 {
		t.Errorf("device requests %d", len(w.oauth.DeviceRequests))
	}
	if !strings.Contains(w.stdout.String(), "with a code") {
		t.Errorf("stdout: %s", w.stdout)
	}
}
