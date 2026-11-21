package wrapper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The work-in-progress save (the spec's V4) and the checkpoint (V5). One
// save serves both: a commit of the checkout's working tree, with HEAD as
// its parent, force-pushed to the Run's work-in-progress ref. The commit
// is made in a git directory of the save's own, with its own index, so
// the agent's index, HEAD, branch and objects stay as they are, and git
// reads no configuration of the agent's (no fsmonitor, no filter and no
// hook of the checkout runs).
//
// Each git command of the save runs as the run user: it reads the
// working tree with the agent's rights, never with root's. The
// checkpoint runs while the agent works, so its commands are in the
// Execution's kill boundary; the preemption handler's save runs after
// the halt removed the boundary. The push is the wrapper's, as the push
// before the end is, with the forge token in a header.

// reasonCheckpoint is the reason of a checkpoint's save.
const reasonCheckpoint = "checkpoint"

// The limits of the untracked files that a save keeps: each file of
// untrackedFileLimit or less, until untrackedTotalLimit in total. The save
// leaves out the others, and names them.
const (
	untrackedFileLimit  = 5 << 20
	untrackedTotalLimit = 50 << 20
)

// submoduleDepth is the deepest submodule that a save saves (1 is a
// submodule of the checkout).
const submoduleDepth = 3

// checkpointWait is the limit of one checkpoint.
const checkpointWait = 2 * time.Minute

// plainRunID is a Run id that can be part of a ref name.
var plainRunID = regexp.MustCompile(`^[a-z0-9]{1,64}$`)

// wipRef is the Run's work-in-progress ref, as the task gives it: the
// Forge's place, refs/agent-wip or refs/heads/agent-wip. It refuses any
// other ref, so that no save goes to a branch of the repository.
func (e *execution) wipRef() (string, error) {
	if !plainRunID.MatchString(e.task.RunID) {
		return "", fmt.Errorf("the Run id %q cannot be part of a ref name", e.task.RunID)
	}
	switch ref := e.task.WIPRef; ref {
	case "":
		return "refs/agent-wip/" + e.task.RunID, nil
	case "refs/agent-wip/" + e.task.RunID, "refs/heads/agent-wip/" + e.task.RunID:
		return ref, nil
	default:
		return "", fmt.Errorf("the task's work-in-progress ref %q is not refs/agent-wip/%s or refs/heads/agent-wip/%s",
			ref, e.task.RunID, e.task.RunID)
	}
}

// checkpoints runs the checkpoint each interval of the task, on
// cfg.clock from start, until ctx ends. A checkpoint saves only when the
// work changed since the last save. On standard VMs too: a machine can
// go with no notice.
func (e *execution) checkpoints(ctx context.Context, start time.Time) {
	every := time.Duration(e.task.CheckpointSeconds) * time.Second
	if every <= 0 {
		fmt.Fprintln(e.log, "wrapper: the checkpoint is off")
		return
	}
	check := e.cfg.limitEvery
	if check <= 0 {
		check = limitEvery
	}
	tick := time.NewTicker(check)
	defer tick.Stop()
	next := start.Add(every)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if e.cfg.clock().Before(next) {
			continue
		}
		call, cancel := context.WithTimeout(ctx, checkpointWait)
		saved, pushed, err := e.saver.checkpoint(call)
		cancel()
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			fmt.Fprintln(e.log, "wrapper: the checkpoint:", err)
		case pushed:
			fmt.Fprintf(e.log, "wrapper: the checkpoint saved the work in %s (%d files left out)\n", saved.Commit,
				len(saved.Skipped))
		default:
			fmt.Fprintln(e.log, "wrapper: the checkpoint: no change since the last save")
		}
		next = e.cfg.clock().Add(every)
	}
}

// wipSaver is the work-in-progress save of an Execution's checkout.
type wipSaver struct {
	e *execution
	// mu holds one save at a time: a checkpoint's push never comes after
	// the preemption handler's.
	mu sync.Mutex
	// last is the content of the last save that was pushed ("": none).
	last string
	// at is the time of the last checkpoint (zero: none).
	atMu sync.Mutex
	at   time.Time
}

func newWIPSaver(e *execution) *wipSaver { return &wipSaver{e: e} }

func (s *wipSaver) lastCheckpoint() time.Time {
	s.atMu.Lock()
	defer s.atMu.Unlock()
	return s.at
}

// save is the preemption handler's save. It always pushes.
func (s *wipSaver) save(ctx context.Context, reason string) (savedWork, error) {
	saved, _, err := s.run(ctx, reason, false)
	return saved, err
}

