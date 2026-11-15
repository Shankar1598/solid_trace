// Package fakedocker is the fake Docker of Seam 3: a docker program that
// keeps its containers as files in a directory, so that a test can see
// what the agent ran and what the wrapper's end procedure stopped. No test
// may reach the real Docker of the dev box: the wrapper's end kills each
// running container.
//
// The program knows three commands:
//
//	docker run [flags] IMAGE [COMMAND...]   a new running container; prints its id
//	docker ps -q                            the ids of the running containers
//	docker kill ID...                       stops each container; prints its id
//
// With Hang(true), each command waits for 60 seconds first, as a docker
// whose daemon does not answer.
package fakedocker

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// EnvDir names the directory of the fake Docker's state.
const EnvDir = "FAKE_DOCKER_DIR"

// Docker is the state of one fake Docker, as the test controls it.
type Docker struct{ dir string }

// New makes a fake Docker with its state in dir.
func New(dir string) (*Docker, error) {
	for _, d := range []string{"running", "killed"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return nil, err
		}
	}
	return &Docker{dir: dir}, nil
}

// Dir is the state directory, for EnvDir.
func (d *Docker) Dir() string { return d.dir }

// Running gives the ids of the running containers.
func (d *Docker) Running() []string { return list(filepath.Join(d.dir, "running")) }

// Killed gives the ids of the containers that docker kill stopped.
func (d *Docker) Killed() []string { return list(filepath.Join(d.dir, "killed")) }

// Hang makes each later command wait 60 seconds first (false: no wait).
func (d *Docker) Hang(on bool) error {
	path := filepath.Join(d.dir, "hang")
	if on {
		return os.WriteFile(path, nil, 0o644)
	}
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func list(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var ids []string
	for _, e := range entries {
		ids = append(ids, e.Name())
	}
	sort.Strings(ids)
	return ids
}

// Main is the docker program. args are the arguments after its name.
func Main(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	dir := getenv(EnvDir)
	if dir == "" {
		fmt.Fprintln(stderr, "fake docker: "+EnvDir+" is not set")
		return 1
	}
	if _, err := os.Stat(filepath.Join(dir, "hang")); err == nil {
		time.Sleep(60 * time.Second)
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "fake docker: no command")
		return 1
	}
	switch args[0] {
	case "run":
		var b [6]byte
		rand.Read(b[:])
		id := fmt.Sprintf("%x", b)
		if err := os.WriteFile(filepath.Join(dir, "running", id), nil, 0o644); err != nil {
			fmt.Fprintln(stderr, "fake docker:", err)
			return 1
		}
		fmt.Fprintln(stdout, id)
	case "ps":
		for _, id := range list(filepath.Join(dir, "running")) {
			fmt.Fprintln(stdout, id)
		}
	case "kill":
		code := 0
		for _, id := range args[1:] {
			if err := os.Rename(filepath.Join(dir, "running", id), filepath.Join(dir, "killed", id)); err != nil {
				fmt.Fprintf(stderr, "Error response from daemon: No such container: %s\n", id)
				code = 1
				continue
			}
			fmt.Fprintln(stdout, id)
		}
		return code
	default:
		fmt.Fprintf(stderr, "fake docker: no command %q\n", args[0])
		return 1
	}
	return 0
}
