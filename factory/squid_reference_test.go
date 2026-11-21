package factory

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A default SolidTrace install has its ingest at https://host:4443, so a wrapper sends
// "CONNECT host:4443" to Squid. The reference squid.conf must let that request pass, and
// must refuse a CONNECT to the same host on another port, and to another host on 4443.
// The test runs the reference file in the pinned Squid image of the local stack.
func TestTheReferenceSquidAllowsTheIngestOfADefaultInstall(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available")
	}
	compose, err := os.ReadFile(filepath.Join("local", "compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	found := regexp.MustCompile(`image: ubuntu/squid:\S+?(@sha256:[0-9a-f]+)`).FindStringSubmatch(string(compose))
	if found == nil {
		t.Fatal("factory/local/compose.yml names no pinned Squid image")
	}
	// A local image has the digest and no tag, once the tag has moved: use the digest.
	image := []string{"", "ubuntu/squid" + found[1]}
	if err := exec.Command("docker", "image", "inspect", image[1]).Run(); err != nil {
		t.Skipf("the image %s is not pulled", image[1])
	}

	conf, err := os.ReadFile(filepath.Join("reference", "squid", "squid.conf"))
	if err != nil {
		t.Fatal(err)
	}
	// The test's client is the Docker bridge, not the agent VMs' subnet.
	conf = []byte(strings.Replace(string(conf), "src 10.100.0.0/24", "src 0.0.0.0/0", 1))
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "squid.conf"), conf, 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "allowlist.txt"), []byte(".github.com\n"), 0o644))

	name := fmt.Sprintf("factory-squid-reference-test-%d", os.Getpid())
	out, err := exec.Command("docker", "run", "-d", "--rm", "--name", name, "--memory", "256m",
		"-p", "127.0.0.1::3128",
		"-v", filepath.Join(dir, "squid.conf")+":/etc/squid/squid.conf:ro",
		"-v", filepath.Join(dir, "allowlist.txt")+":/etc/squid/factory/allowlist.txt:ro",
		image[1]).CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v: %s", err, out)
	}
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", name).Run() })
	portOut, err := exec.Command("docker", "port", name, "3128/tcp").Output()
	if err != nil {
		t.Fatal(err)
	}
	address := strings.TrimSpace(strings.SplitN(string(portOut), "\n", 2)[0])

	// Squid sends 403 for a request that the rules refuse. A request that the rules allow
	// goes on to the host's address, which does not exist here (a 5xx answer, not a 403).
	connect := func(target string) string {
		deadline := time.Now().Add(30 * time.Second)
		for {
			if status, err := connectStatus(address, target); err == nil {
				return status
			} else if time.Now().After(deadline) {
				t.Fatalf("CONNECT %s: %v", target, err)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	for _, c := range []struct {
		target  string
		allowed bool
	}{
		{"solidtrace.example.com:4443", true},
		{"solidtrace.example.com:22", false},
		{"other.example.com:4443", false},
		{"other.example.com:22", false},
	} {
		status := connect(c.target)
		if refused := status == "403"; refused == c.allowed {
			t.Errorf("CONNECT %s: Squid answered %s, allowed is %v", c.target, status, c.allowed)
		}
	}
}

func connectStatus(address, target string) (string, error) {
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(20 * time.Second))
	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return "", err
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", fmt.Errorf("odd answer %q", line)
	}
	return fields[1], nil
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