// checkpoint saves the work when it changed since the last save. It is
// true when it pushed.
func (s *wipSaver) checkpoint(ctx context.Context) (savedWork, bool, error) {
	return s.run(ctx, reasonCheckpoint, true)
}

func (s *wipSaver) run(ctx context.Context, reason string, checkpoint bool) (savedWork, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.e
	if e.checkout == "" {
		return savedWork{}, false, errors.New("the setup made no checkout, so there is no work to save")
	}
	ref, err := e.wipRef()
	if err != nil {
		return savedWork{}, false, err
	}
	git := s.gitCommand(checkpoint)
	root := &repoSave{work: e.checkout, gitDir: filepath.Join(e.checkout, ".git"), pushURL: e.task.CloneURL}
	defer root.remove()
	if info, err := os.Lstat(root.gitDir); err != nil || !info.IsDir() {
		return savedWork{}, false, errors.New("the checkout has no .git folder")
	}
	if err := s.snapshot(ctx, git, root, 0); err != nil {
		return savedWork{}, false, err
	}
	key := root.key()
	if checkpoint && (key == s.last || s.last == "" && root.tree == root.headTree && root.head == e.startTip && !root.subsChanged()) {
		return savedWork{}, false, nil
	}
	now := e.clock.Now().UTC().Truncate(time.Second)
	message := fmt.Sprintf("WIP [skip ci]: saved by the factory at %s, Run %s, Execution %s, reason %s. Not reviewed.",
		now.Format(time.RFC3339), e.task.RunID, e.task.ExecutionID, reason)
	var skipped []string
	// The submodules first, then the checkout, each to the same ref name.
	var saveSubs func(r *repoSave)
	saveSubs = func(r *repoSave) {
		for _, sub := range r.subs {
			saveSubs(sub)
			switch {
			case sub.notSaved == "" && sub.changed():
				if err := s.commitAndPush(ctx, git, sub, message, ref, now); err != nil {
					sub.notSaved = err.Error()
				}
			}
			if sub.notSaved != "" {
				fmt.Fprintf(e.log, "wrapper: the save of the submodule %s: %s\n", sub.path, sub.notSaved)
				skipped = append(skipped, sub.path+" (a submodule that is not saved: "+sub.notSaved+")")
			}
		}
	}
	saveSubs(root)
	skipped = append(root.allSkipped(), skipped...)
	// The checkout's commit names what the save left out, so that a
	// restore can say what did not come back.
	if err := s.commitAndPush(ctx, git, root, message+leftOutText(skipped), ref, now); err != nil {
		return savedWork{}, false, err
	}
	s.last = key
	if checkpoint {
		s.atMu.Lock()
		s.at = e.clock.Now()
		s.atMu.Unlock()
	}
	return savedWork{Commit: root.commit, Skipped: skipped, parent: root.head}, true, nil
}

// repoSave is the save of one repository: the checkout or a submodule.
type repoSave struct {
	// path is the submodule's folder in the checkout ("" is the checkout).
	path string
	// work is the working tree, gitDir the agent's git directory, and
	// pushURL the clone URL that the save pushes to.
	work, gitDir, pushURL string
	// dir is the save's own folder, with its git directory (repo).
	dir string
	env []string
	// head is the agent's HEAD, headTree its tree, and tree the tree of
	// the working tree. recorded is the commit that the parent's HEAD
	// records for a submodule.
	head, headTree, tree, recorded string
	skipped                        []string
	subs                           []*repoSave
	// notSaved is why a submodule is not saved ("": it is).
	notSaved string
	commit   string
}

func (r *repoSave) remove() {
	for _, sub := range r.subs {
		sub.remove()
	}
	if r.dir != "" {
		removeAll(r.dir)
	}
}

func (r *repoSave) key() string {
	k := r.head + " " + r.tree
	for _, sub := range r.subs {
		k += " [" + sub.path + " " + sub.key() + " " + sub.notSaved + "]"
	}
	return k
}

// changed tells if a submodule has work that its remote can lack: a
// working tree that is not its HEAD, or a HEAD that is not the commit
// that the parent records.
func (r *repoSave) changed() bool {
	return r.tree != r.headTree || r.head != r.recorded || r.subsChanged()
}

func (r *repoSave) subsChanged() bool {
	return slices.ContainsFunc(r.subs, func(sub *repoSave) bool { return sub.notSaved != "" || sub.changed() })
}

