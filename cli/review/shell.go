package review

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// commitMessages are the messages of the git commit and git merge commands
// that a Bash command runs, one for each such command, and "" for one that
// gives no message on the command line. A git command in a heredoc's body
// or in a quoted text is not run, so it is not in the list.
func commitMessages(command string) []string {
	var messages []string
	for _, c := range shellCommands(command) {
		if message, ok := commitMessage(c); ok {
			messages = append(messages, message)
		}
	}
	return messages
}

// subject is the subject of a commit message as git log %s gives it: the
// first paragraph as one line, with each run of white space as one space.
func subject(message string) string {
	var first []string
	for _, s := range strings.Split(strings.TrimLeft(message, "\n \t"), "\n") {
		if strings.TrimSpace(s) == "" {
			break
		}
		first = append(first, s)
	}
	return collapse(strings.Join(first, " "))
}

// reviewTrailers are the values of the Review-Findings trailers in commit
// messages, such as "run 1: F2, F5".
func reviewTrailers(messages []string) []string {
	// As git does, the trailers are the last paragraph of the message, and
	// only when each of its lines is a trailer or a folded line of one.
	var paragraphs []string
	for _, p := range strings.Split(strings.Join(messages, "\n\n"), "\n\n") {
		if strings.TrimSpace(p) != "" {
			paragraphs = append(paragraphs, p)
		}
	}
	if len(paragraphs) == 0 {
		return nil
	}
	var out []string
	for _, s := range strings.Split(strings.Trim(paragraphs[len(paragraphs)-1], "\n"), "\n") {
		if s != "" && (s[0] == ' ' || s[0] == '\t') {
			continue
		}
		if !trailerLine.MatchString(s) {
			return nil
		}
		if v, ok := strings.CutPrefix(s, reviewTrailer); ok {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

// trailerLine is a "Token: value" line of git's trailers.
var trailerLine = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*: `)

// shellCommand is a simple command of a Bash command: its words with the
// quotes taken out, and the bodies of its heredocs and here-strings.
type shellCommand struct {
	words []string
	stdin []string
}

// commandPrefixes are the words before a command that do not name it.
var commandPrefixes = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "while": true, "until": true, "do": true,
	"!": true, "{": true, "time": true, "env": true, "command": true, "exec": true,
}

var assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// gitOptionsWithValue are the git options before the subcommand that take
// the next word as their value.
var gitOptionsWithValue = map[string]bool{
	"-C": true, "-c": true, "--git-dir": true, "--work-tree": true, "--namespace": true,
	"--config-env": true, "--super-prefix": true,
}

func commitMessage(c *shellCommand) (string, bool) {
	w := c.words
	for len(w) > 0 && (commandPrefixes[w[0]] || assignment.MatchString(w[0])) {
		w = w[1:]
	}
	if len(w) == 0 || filepath.Base(w[0]) != "git" {
		return "", false
	}
	w = w[1:]
	for len(w) > 0 && strings.HasPrefix(w[0], "-") {
		if gitOptionsWithValue[w[0]] && len(w) > 1 {
			w = w[1:]
		}
		w = w[1:]
	}
	if len(w) == 0 {
		return "", false
	}
	switch w[0] {
	case "commit":
	case "merge":
		if slices.Contains(w[1:], "--abort") || slices.Contains(w[1:], "--quit") {
			return "", false
		}
	default:
		return "", false
	}
	return messageOf(w[1:], c.stdin), true
}

// messageOf is the commit message that the arguments of git commit or git
// merge give: each -m is a paragraph, and -F - reads the heredoc.
func messageOf(args, stdin []string) string {
	var paragraphs, trailers []string
	fromStdin := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		// value is the option's value: attached to it, else the next word.
		value := func(attached string, has bool) string {
			if has || i+1 >= len(args) {
				return attached
			}
			i++
			return args[i]
		}
		long, v, has := strings.Cut(a, "=")
		switch {
		case a == "--":
			i = len(args)
		case long == "--message":
			paragraphs = append(paragraphs, value(v, has))
		case long == "--file":
			fromStdin = fromStdin || value(v, has) == "-"
		case long == "--trailer":
			trailers = append(trailers, value(v, has))
		case strings.HasPrefix(a, "--") || !strings.HasPrefix(a, "-") || a == "-":
		default:
			// A group of short options, such as -am "text" or -qmtext.
			for j := 1; j < len(a); j++ {
				rest := a[j+1:]
				switch a[j] {
				case 'm':
					paragraphs = append(paragraphs, value(rest, rest != ""))
				case 'F':
					fromStdin = fromStdin || value(rest, rest != "") == "-"
				case 'C', 'c', 't':
					value(rest, rest != "")
				default:
					continue
				}
				break
			}
		}
	}
	message := strings.Join(paragraphs, "\n\n")
	if len(paragraphs) == 0 && fromStdin && len(stdin) > 0 {
		message = stdin[len(stdin)-1]
	}
	if len(trailers) > 0 {
		message = strings.TrimRight(message, "\n") + "\n\n" + strings.Join(trailers, "\n")
	}
	return message
}

// shellCommands splits a Bash command into its simple commands, at the
// operators, the new lines and the parentheses. It reads quotes, escapes,
// comments, heredocs and $(…) as Bash does, well enough to find the git
// commands and their messages; it does not expand variables.
func shellCommands(src string) []*shellCommand {
	p := shellParser{src: src}
	return p.commands(false)
}

type shellParser struct {
	src string
	pos int
}

type heredoc struct {
	cmd   *shellCommand
	i     int
	delim string
	tabs  bool
}

// commands reads simple commands up to the end of src, or in a $(…) up to
// its closing parenthesis.
func (p *shellParser) commands(inSubstitution bool) []*shellCommand {
	var cmds []*shellCommand
	cur := &shellCommand{}
	var pending []heredoc
	depth := 0
	end := func() {
		if len(cur.words) > 0 || len(cur.stdin) > 0 {
			cmds = append(cmds, cur)
		}
		cur = &shellCommand{}
	}
	for p.pos < len(p.src) {
		switch c := p.src[p.pos]; {
		case c == ' ' || c == '\t' || c == '\r':
			p.pos++
		case c == '\n':
			p.pos++
			for _, h := range pending {
				h.cmd.stdin[h.i] = p.heredocBody(h.delim, h.tabs)
			}
			pending = nil
			end()
		case c == '#':
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		case c == '&' && p.at(1) == '>':
			p.pos++
			p.redirect(cur, &pending)
		case c == ';' || c == '&' || c == '|':
			p.pos++
			end()
		case c == '(':
			p.pos++
			depth++
			end()
		case c == ')':
			p.pos++
			end()
			if inSubstitution && depth == 0 {
				return cmds
			}
			depth--
		case c == '<' || c == '>':
			p.redirect(cur, &pending)
		default:
			start := p.pos
			w := p.word()
			// The number of a file descriptor, as in 2>&1, is not a word.
			if (p.at(0) == '<' || p.at(0) == '>') && strings.Trim(p.src[start:p.pos], "0123456789") == "" {
				continue
			}
			cur.words = append(cur.words, w)
		}
	}
	end()
	return cmds
}

// at is the byte n bytes after the parser's place, or 0 at the end.
func (p *shellParser) at(n int) byte {
	if p.pos+n < len(p.src) {
		return p.src[p.pos+n]
	}
	return 0
}

// redirect reads a redirection: a heredoc or here-string becomes a body
// of cur's stdin, and the target of another redirection is skipped.
func (p *shellParser) redirect(cur *shellCommand, pending *[]heredoc) {
	op := p.pos
	for p.pos < len(p.src) && strings.IndexByte("<>&|-", p.src[p.pos]) >= 0 && p.pos-op < 3 {
		p.pos++
	}
	kind := p.src[op:p.pos]
	for p.at(0) == ' ' || p.at(0) == '\t' {
		p.pos++
	}
	target := p.word()
	switch {
	case kind == "<<<":
		cur.stdin = append(cur.stdin, target+"\n")
	case strings.HasPrefix(kind, "<<"):
		cur.stdin = append(cur.stdin, "")
		*pending = append(*pending, heredoc{cur, len(cur.stdin) - 1, target, kind == "<<-"})
	}
}

// heredocBody reads the lines up to the line that is delim.
func (p *shellParser) heredocBody(delim string, tabs bool) string {
	var body strings.Builder
	for p.pos < len(p.src) {
		line, _, found := strings.Cut(p.src[p.pos:], "\n")
		p.pos += len(line)
		if found {
			p.pos++
		}
		if tabs {
			line = strings.TrimLeft(line, "\t")
		}
		if line == delim {
			break
		}
		body.WriteString(line + "\n")
	}
	return body.String()
}

// word reads one word up to a space or an operator, with its quotes and
// escapes taken out.
func (p *shellParser) word() string {
	var w strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case strings.IndexByte(" \t\r\n;&|()<>", c) >= 0:
			return w.String()
		case c == '\'':
			end := strings.IndexByte(p.src[p.pos+1:], '\'')
			if end < 0 {
				end = len(p.src) - p.pos - 1
			}
			w.WriteString(p.src[p.pos+1 : p.pos+1+end])
			p.pos += end + 2
		case c == '"':
			p.pos++
			p.doubleQuoted(&w)
		case c == '\\':
			if p.at(1) != '\n' && p.at(1) != 0 {
				w.WriteByte(p.at(1))
			}
			p.pos += 2
		case c == '$' && p.at(1) == '\'':
			p.pos += 2
			p.ansiQuoted(&w)
		case c == '$' && p.at(1) == '(':
			w.WriteString(p.substitution())
		case c == '`':
			w.WriteString(p.backquoted())
		default:
			w.WriteByte(c)
			p.pos++
		}
	}
	return w.String()
}

