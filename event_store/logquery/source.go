package logquery

import (
	"fmt"

	"github.com/solidtrace/event_store/logquery/parser"
)

// A Query reads the Logs of one Project, the request's. Its leading source,
// when it has one, names them: logs, or the Project's slug as Console writes
// it, such as my-api | where level == "error".

// SourceOf returns the name a Query's text gives its source, or "" when it
// gives none or doesn't parse.
func SourceOf(text string) string {
	tree, err := parser.Parse(text)
	if err != nil || tree.Source == nil {
		return ""
	}
	return tree.Source.Name
}

// CheckSource returns an *Error at the Query's source when it names Logs
// other than those of the Project whose slug is given: any name but logs and
// the slug. An empty slug, a Project Console doesn't know, checks nothing, and
// neither does a Query that doesn't parse: Compile reports that.
func CheckSource(text, slug string) error {
	tree, err := parser.Parse(text)
	if err != nil || tree.Source == nil || slug == "" {
		return nil
	}
	if name := tree.Source.Name; name != "logs" && name != slug {
		return &Error{Span: tree.Source.NameSpan, Message: fmt.Sprintf("expected %s, this Project's slug, or logs, got %q", slug, name)}
	}
	return nil
}
