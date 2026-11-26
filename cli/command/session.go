package command

import (
	"flag"
	"strings"
)

// ClaudeSessionEnv is the environment variable in which Claude Code gives
// its Bash tool the id of the Agent session.
const ClaudeSessionEnv = "CLAUDE_CODE_SESSION_ID"

// sessionFlag is the --session flag of a command that acts for an Agent
// session.
func sessionFlag(flags *flag.FlagSet) *string {
	return flags.String("session", "", "the Agent session (default: "+ClaudeSessionEnv+")")
}

// agentSessionID is the --session value, else the Agent session that
// Claude Code gives its Bash tool; false when there is none.
func agentSessionID(flagValue string, env Env) (string, bool) {
	id := strings.TrimSpace(flagValue)
	if id == "" {
		id = strings.TrimSpace(env.ClaudeSessionID)
	}
	return id, id != ""
}

// noAgentSession is the error of a command that found no Agent session.
const noAgentSession = "no Agent session: run this from Claude Code, or give --session"