func (p *shellParser) doubleQuoted(w *strings.Builder) {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == '"':
			p.pos++
			return
		case c == '\\' && strings.IndexByte("$`\"\\\n", p.at(1)) >= 0:
			if p.at(1) != '\n' {
				w.WriteByte(p.at(1))
			}
			p.pos += 2
		case c == '$' && p.at(1) == '(':
			w.WriteString(p.substitution())
		case c == '`':
			w.WriteString(p.backquoted())
		default:
			w.WriteByte(c)
			p.pos++
		}
	}
}

var ansiEscapes = map[byte]string{'n': "\n", 't': "\t", 'r': "\r", '\\': "\\", '\'': "'", '"': "\""}

func (p *shellParser) ansiQuoted(w *strings.Builder) {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == '\'':
			p.pos++
			return
		case c == '\\' && ansiEscapes[p.at(1)] != "":
			w.WriteString(ansiEscapes[p.at(1)])
			p.pos += 2
		default:
			w.WriteByte(c)
			p.pos++
		}
	}
}

// substitution reads a $(…). Its value is the body of its heredoc when it
// is only a cat of a heredoc, as in -m "$(cat <<'EOF' … EOF)". Else it is
// the text of the $(…).
func (p *shellParser) substitution() string {
	start := p.pos
	p.pos += 2
	inner := p.commands(true)
	if len(inner) == 1 && slices.Equal(inner[0].words, []string{"cat"}) && len(inner[0].stdin) == 1 {
		return strings.TrimRight(inner[0].stdin[0], "\n")
	}
	return p.src[start:min(p.pos, len(p.src))]
}

func (p *shellParser) backquoted() string {
	start := p.pos
	for p.pos++; p.pos < len(p.src) && p.src[p.pos] != '`'; p.pos++ {
		if p.src[p.pos] == '\\' {
			p.pos++
		}
	}
	p.pos++
	return p.src[start:min(p.pos, len(p.src))]
}
