// Package fakehc is the fake Take-over of Seam 3: an hc program (the
// terminal core of a take-over) that keeps its terminals as files in a
// directory, so that a test can be the person at the terminal: see what
// the program shows, type lines, and leave. No test may reach the real hc
// of the dev box.
//
// The program knows the commands of hc that the wrapper uses:
//
//	hc server start                     starts the core, when no core runs
//	hc server status --json             prints {"pid","holders":[{"pid"}]}
//	hc run --bg --json [--name N] [--cwd D] [--no-notify] -- ARGV...
//	                                    starts ARGV in a new terminal; prints {"address":"t1"}
//	hc get --json T                     prints {"address","state":"live"|"finished","exit_code"}
//	hc kill T                           ends T's program (SIGTERM), and waits for it
//
// The core is a process, as the real one: hc server start starts it,
// and it starts each terminal's holder as its child. So a terminal runs
// in the core's cgroup, not in the cgroup of the hc run command: a core
// that started outside the Execution's cgroup (StartCore, as a person's
// early hc connect) runs its terminals outside it. A terminal's program
// runs under its holder: its stdin gets each line that the test types,
// and closes when the test leaves; its stdout and stderr are the screen.
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

// holdCommand is the holder's command, and coreCommand the core's (not
// commands of the real hc).
const (
	holdCommand = "__hold"
	coreCommand = "__core"
)

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

// Core gives the environment of the last core that started, and its user
// id (-1: no core started).
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
		if livePID(dir) > 0 {
			// As the real hc: a core runs, so nothing happens.
			break
		}
		var cmd *exec.Cmd
		if cmd, err = coreCmd(""); err == nil {
			err = startCore(dir, cmd)
		}
	case "run":
		err = run(dir, args[1:], stdout)
	case "get":
		err = get(dir, args[1:], stdout)
	case "kill":
		err = kill(dir, args[1:], stdout)
	case holdCommand:
		return hold(dir, args[1:])
	case coreCommand:
		return core(dir)
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
	if livePID(dir) <= 0 {
		return errors.New("no core: run hc server start")
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
	if err := os.WriteFile(filepath.Join(tdir, "cwd"), []byte(cwd), 0o666); err != nil {
		return err
	}
	b, _ := json.Marshal(argv)
	// The argv file comes last: with it, the core starts the holder.
	if err := os.WriteFile(filepath.Join(tdir, "argv.new"), b, 0o666); err != nil {
		return err
	}
	if err := os.Rename(filepath.Join(tdir, "argv.new"), filepath.Join(tdir, "argv")); err != nil {
		return err
	}
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

// StartCore starts a core from the test's process, so outside the
// Execution's cgroup, as a person's 'sudo -u <run user> hc connect'
// starts one before the wrapper's. program is the fake hc program, and
// attr can set the core's user. The test ends the core it gives.
func (h *HC) StartCore(program string, attr *syscall.SysProcAttr) (*exec.Cmd, error) {
	cmd, err := coreCmd(program)
	if err != nil {
		return nil, err
	}
	cmd.Env = append(os.Environ(), EnvDir+"="+h.dir)
	cmd.SysProcAttr = attr
	return cmd, startCore(h.dir, cmd)
}

// coreCmd is the command of the core: program, or this hc program.
func coreCmd(program string) (*exec.Cmd, error) {
	path := program
	if path == "" {
		self, err := os.Executable()
		if err != nil {
			return nil, err
		}
		path = self
	}
	cmd := exec.Command(path, coreCommand)
	if program == "" {
		// The name chooses the program of the test binary.
		cmd.Args[0] = os.Args[0]
	}
	cmd.Env = os.Environ()
	// Its own session: it stays when hc server start exits.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd, nil
}

// startCore starts cmd, the core, and waits until it runs.
func startCore(dir string, cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if livePID(dir) == pid {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("the core did not start in 10 s")
		}
	}
}

// livePID gives the pid of the core that runs (0: none). A core that the
// end killed can stay as a zombie, with no command line: it does not run.
func livePID(dir string) int {
	b, err := os.ReadFile(filepath.Join(dir, "core"))
	if err != nil {
		return 0
	}
	var c struct {
		PID int `json:"pid"`
	}
	if json.Unmarshal(b, &c) != nil || c.PID <= 0 {
		return 0
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", c.PID))
	if err != nil || !strings.Contains(string(cmdline), coreCommand) {
		return 0
	}
	return c.PID
}

// core is the core's process: it records its user and environment, and
// starts the holder of each new terminal as its child, so in its cgroup.
// It runs until it is killed.
func core(dir string) int {
	b, _ := json.Marshal(map[string]any{"uid": os.Getuid(), "env": os.Environ(), "pid": os.Getpid()})
	tmp := filepath.Join(dir, fmt.Sprintf("core.%d", os.Getpid()))
	if os.WriteFile(tmp, b, 0o666) != nil || os.Rename(tmp, filepath.Join(dir, "core")) != nil {
		return 1
	}
	self, err := os.Executable()
	if err != nil {
		return 1
	}
	for ; ; time.Sleep(5 * time.Millisecond) {
		for _, address := range (&HC{dir: dir}).Terminals() {
			tdir := filepath.Join(dir, address)
			if _, err := os.Stat(filepath.Join(tdir, "argv")); err != nil {
				continue
			}
			claim, err := os.OpenFile(filepath.Join(tdir, "claimed"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
			if err != nil {
				continue
			}
			claim.Close()
			cwd, _ := os.ReadFile(filepath.Join(tdir, "cwd"))
			holder := exec.Command(self, holdCommand, address)
			holder.Args[0] = os.Args[0]
			holder.Dir = string(cwd)
			holder.Env = os.Environ()
			holder.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			if err := holder.Start(); err != nil {
				os.WriteFile(filepath.Join(tdir, "exit"), []byte("127"), 0o666)
				continue
			}
			go holder.Wait()
		}
	}
}

// status prints the pid of the core that runs, and of the holder of
// each terminal whose program runs.
func status(dir string, stdout io.Writer) error {
	pid := livePID(dir)
	if pid <= 0 {
		return errors.New("no core")
	}
	holders := []map[string]any{}
	for _, address := range (&HC{dir: dir}).Terminals() {
		tdir := filepath.Join(dir, address)
		if _, err := os.Stat(filepath.Join(tdir, "exit")); err == nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(tdir, "holder"))
		if n, _ := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && n > 0 {
			holders = append(holders, map[string]any{"pid": n})
		}
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"pid": pid, "holders": holders})
}