// allSkipped are the files that the save of r and of its submodules left
// out, as paths in the checkout.
func (r *repoSave) allSkipped() []string {
	out := slices.Clone(r.skipped)
	for _, sub := range r.subs {
		out = append(out, sub.allSkipped()...)
	}
	return out
}

// gitCommand runs one git command of the save as the run user, with env,
// and gives its standard output. In the kill boundary when inBoundary.
type gitCommand func(ctx context.Context, env []string, stdin []byte, args ...string) (string, error)

func (s *wipSaver) gitCommand(inBoundary bool) gitCommand {
	e := s.e
	return func(ctx context.Context, env []string, stdin []byte, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir, cmd.Env = "/", env
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
		cmd.WaitDelay = gitWait
		if stdin != nil {
			cmd.Stdin = bytes.NewReader(stdin)
		}
		e.runUser.start(cmd)
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		var err error
		if inBoundary {
			err = e.boundary.Start(cmd)
		} else {
			err = cmd.Start()
		}
		if err == nil {
			if err = cmd.Wait(); errors.Is(err, exec.ErrWaitDelay) {
				err = nil
			}
		}
		if err != nil {
			return out.String(), fmt.Errorf("git %s: %v: %s", args[0], err, lastLines(errOut.String(), 5))
		}
		return out.String(), nil
	}
}

// snapshot makes the save's tree of r and of each submodule in it, with
// no commit and no push.
func (s *wipSaver) snapshot(ctx context.Context, git gitCommand, r *repoSave, depth int) error {
	head, err := readHead(r.gitDir)
	if err != nil {
		return err
	}
	r.head = head
	if err := s.prepare(r); err != nil {
		return err
	}
	repo := filepath.Join(r.dir, "repo")
	// No template: the info/exclude of prepare stays.
	init := []string{"init", "--quiet", "--bare", "--template="}
	if len(head) == 64 {
		init = append(init, "--object-format=sha256")
	}
	if _, err := git(ctx, r.env, nil, append(init, repo)...); err != nil {
		return err
	}
	r.env = append(r.env, "GIT_DIR="+repo, "GIT_WORK_TREE="+r.work, "GIT_INDEX_FILE="+filepath.Join(repo, "index"),
		"GIT_ALTERNATE_OBJECT_DIRECTORIES="+filepath.Join(r.gitDir, "objects"))
	run := func(stdin []byte, args ...string) (string, error) { return git(ctx, r.env, stdin, args...) }
	if _, err := run(nil, "read-tree", head); err != nil {
		return err
	}
	tree, err := run(nil, "rev-parse", head+"^{tree}")
	if err != nil {
		return err
	}
	r.headTree = strings.TrimSpace(tree)

	untracked, err := run(nil, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return err
	}
	ignored, err := run(nil, "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory")
	if err != nil {
		return err
	}
	prefix := ""
	if r.path != "" {
		prefix = r.path + "/"
	}
	// git can name an ignored folder and a file in it: the folder is
	// enough.
	names := splitNUL(ignored)
	slices.Sort(names)
	dir := ""
	for _, name := range names {
		if dir != "" && strings.HasPrefix(name, dir) {
			continue
		}
		if strings.HasSuffix(name, "/") {
			dir = name
		}
		r.skipped = append(r.skipped, prefix+name)
	}
	var keep []byte
	var total int64
	full := false
	for _, name := range splitNUL(untracked) {
		var size int64
		info, err := os.Lstat(filepath.Join(r.work, name))
		if err == nil {
			size = info.Size()
		}
		// A folder is a repository of its own in the working tree.
		if strings.HasSuffix(name, "/") || size > untrackedFileLimit || full || total+size > untrackedTotalLimit {
			full = full || size <= untrackedFileLimit && total+size > untrackedTotalLimit
			r.skipped = append(r.skipped, prefix+name)
			continue
		}
		total += size
		keep = append(append(keep, name...), 0)
	}
	if _, err := run(nil, "add", "--update"); err != nil {
		return err
	}
	if len(keep) > 0 {
		if _, err := run(keep, "add", "--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
			return err
		}
	}
	if err := s.submodules(ctx, git, r, depth); err != nil {
		return err
	}
	tree, err = run(nil, "write-tree")
	if err != nil {
		return err
	}
	r.tree = strings.TrimSpace(tree)
	return nil
}

