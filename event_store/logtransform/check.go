package logtransform

import (
	"strings"

	"github.com/solidtrace/event_store/vrl"
)

// MaxProgramBytes caps a program's size.
const MaxProgramBytes = 64 << 10

// empty reports whether program is no Log transform: an empty program, or
// one of only white space, means none.
func empty(program string) bool {
	return strings.TrimSpace(program) == ""
}

// isError reports whether a diagnostic stops a program compiling.
func isError(d vrl.Diagnostic) bool {
	return d.Severity == "error" || d.Severity == "bug"
}

// Problem is an error or a warning in a program.
type Problem struct {
	Message string `json:"message"`
	Code    int    `json:"code"` // VRL's code: E105 is 105; 0 for the size limit
	// Start and End are the byte offsets of the source the problem is about.
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"` // VRL's formatted diagnostic, quoting the source
}

// Check is what is wrong with a program. It compiles when Errors is empty.
type Check struct {
	Errors   []Problem `json:"errors"`
	Warnings []Problem `json:"warnings"`
}

// CheckProgram compiles program and returns its errors and warnings.
func CheckProgram(program string) Check {
	compiled, check := compile(program)
	if compiled != nil {
		compiled.Free()
	}
	return check
}

// compile compiles program, and returns it with its warnings, or nil and its
// errors. A program over MaxProgramBytes is an error.
func compile(program string) (*vrl.Program, Check) {
	check := Check{Errors: []Problem{}, Warnings: []Problem{}}
	if len(program) > MaxProgramBytes {
		message := "the program is over 64 KB"
		check.Errors = append(check.Errors, Problem{
			Message: message,
			Start:   len(cut(program, MaxProgramBytes)),
			End:     len(program),
			Text:    "error: " + message,
		})
		return nil, check
	}
	compiled, diagnostics := vrl.Compile(program)
	for _, d := range diagnostics {
		p := problem(d)
		if isError(d) {
			check.Errors = append(check.Errors, p)
		} else {
			check.Warnings = append(check.Warnings, p)
		}
	}
	return compiled, check
}

// problem is a diagnostic as a Problem, about the source its primary label
// spans, or its first label's when none is primary.
func problem(d vrl.Diagnostic) Problem {
	p := Problem{Message: d.Message, Code: d.Code, Text: d.Formatted}
	if p.Text == "" {
		p.Text = d.Message
	}
	for i, label := range d.Labels {
		if label.Primary || i == 0 {
			p.Start, p.End = label.Start, label.End
		}
		if label.Primary {
			break
		}
	}
	return p
}
