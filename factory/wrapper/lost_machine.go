package wrapper

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The restore after a lost machine (the spec's V6). When the machine of
// the Run's last Execution went, the task has a Restore, and the setup
// puts the work-in-progress save W back into the new checkout: the Run's
// branch at W's parent P (the HEAD at the save), the working tree equal
// to W's tree, and the changes not staged. So no WIP commit is in the
// branch's history. When the remote branch has commits that P does not
// have (a person pushed), the person's push wins: no restore, and the
// first message says where the saved work is. The first message also
// says when the machine went and what did not come back.
//
// The restore uses only a save that one of the lost Executions made: a
// save of an older Execution is not the Run's last work. Each git
// command runs as the run user in the kill boundary, before the agent's
// first process, in the clone that the setup made.

// savedWorkRef is the local ref of the save in the checkout, when the
// restore does not put it in the working tree.
const savedWorkRef = "refs/factory/saved-work"

// wipSubject is the first line of a WIP commit's message (see
// wipSaver.run).
var wipSubject = regexp.MustCompile(`^WIP \[skip ci\]: saved by the factory at (\S+), Run (\S+), Execution (\S+), ` +
	`reason ([a-z ]+)\. Not reviewed\.$`)

// restoreRetryWait is the wait before the second try of a git command
// that reaches the forge.
const restoreRetryWait = 2 * time.Second

// foundSave is the work-in-progress save that a restore found.
type foundSave struct {
	commit, parent string
	at             string
	reason         string
	leftOut        []string
	submodules     bool
}

// restoreWork restores the save of the lost machine into the checkout,
// and makes the first message's note. pushed is true when the clone is of
// the remote Run's branch (at e.startTip). An error fails the setup: an
// Execution that starts with no saved work while the save is on the forge
// could overwrite the save with its first checkpoint.
func (e *execution) restoreWork(ctx context.Context, environ []string, pushed bool) error {
	r := e.task.Restore
	if r == nil {
		return nil
	}
	ref, err := e.wipRef()
	if err != nil {
		e.lostNote = lostNote(r, e.notRestored("the Run has no work-in-progress ref ("+err.Error()+")"))
		return nil
	}
	save, err := e.findSave(ctx, environ, ref, r.Executions)
	if err != nil {
		return fmt.Errorf("restore the work-in-progress save from %s: %w", ref, err)
	}
	if save == nil {
		e.lostNote = lostNote(r, e.notRestored("no save of the lost Execution is in "+ref))
		return nil
	}
	if pushed {
		_, code, err := e.git(ctx, environ, e.checkout, "merge-base", "--is-ancestor", e.startTip, save.parent)
		if code == 1 {
			fmt.Fprintf(e.log, "wrapper: a person pushed to %s after the save %s: no restore\n", e.task.Branch, save.commit)
			e.lostNote = lostNote(r, e.personPushed(save, ref))
			return nil
		}
		if err != nil {
			return fmt.Errorf("compare the Run's branch with the save: %w", err)
		}
	}
	for _, args := range [][]string{
		// The branch at P, with the index and the working tree as they are.
		{"reset", "--quiet", "--soft", save.parent},
		// The index and the working tree equal to W: each file of the
		// clone that W does not have goes.
		{"read-tree", "-u", "--reset", save.commit},
		// The index at P again: the changes after P are not staged.
		{"reset", "--quiet", save.parent},
		{"update-ref", "-d", savedWorkRef},
	} {
		if _, _, err := e.git(ctx, environ, e.checkout, args...); err != nil {
			return fmt.Errorf("restore the save %s: %w", save.commit, err)
		}
	}
	fmt.Fprintf(e.log, "wrapper: restored the save %s: the Run's branch is at %s\n", save.commit, save.parent)
	e.lostNote = lostNote(r, e.restored(save, ref))
	return nil
}

// findSave finds the save on the forge, fetches it to savedWorkRef, and
// gives it when one of executions made it (nil: none).
func (e *execution) findSave(ctx context.Context, environ []string, ref string, executions []string) (*foundSave, error) {
	out, code, err := e.gitTwice(ctx, environ, "ls-remote", "--exit-code", "origin", ref)
	if code == 2 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	commit := ""
	for _, line := range strings.Split(out, "\n") {
		if id, name, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok && name == ref && objectID(id) {
			commit = id
		}
	}
	if commit == "" {
		return nil, nil
	}
	if _, _, err := e.gitTwice(ctx, environ, "fetch", "--quiet", "--no-tags", "--no-recurse-submodules",
		"--no-write-fetch-head", "origin", "+"+ref+":"+savedWorkRef); err != nil {
		return nil, err
	}
	raw, _, err := e.git(ctx, environ, e.checkout, "cat-file", "commit", savedWorkRef)
	if err != nil {
		return nil, err
	}
	header, message, _ := strings.Cut(raw, "\n\n")
	save := &foundSave{}
	for _, line := range strings.Split(header, "\n") {
		if p, ok := strings.CutPrefix(line, "parent "); ok {
			if save.parent != "" {
				save.parent = ""
				break
			}
			save.parent = p
		}
		if id, ok := strings.CutPrefix(line, "tree "); ok && !objectID(id) {
			return nil, fmt.Errorf("the save's tree %q is not an object id", id)
		}
	}
	subject, body, _ := strings.Cut(message, "\n")
	m := wipSubject.FindStringSubmatch(subject)
	if m == nil || m[2] != e.task.RunID || !slices.Contains(executions, m[3]) || !objectID(save.parent) {
		fmt.Fprintf(e.log, "wrapper: %s is not a save of the lost Execution: %q\n", ref, subject)
		_, _, _ = e.git(ctx, environ, e.checkout, "update-ref", "-d", savedWorkRef)
		return nil, nil
	}
	id, _, err := e.git(ctx, environ, e.checkout, "rev-parse", "--verify", savedWorkRef+"^{commit}")
	if err != nil {
		return nil, err
	}
	save.commit, save.at, save.reason, save.leftOut = strings.TrimSpace(id), m[1], m[4], leftOut(body)
	tree, _, err := e.git(ctx, environ, e.checkout, "ls-tree", "-r", "-z", save.commit)
	if err != nil {
		return nil, err
	}
	for _, entry := range splitNUL(tree) {
		if strings.HasPrefix(entry, "160000 ") {
			save.submodules = true
		}
	}
	return save, nil
}

// gitTwice runs git in the checkout, and runs it a second time after a
// failure that is not code 2 (ls-remote's "no such ref"): a call to the
// forge can fail one time.
func (e *execution) gitTwice(ctx context.Context, environ []string, args ...string) (string, int, error) {
	out, code, err := e.git(ctx, environ, e.checkout, args...)
	if err == nil || code == 2 {
		return out, code, err
	}
	select {
	case <-ctx.Done():
		return out, code, err
	case <-time.After(restoreRetryWait):
	}
	return e.git(ctx, environ, e.checkout, args...)
}

// leftOut are the names of a WIP commit's message after leftOutHeading.
func leftOut(body string) []string {
	_, list, ok := strings.Cut(body, "\n"+leftOutHeading+"\n")
	if !ok {
		return nil
	}
	var names []string
	for _, line := range strings.Split(list, "\n") {
		if line == "" {
			continue
		}
		if unquoted, err := strconv.Unquote(line); err == nil && strings.HasPrefix(line, `"`) {
			line = unquoted
		}
		names = append(names, line)
	}
	return names
}

// The parts of the first message after a lost machine.

func (e *execution) restored(s *foundSave, ref string) string {
	when := "when the machine went"
	if s.reason == reasonCheckpoint {
		when = "at the last checkpoint"
	}
	text := fmt.Sprintf("Your work is back as the factory saved it at %s, %s. The branch %s is at %s, the commit "+
		"that was HEAD at the save, and the changes after that commit are in the working tree, not staged. "+
		"Changes after the save are not back.", s.at, when, e.task.Branch, short(s.parent))
	if len(s.leftOut) > 0 {
		text += " The save left out these files, so they are not back: " + strings.Join(s.leftOut, ", ") + "."
	}
	if s.submodules {
		text += " The submodules are as the clone gives them: a submodule's saved changes are in " + ref +
			" of its own repository, and are not back."
	}
	return text
}

func (e *execution) personPushed(s *foundSave, ref string) string {
	return fmt.Sprintf("Your saved work is not back: a person pushed to the branch %s after the save, and a "+
		"person's push wins. The checkout is the branch as it is on the remote. The saved work (from %s) is the "+
		"commit %s: the ref %s in this checkout, and %s on the remote until the next save. Look at it with "+
		"'git diff HEAD %s', and take what is still necessary.", e.task.Branch, s.at, s.commit, savedWorkRef,
		ref, savedWorkRef)
}

func (e *execution) notRestored(why string) string {
	return "No saved work is back: " + why + ". The checkout is the branch " + e.task.Branch +
		" as it is on the remote, or a new branch: the changes that were not pushed are not back."
}

// lostNote is the first message after a lost machine.
func lostNote(r *dispatcherapi.Restore, back string) string {
	how := map[string]string{dispatcherapi.EndPreempted: "was preempted (the cloud took it back)",
		dispatcherapi.EndMachineStopped: "stopped", dispatcherapi.MachineLost: "was lost with no notice"}[r.Reason]
	if how == "" {
		how = "went (" + r.Reason + ")"
	}
	return fmt.Sprintf("A message from the factory: the machine of this Run's last Execution %s at %s. This is a "+
		"new Execution on a new machine. %s Containers, background processes, caches and the files that "+
		".gitignore excludes are gone: make them again if the work needs them. The last tool call possibly did not "+
		"finish: look at its result before you continue.", how, r.LostAt.UTC().Format(time.RFC3339), back)
}

// short is the first 12 characters of a commit id.
func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

