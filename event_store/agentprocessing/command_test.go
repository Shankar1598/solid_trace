package agentprocessing

import (
	"encoding/json"
	"testing"
)

// The lines that Claude Code writes for a slash command, as /compact
// writes them: the person's input, a caveat for the model, the command
// with its arguments, and the command's output. The person typed the
// command; the caveat and the output are Claude Code's.
var compactLines = []struct {
	name, line, role string
}{
	{"the input", `{"type":"user","uuid":"c0","timestamp":"2026-11-11T15:00:18.522Z","message":{"role":"user","content":"/compact"}}`, rolePerson},
	{"the caveat", `{"type":"user","uuid":"c1","isMeta":true,"timestamp":"2026-11-11T15:00:18.532Z","message":{"role":"user","content":"<local-command-caveat>The command below was run directly in Claude Code, not by you.</local-command-caveat>"}}`, roleNotPerson},
	{"the command", `{"type":"user","uuid":"c2","timestamp":"2026-11-11T15:00:18.532Z","message":{"role":"user","content":"<command-name>/compact</command-name>\n            <command-message>compact</command-message>\n            <command-args></command-args>"}}`, rolePerson},
	{"the output", `{"type":"user","uuid":"c3","timestamp":"2026-11-11T15:00:54.478Z","message":{"role":"user","content":"<local-command-stdout>\u001b[2mCompacted (ctrl+o to see full summary)\u001b[22m</local-command-stdout>"}}`, roleNotPerson},
	{"an error output", `{"type":"user","uuid":"c4","timestamp":"2026-11-11T15:00:55Z","message":{"role":"user","content":"<local-command-stderr>Error: no conversation to compact</local-command-stderr>"}}`, roleNotPerson},
	{"an output in a text block", `{"type":"user","uuid":"c5","timestamp":"2026-11-11T15:00:56Z","message":{"role":"user","content":[{"type":"text","text":"<local-command-stdout>Set model to Opus</local-command-stdout>"}]}}`, roleNotPerson},
}

// The lines that Claude Code writes for a shell command (`! ls`): the
// person's command, and its output with the terminal's colour codes.
var shellLines = []struct {
	name, line, role string
}{
	{"the shell command", `{"type":"user","uuid":"b0","timestamp":"2026-11-25T13:55:00Z","message":{"role":"user","content":"<bash-input>terraform apply</bash-input>"}}`, rolePerson},
	{"the shell output", `{"type":"user","uuid":"b1","timestamp":"2026-11-25T13:55:07Z","message":{"role":"user","content":"<bash-stdout>\u001b[0m\u001b[1mRefreshing state...\u001b[0m</bash-stdout><bash-stderr>Pseudo-terminal will not be allocated.</bash-stderr>"}}`, roleNotPerson},
	{"an empty shell output", `{"type":"user","uuid":"b2","timestamp":"2026-11-25T13:55:08Z","message":{"role":"user","content":"<bash-stdout></bash-stdout><bash-stderr></bash-stderr>"}}`, roleNotPerson},
}

func TestTheOutputOfASlashCommandIsNotFromAPerson(t *testing.T) {
	for _, c := range append(compactLines, shellLines...) {
		t.Run(c.name, func(t *testing.T) {
			object, _ := lineObject([]byte(c.line), map[string]string{}, false)
			var role string
			_ = json.Unmarshal(object["agent.role"], &role)
			if role != c.role {
				t.Errorf("Expected the role %q, got %q", c.role, role)
			}
		})
	}
}
