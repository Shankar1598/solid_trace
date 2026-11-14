package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// installFrom runs the release's install.sh with the release files served
// from dist, and returns the install directory, its output and its error.
func installFrom(t *testing.T, dist string) (string, string, error) {
	t.Helper()
	server := httptest.NewServer(http.FileServer(http.Dir(dist)))
	t.Cleanup(server.Close)
	dir := filepath.Join(t.TempDir(), "bin")
	cmd := exec.Command("sh", filepath.Join(dist, "install.sh"))
	cmd.Env = append(os.Environ(), "SOLIDTRACE_DOWNLOAD_URL="+server.URL, "SOLIDTRACE_INSTALL_DIR="+dir)
	out, err := cmd.CombinedOutput()
	return dir, string(out), err
}

func thisBinary() string {
	return "solidtrace-" + runtime.GOOS + "-" + runtime.GOARCH
}

func TestTheInstallScriptInstallsTheBinaryOfThisMachine(t *testing.T) {
	dist := release(t)

	dir, out, err := installFrom(t, dist)
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	got, err := os.ReadFile(filepath.Join(dir, "solidtrace"))
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if string(got) != "binary "+thisBinary() {
		t.Fatalf("installed %q", got)
	}
	if info, _ := os.Stat(filepath.Join(dir, "solidtrace")); info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("mode %v: not executable", info.Mode())
	}
	if !strings.Contains(out, "v0.19.0") {
		t.Fatalf("output does not name the version: %s", out)
	}
}

func TestTheInstallScriptRefusesABinaryWhoseSHA256DoesNotMatch(t *testing.T) {
	dist := release(t)
	// A changed download, after the checksum file was made.
	if err := os.WriteFile(filepath.Join(dist, thisBinary()), []byte("another binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	dir, out, err := installFrom(t, dist)
	if err == nil {
		t.Fatalf("install passed:\n%s", out)
	}
	if !strings.Contains(out, "SHA-256") {
		t.Fatalf("output: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "solidtrace")); !os.IsNotExist(err) {
		t.Fatal("the install script installed a binary whose SHA-256 does not match")
	}
}

func TestTheInstallScriptRefusesABinaryThatTheChecksumFileDoesNotList(t *testing.T) {
	dist := release(t)
	sums, _ := os.ReadFile(filepath.Join(dist, "SHA256SUMS"))
	var kept []string
	for _, line := range strings.SplitAfter(string(sums), "\n") {
		if !strings.HasSuffix(strings.TrimSpace(line), "  "+thisBinary()) {
			kept = append(kept, line)
		}
	}
	if err := os.WriteFile(filepath.Join(dist, "SHA256SUMS"), []byte(strings.Join(kept, "")), 0o644); err != nil {
		t.Fatal(err)
	}

	dir, out, err := installFrom(t, dist)
	if err == nil {
		t.Fatalf("install passed:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "solidtrace")); !os.IsNotExist(err) {
		t.Fatal("installed a binary with no checksum")
	}
}
