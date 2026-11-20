// Package fakehc is the fake Take-over of Seam 3: an hc program (the
// terminal core of a take-over) that keeps its terminals as files in a
// directory, so that a test can be the person at the terminal: see what
// the program shows, type lines, and leave. No test may reach the real hc
// of the dev box.
//
// The program knows the commands of hc that the wrapper uses:
//
//	hc server start                     records the core's user and environment
//	hc run --bg --json [--name N] [--cwd D] [--no-notify] -- ARGV...
//	                                    starts ARGV in a new terminal; prints {"address":"t1"}
//	hc get --json T                     prints {"address","state":"live"|"finished","exit_code"}
//	hc kill T                           ends T's program (SIGTERM), and waits for it
//
// A terminal's program runs under a holder process (the core's part): its
// stdin gets each line that the test types, and closes when the test
// leaves; its stdout and stderr are the screen. Both run in the cgroup of
// the hc run command, as the real core's terminals run in the core's.
package fakehc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// EnvDir names the directory of the fake hc's state.
const EnvDir = "FAKE_HC_DIR"

// holdCommand is the holder's command (not a command of the real hc).
const holdCommand = "__hold"

// HC is the state of one fake hc, as the test controls it.
type HC struct{ dir string }

// New makes a fake hc with its state in dir.
func New(dir string) (*HC, error) {
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return nil, err
	}
	// As root, the run user runs hc: it writes here.
	if err := os.Chmod(dir, 0o777); err != nil {
		return nil, err
	}
	return &HC{dir: dir}, nil
}

// Dir is the state directory, for EnvDir.
func (h *HC) Dir() string { return h.dir }

// Terminals gives the addresses of the terminals that hc run started, in
// order.
func (h *HC) Terminals() []string {
	entries, _ := os.ReadDir(h.dir)
	var list []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "t") {
			list = append(list, e.Name())
		}
	}
	sort.Slice(list, func(i, j int) bool { return number(list[i]) < number(list[j]) })
	return list
}

func number(address string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(address, "t"))
	return n
}

