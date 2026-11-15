package dispatcher

import (
	"testing"
	"time"

	"github.com/solidtrace/factory/linear"
)

func TestTheStatusByOfALinearTicketIsTheActorOfTheChangeToItsStatus(t *testing.T) {
	ann, ben := linear.User{ID: "lin-ann", Name: "Ann"}, linear.User{ID: "lin-ben", Name: "Ben"}
	at := time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)

	// The newest change is to the issue's status: its actor set the status.
	ticket := linearTicket(linear.Issue{ID: "issue-1", Status: "Ready for agent",
		LastStatusChange: linear.StatusChange{At: at, By: ben, Status: "Ready for agent"}}, "Project")
	if want := (TrackerUser{ID: "lin-ben", Name: "Ben"}); ticket.StatusBy != want {
		t.Errorf("the StatusBy is %+v, want %+v", ticket.StatusBy, want)
	}

	// The newest change in the history page is to another status (the
	// change to the status is not in the page): its actor did not set the
	// status, and the Run must not show that actor as the person who
	// started it.
	ticket = linearTicket(linear.Issue{ID: "issue-2", Status: "Ready for agent",
		LastStatusChange: linear.StatusChange{At: at, By: ann, Status: "Backlog"}}, "Project")
	if ticket.StatusBy != (TrackerUser{}) {
		t.Errorf("the StatusBy is %+v, want none: Ann moved the issue to Backlog, not to Ready for agent", ticket.StatusBy)
	}
}
