package dispatcher

import (
	"strings"
	"testing"
)

func TestThePROpenedCommentListsEachCheckAndSaysNoEvidenceWithNoPassedCheck(t *testing.T) {
	const pr, run = "https://bitbucket.org/example/shop/pull-requests/1", "Run: https://solidtrace.example.com/example/runs/r-1"
	with := prOpenedComment(pr, run, "Fixed the total.", []checkLine{{name: "unit", passed: true}, {name: "lint"}})
	for _, part := range []string{pr, run, "Fixed the total.", "- passed: unit", "- failed: lint"} {
		if !strings.Contains(with, part) {
			t.Errorf("the comment does not have %q:\n%s", part, with)
		}
	}
	if strings.Contains(with, "no Evidence") {
		t.Errorf("a PR with a passed check says no Evidence:\n%s", with)
	}

	for _, checks := range [][]checkLine{nil, {{name: "lint"}}} {
		if got := prOpenedComment(pr, run, "Fixed.", checks); !strings.Contains(got, "no Evidence") {
			t.Errorf("a PR with the checks %v does not say no Evidence:\n%s", checks, got)
		}
	}
}
