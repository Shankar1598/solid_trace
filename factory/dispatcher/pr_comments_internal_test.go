package dispatcher

import (
	"strings"
	"testing"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/forge"
)

func reviewer(name string) forge.Account {
	return forge.Account{ID: "557058:" + name, UUID: "{" + name + "}", Name: name, Kind: forge.AccountPerson}
}

// A review larger than a message: the message keeps the limit, and the
// comments that do not fit are not in it (the next trigger gives them).
func TestAMessageOfALargeReviewKeepsTheLimitAndSaysWhatDidNotFit(t *testing.T) {
	// 30 KiB each: the trigger and one comment fit in 64 KiB.
	big := strings.Repeat("x", 30<<10)
	comments := []forge.Comment{{ID: 1, Author: reviewer("Asha"), Text: "/agent " + big}}
	for id := 2; id <= 6; id++ {
		comments = append(comments, forge.Comment{ID: id, Author: reviewer("Ravi"), Text: big, File: "a.go", Line: id})
	}
	r := readReview(comments, reviewer("factory"), "/agent")
	text, in := r.message(r.triggers[0], r.others)
	if len(text) > dispatcherapi.MaxMessage {
		t.Fatalf("the message has %d bytes, more than %d", len(text), dispatcherapi.MaxMessage)
	}
	var ids []int
	for _, c := range in {
		ids = append(ids, c.ID)
	}
	if len(ids) != 1 || ids[0] != 2 {
		t.Fatalf("the comments in the message: %v; want comment 2 only", ids)
	}
	if !strings.Contains(text, "4 more comments did not fit") {
		t.Errorf("the message does not say that 4 comments did not fit:\n%.300s", text[len(text)-300:])
	}
}

// A reply in a thread whose first comment is resolved is resolved; a
// reply whose parent is not in the list keeps its own thread.
func TestTheThreadOfAReplyIsItsFirstComment(t *testing.T) {
	comments := []forge.Comment{
		{ID: 1, Author: reviewer("Ravi"), Text: "Rename", File: "a.go", Line: 3, Resolved: true},
		{ID: 2, Author: reviewer("Asha"), Text: "Agreed", Parent: 1},
		{ID: 3, Author: reviewer("Asha"), Text: "More", Parent: 2},
		{ID: 5, Author: reviewer("Asha"), Text: "Orphan", Parent: 4},
		{ID: 6, Author: reviewer("Asha"), Text: "/agent go"},
	}
	r := readReview(comments, reviewer("factory"), "/agent")
	if r.thread[3] != 1 || r.thread[5] != 5 {
		t.Errorf("threads: %v; want 3 in thread 1, and 5 in its own", r.thread)
	}
	if len(r.others) != 1 || r.others[0].ID != 5 || len(r.triggers) != 1 || r.triggers[0].ID != 6 {
		t.Errorf("others %+v, triggers %+v; want only 5 and the trigger 6", r.others, r.triggers)
	}
}