// WaitForTerminal waits up to timeout for the nth terminal (from 1), and
// gives its address.
func (h *HC) WaitForTerminal(n int, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		if list := h.Terminals(); len(list) >= n {
			address := list[n-1]
			if _, err := os.Stat(filepath.Join(h.dir, address, "pid")); err == nil {
				return address, nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("fake hc: no terminal %d after %s", n, timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Screen gives what the terminal's program wrote.
func (h *HC) Screen(address string) string {
	b, _ := os.ReadFile(filepath.Join(h.dir, address, "screen"))
	return string(b)
}

// WaitForScreen waits up to timeout until the screen holds text.
func (h *HC) WaitForScreen(address, text string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		screen := h.Screen(address)
		if strings.Contains(screen, text) {
			return screen, nil
		}
		if time.Now().After(deadline) {
			return screen, fmt.Errorf("fake hc: the screen of %s has no %q after %s", address, text, timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Type types line and Enter into the terminal, as the person.
func (h *HC) Type(address, line string) error {
	f, err := os.OpenFile(filepath.Join(h.dir, address, "typed"), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o666)
	if err != nil {
		return err
	}
	_, err = f.WriteString(line + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Leave closes the program's stdin, as a person who leaves the harness.
func (h *HC) Leave(address string) error {
	return os.WriteFile(filepath.Join(h.dir, address, "leave"), nil, 0o666)
}

// Finished tells if the terminal's program ended.
func (h *HC) Finished(address string) bool {
	_, err := os.Stat(filepath.Join(h.dir, address, "exit"))
	return err == nil
}

// Core gives the environment of the last hc server start, and its user id
// (-1: no core started).
func (h *HC) Core() (env []string, uid int) {
	b, err := os.ReadFile(filepath.Join(h.dir, "core"))
	if err != nil {
		return nil, -1
	}
	var core struct {
		UID int      `json:"uid"`
		Env []string `json:"env"`
	}
	if json.Unmarshal(b, &core) != nil {
		return nil, -1
	}
	return core.Env, core.UID
}

// Main is the hc program. args are the arguments after its name.
func Main(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	dir := getenv(EnvDir)
	if dir == "" {
		fmt.Fprintln(stderr, "fake hc: "+EnvDir+" is not set")
		return 1
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "fake hc: give a command")
		return 2
	}
	var err error
	switch args[0] {
	case "server":
		if len(args) >= 2 && args[1] == "status" {
			err = status(dir, stdout)
			break
		}
		if len(args) < 2 || args[1] != "start" {
			err = errors.New("only server start and server status")
			break
		}
		var b []byte
		b, err = json.Marshal(map[string]any{"uid": os.Getuid(), "env": os.Environ()})
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, "core"), b, 0o666)
		}
	case "run":
		err = run(dir, args[1:], stdout)
	case "get":
		err = get(dir, args[1:], stdout)
	case "kill":
		err = kill(dir, args[1:], stdout)
	case holdCommand:
		return hold(dir, args[1:])
	default:
		err = fmt.Errorf("no command %q", args[0])
	}
	if err != nil {
		fmt.Fprintln(stderr, "fake hc:", err)
		return 1
	}
	return 0
}

// run starts the holder of a new terminal, and waits until its program
// started.
func run(dir string, args []string, stdout io.Writer) error {
	cwd, bg := "", false
	var argv []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--":
			argv = args[i+1:]
			i = len(args)
		case a == "--bg":
			bg = true
		case a == "--json" || a == "--no-notify" || a == "--no-parent":
		case a == "--cwd" || a == "--name":
			if i+1 >= len(args) {
				return fmt.Errorf("%s needs a value", a)
			}
			i++
			if a == "--cwd" {
				cwd = args[i]
			}
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("no option %s", a)
		default:
			argv = args[i:]
			i = len(args)
		}
	}
	if !bg || len(argv) == 0 {
		return errors.New("the fake hc runs only --bg with a program")
	}
	var address string
	for n := 1; ; n++ {
		address = "t" + strconv.Itoa(n)
		if err := os.Mkdir(filepath.Join(dir, address), 0o777); err == nil {
			break
		} else if !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	tdir := filepath.Join(dir, address)
	os.Chmod(tdir, 0o777)
	b, _ := json.Marshal(argv)
	if err := os.WriteFile(filepath.Join(tdir, "argv"), b, 0o666); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	holder := exec.Command(self, holdCommand, address)
	holder.Args[0] = os.Args[0]
	holder.Path = self
	holder.Dir = cwd
	holder.Env = os.Environ()
	// Its own session: it stays when hc run exits, as the core's
	// terminals do.
	holder.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := holder.Start(); err != nil {
		return err
	}
	holder.Process.Release()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(tdir, "pid")); err == nil {
			break
		}
		if _, err := os.Stat(filepath.Join(tdir, "exit")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return errors.New("the terminal's program did not start in 10 s")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"address": address})
}

// hold runs the terminal's program: the typed lines go to its stdin, and
// its output to the screen. It writes the exit code at the end.
func hold(dir string, args []string) int {
	if len(args) != 1 {
		return 2
	}
	tdir := filepath.Join(dir, args[0])
	os.WriteFile(filepath.Join(tdir, "holder"), []byte(strconv.Itoa(os.Getpid())), 0o666)
	var argv []string
	b, err := os.ReadFile(filepath.Join(tdir, "argv"))
	if err == nil {
		err = json.Unmarshal(b, &argv)
	}
	screen, serr := os.OpenFile(filepath.Join(tdir, "screen"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o666)
	finish := func(code int) int {
		os.WriteFile(filepath.Join(tdir, "exit"), []byte(strconv.Itoa(code)), 0o666)
		return 0
	}
	if err != nil || serr != nil || len(argv) == 0 {
		return finish(127)
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout, cmd.Stderr = screen, screen
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "HC_TERMINAL_ID="+args[0])
	in, err := cmd.StdinPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		fmt.Fprintln(screen, "fake hc:", err)
		return finish(127)
	}
	os.WriteFile(filepath.Join(tdir, "pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o666)
	// The person: each new byte of typed goes to stdin; leave closes it.
	go func() {
		defer in.Close()
		var sent int64
		for {
			if f, err := os.Open(filepath.Join(tdir, "typed")); err == nil {
				f.Seek(sent, io.SeekStart)
				n, _ := io.Copy(in, f)
				sent += n
				f.Close()
			}
			if _, err := os.Stat(filepath.Join(tdir, "leave")); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	err = cmd.Wait()
	code := 0
	if err != nil {
		code = -1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
	}
	return finish(code)
}

func address(args []string) (string, error) {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a, nil
		}
	}
	return "", errors.New("give a terminal")
}

// get prints the terminal's state.
func get(dir string, args []string, stdout io.Writer) error {
	t, err := address(args)
	if err != nil {
		return err
	}
	tdir := filepath.Join(dir, t)
	if _, err := os.Stat(tdir); err != nil {
		return fmt.Errorf("not_found: no terminal %s", t)
	}
	state := map[string]any{"address": t, "state": "live", "exit_code": nil}
	if b, err := os.ReadFile(filepath.Join(tdir, "exit")); err == nil {
		code, _ := strconv.Atoi(strings.TrimSpace(string(b)))
		state["state"], state["exit_code"] = "finished", code
	}
	return json.NewEncoder(stdout).Encode(state)
}

// kill ends the terminal's program and waits up to 5 s for its end.
func kill(dir string, args []string, stdout io.Writer) error {
	t, err := address(args)
	if err != nil {
		return err
	}
	tdir := filepath.Join(dir, t)
	b, err := os.ReadFile(filepath.Join(tdir, "pid"))
	if err != nil {
		return fmt.Errorf("not_found: no terminal %s", t)
	}
	if pid, _ := strconv.Atoi(strings.TrimSpace(string(b))); pid > 0 {
		syscall.Kill(pid, syscall.SIGTERM)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(tdir, "exit")); err == nil {
			return get(dir, []string{t}, stdout)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("the terminal's program did not end in 5 s")
}

// EscapeCore makes the fake's core seem to be outside the Execution's
// cgroup: server status gives a process that is not in it (a core that
// a person's hc connect started before the wrapper's).
func (h *HC) EscapeCore(pid int) error {
	return os.WriteFile(filepath.Join(h.dir, "escaped"), []byte(strconv.Itoa(pid)), 0o666)
}

// status prints the core's pid: the fake has no core process, so it is
// the holder of the last terminal, which is in the cgroup of hc run.
func status(dir string, stdout io.Writer) error {
	pid := 0
	if b, err := os.ReadFile(filepath.Join(dir, "escaped")); err == nil {
		pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	} else if list := (&HC{dir: dir}).Terminals(); len(list) > 0 {
		b, _ := os.ReadFile(filepath.Join(dir, list[len(list)-1], "holder"))
		pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	if pid <= 0 {
		return errors.New("no core")
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"pid": pid, "holders": []map[string]any{{"pid": pid}}})
}
