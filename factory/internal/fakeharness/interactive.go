package fakeharness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// The interactive form: `claude` with no -p, in a terminal where a person
// types (a take-over). The Harness writes its screen to stdout as lines:
// first the init event as JSON (with its tools, the permission mode, the
// Agent session id and the MCP servers of --mcp-config), then the text of each turn. Each line on stdin
// is what the person types, and ends a turn:
//
//	!COMMAND   runs COMMAND with sh -c in the working directory, as Claude
//	           Code's bash mode does (such as !solidtrace report ...)
//	/exit      ends the Agent session, with exit code 0
//	TEXT       a person message: a user line in the transcript, and the
//	           reply "Done."
//
// After each turn, the Harness runs the Stop hooks of --settings, as Claude
// Code does; a refusal shows "Stop hook feedback" on the screen and in the
// transcript, and the agent goes on (at most 10 times). When stdin closes,
// the Agent session ends with exit code 0.
func interactive(f flags, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	cwd, _ := os.Getwd()
	h := &harness{flags: f, cwd: cwd, out: stdout}
	if err := h.openTranscript(getenv); err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	defer h.transcript.Close()
	tools := []any{}
	for _, t := range []string{"Bash", "Edit", "Read", "AskUserQuestion"} {
		if !f.disallowed[t] {
			tools = append(tools, t)
		}
	}
	h.write(map[string]any{"type": "system", "subtype": "init", "form": "interactive", "session_id": h.sessionID,
		"cwd": cwd, "permissionMode": f.permissionMode, "tools": tools, "resumed": f.resume != "",
		"mcp_servers": mcpServers(f.mcpConfig)})
	screen := func(text string) {
		h.outMu.Lock()
		defer h.outMu.Unlock()
		fmt.Fprintln(stdout, text)
	}
	sc := bufio.NewScanner(stdin)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
			continue
		case line == "/exit":
			return 0
		case strings.HasPrefix(line, "!"):
			cmd := exec.Command("sh", "-c", line[1:])
			cmd.Dir = cwd
			out, err := cmd.CombinedOutput()
			screen(strings.TrimRight(string(out), "\n"))
			if err != nil {
				screen("exit: " + err.Error())
			}
		default:
			id := newUUID()
			h.writeTranscript(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": line},
				"uuid": id})
			h.writeTranscript(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant",
				"content": []any{map[string]any{"type": "text", "text": "Done."}}}, "uuid": newUUID()})
			screen("Done.")
		}
		for refusals := 0; refusals < maxStopRefusals; refusals++ {
			reason, refused := h.runStopHooks(refusals > 0, stderr)
			if !refused {
				break
			}
			screen("Stop hook feedback:\n" + reason)
			h.writeTranscript(map[string]any{"type": "user", "isMeta": true, "message": map[string]any{"role": "user",
				"content": "Stop hook feedback:\n" + reason}, "uuid": newUUID()})
		}
	}
	return 0
}

// mcpServers are the servers of the MCP configuration file, as Claude
// Code's init event names them: "connected" for each server of the file,
// and "failed" for a file that the harness cannot read.
func mcpServers(file string) []any {
	servers := []any{}
	if file == "" {
		return servers
	}
	var config struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	b, err := os.ReadFile(file)
	if err == nil {
		err = json.Unmarshal(b, &config)
	}
	if err != nil {
		return append(servers, map[string]any{"name": file, "status": "failed"})
	}
	for name := range config.MCPServers {
		servers = append(servers, map[string]any{"name": name, "status": "connected"})
	}
	return servers
}