// prepare makes the save's own folder in the Execution's directory: root
// writes the patterns that the save leaves out in it, and the run user
// gets its git directory.
func (s *wipSaver) prepare(r *repoSave) error {
	e := s.e
	dir, err := os.MkdirTemp(e.cfg.dir, "save-")
	if err != nil {
		return err
	}
	r.dir = dir
	if err := os.Chmod(dir, 0o711); err != nil {
		return err
	}
	// The factory file's patterns go in the info/exclude of the save's
	// git directory, which root writes before the run user gets the
	// directory. The agent's own .git/info/exclude, which git does not
	// read from another git directory, is the save's core.excludesFile:
	// git reads it as the run user, so the wrapper reads no file that the
	// agent can change (a link to a file of root's gives nothing). The
	// factory file's patterns win over the agent's.
	repo := filepath.Join(dir, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(repo, "info"), 0o755); err != nil {
		return err
	}
	exclude := filepath.Join(repo, "info", "exclude")
	if err := os.WriteFile(exclude, []byte(strings.Join(e.task.SaveExclude, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.Chmod(exclude, 0o644); err != nil {
		return err
	}
	if err := e.runUser.give(repo); err != nil {
		return err
	}
	for _, kv := range e.runUser.env(harnessEnv(e.environ, "")) {
		if !strings.HasPrefix(kv, "GIT_") && !strings.HasPrefix(kv, "HOME=") {
			r.env = append(r.env, kv)
		}
	}
	// No configuration but the save's own: not the system's, not the run
	// user's and not the checkout's.
	r.env = append(r.env, "HOME="+dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_LITERAL_PATHSPECS=1",
		"GIT_CONFIG_COUNT=3",
		"GIT_CONFIG_KEY_0=core.excludesFile", "GIT_CONFIG_VALUE_0="+filepath.Join(r.gitDir, "info", "exclude"),
		"GIT_CONFIG_KEY_1=core.fsmonitor", "GIT_CONFIG_VALUE_1=false",
		"GIT_CONFIG_KEY_2=core.untrackedCache", "GIT_CONFIG_VALUE_2=false",
		"GIT_AUTHOR_NAME="+commitName, "GIT_AUTHOR_EMAIL="+commitEmail,
		"GIT_COMMITTER_NAME="+commitName, "GIT_COMMITTER_EMAIL="+commitEmail)
	return nil
}

// submodules makes the save of each submodule of r that has a checkout,
// from the .gitmodules file of r's working tree.
func (s *wipSaver) submodules(ctx context.Context, git gitCommand, r *repoSave, depth int) error {
	modules := filepath.Join(r.work, ".gitmodules")
	if info, err := os.Lstat(modules); err != nil || !info.Mode().IsRegular() {
		return nil
	}
	out, err := git(ctx, r.env, nil, "config", "--null", "--file", modules, "--get-regexp", `^submodule\..*\.(path|url)$`)
	if err != nil {
		// No path and no URL in the file.
		if strings.Contains(err.Error(), "exit status 1") {
			return nil
		}
		return err
	}
	type module struct{ path, url string }
	var names []string
	byName := map[string]*module{}
	for _, entry := range splitNUL(out) {
		key, value, _ := strings.Cut(entry, "\n")
		rest, _ := strings.CutPrefix(key, "submodule.")
		dot := strings.LastIndex(rest, ".")
		if dot < 0 {
			continue
		}
		name := rest[:dot]
		m := byName[name]
		if m == nil {
			m = &module{}
			byName[name] = m
			names = append(names, name)
		}
		if rest[dot+1:] == "path" {
			m.path = value
		} else {
			m.url = value
		}
	}
	prefix := ""
	if r.path != "" {
		prefix = r.path + "/"
	}
	for _, name := range names {
		m := byName[name]
		if m.path == "" || !plainParts(strings.Split(m.path, "/")) {
			continue
		}
		recorded, err := git(ctx, r.env, nil, "ls-tree", "-z", r.head, "--", m.path)
		if err != nil {
			return err
		}
		fields := strings.Fields(strings.TrimSuffix(recorded, "\x00"))
		if len(fields) < 3 || fields[0] != "160000" {
			continue
		}
		sub := &repoSave{path: prefix + m.path, work: filepath.Join(r.work, m.path), recorded: fields[2]}
		gitDir, ok := submoduleGitDir(sub.work)
		if !ok {
			// Not checked out: nothing to save.
			continue
		}
		sub.gitDir = gitDir
		r.subs = append(r.subs, sub)
		if depth+1 > submoduleDepth {
			sub.notSaved = fmt.Sprintf("it is deeper than %d submodules", submoduleDepth)
			continue
		}
		if sub.pushURL, err = s.e.submoduleURL(m.url, r.pushURL); err != nil {
			sub.notSaved = err.Error()
			continue
		}
		if err := s.snapshot(ctx, git, sub, depth+1); err != nil {
			sub.notSaved = err.Error()
		}
	}
	return nil
}

// submoduleGitDir is the git directory of a submodule's checkout: its
// .git folder, or the folder that its .git file names.
func submoduleGitDir(work string) (string, bool) {
	dotGit := filepath.Join(work, ".git")
	info, err := os.Lstat(dotGit)
	switch {
	case err != nil:
		return "", false
	case info.IsDir():
		return dotGit, true
	case !info.Mode().IsRegular() || info.Size() > 4096:
		return "", false
	}
	b, err := os.ReadFile(dotGit)
	if err != nil {
		return "", false
	}
	dir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
	if !ok || dir == "" {
		return "", false
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(work, dir)
	}
	return dir, true
}

// submoduleURL is the clone URL of a submodule, from its URL in
// .gitmodules: git over ssh to the repository's host is rewritten to the
// forge's git URL, as the run user's git configuration does, and a
// relative URL is relative to the parent's URL. The forge token goes
// only to the forge, and no push goes to a local path: a submodule whose
// URL is not on the forge's git URL is not saved.
func (e *execution) submoduleURL(raw, parent string) (string, error) {
	gitURL := strings.TrimRight(e.task.GitURL, "/")
	if gitURL == "" {
		return "", errors.New("the task names no git URL of the forge")
	}
	u := raw
	host, _, _ := strings.Cut(e.task.Repository, "/")
	for _, ssh := range []string{"git@" + host + ":", "ssh://git@" + host + "/"} {
		if rest, ok := strings.CutPrefix(u, ssh); ok {
			u = gitURL + "/" + rest
		}
	}
	if strings.HasPrefix(u, "./") || strings.HasPrefix(u, "../") {
		base, err := url.Parse(strings.TrimRight(parent, "/") + "/")
		if err != nil {
			return "", fmt.Errorf("its URL %q is not on the forge", raw)
		}
		ref, err := url.Parse(u)
		if err != nil {
			return "", fmt.Errorf("its URL %q is not on the forge", raw)
		}
		u = base.ResolveReference(ref).String()
	}
	parsed, err := url.Parse(u)
	if err != nil || strings.ContainsAny(u, "\n\x00\\") || strings.Contains(u, "..") || parsed.User != nil ||
		!strings.HasPrefix(u, gitURL+"/") {
		return "", fmt.Errorf("its URL %q is not on the forge", raw)
	}
	return u, nil
}

// commitAndPush makes the WIP commit of r, with HEAD as its parent, and
// force-pushes it to ref.
func (s *wipSaver) commitAndPush(ctx context.Context, git gitCommand, r *repoSave, message, ref string, now time.Time) error {
	date := now.Format(time.RFC3339)
	env := append(slices.Clone(r.env), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	commit, err := git(ctx, env, nil, "commit-tree", r.tree, "-p", r.head, "-m", message)
	if err != nil {
		return err
	}
	r.commit = strings.TrimSpace(commit)
	push, done, err := s.e.pushRepository(ctx, s.e.environ, filepath.Join(r.dir, "repo", "objects"),
		filepath.Join(r.gitDir, "objects"))
	if err != nil {
		return err
	}
	defer done()
	if err := push("push", "--quiet", "--no-verify", "--force", "--", r.pushURL, r.commit+":"+ref); err != nil {
		return fmt.Errorf("the push to %s failed: %w", ref, err)
	}
	return nil
}

// leftOutHeading starts the part of a WIP commit's message that names
// the files that the save left out, one on each line.
const leftOutHeading = "Left out of the save:"

// leftOutText is the part of a WIP commit's message that names the
// skipped files ("" when there are none): the first maxSkipped names,
// each name with a control character in Go's quotes, and the count of the
// others.
func leftOutText(skipped []string) string {
	if len(skipped) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n" + leftOutHeading + "\n")
	for i, name := range skipped {
		if i == maxSkipped {
			fmt.Fprintf(&b, "(and %d more)\n", len(skipped)-maxSkipped)
			break
		}
		if strings.ContainsFunc(name, func(r rune) bool { return r < ' ' || r == 0x7f }) {
			name = strconv.Quote(name)
		}
		b.WriteString(name + "\n")
	}
	return b.String()
}

// splitNUL splits git's -z output.
func splitNUL(s string) []string {
	var out []string
	for _, part := range strings.Split(s, "\x00") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
