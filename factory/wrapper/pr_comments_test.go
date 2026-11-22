package wrapper_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/fakeforge"
)

// /agent comments on a Run's PR continue the Run (P1 to P3, P4). Bitbucket
// sends pullrequest:comment_created; a comment that starts with the
// trigger word queues a reconcile of the PR's comments, which reads the
// comments from the forge and makes one message: the trigger comment
// first, then each other comment that is not resolved and that the Run
// did not get before. Only a person's comment on a private repository
// steers a Run; a Closed Run gets one answer comment a day.

const shop = "example/shop"

// prComment adds a comment to the PR and sends its signed
// pullrequest:comment_created webhook. It gives the comment and if the
// webhook queued a reconcile.
func prComment(t *testing.T, env *factorytest.Env, path string, pr int, c fakeforge.Comment) (fakeforge.Comment, bool) {
	t.Helper()
	c = env.Forge.PersonComments(shop, pr, c)
	return c, sendComment(t, env, path, pr, c.ID)
}

// sendComment sends the pullrequest:comment_created webhook of a comment
// with a new request UUID, and gives if it queued a reconcile.
func sendComment(t *testing.T, env *factorytest.Env, path string, pr, comment int) bool {
	t.Helper()
	uuid := fmt.Sprintf("{comment-%d-%d}", comment, time.Now().UnixNano())
	a := env.ForgeWebhook(path, forgeSecret, uuid, fakeforge.EventPRComment, env.Forge.CommentWebhook(shop, pr, comment))
	if a.Status != 200 {
		t.Fatalf("the comment webhook: %d %s, want 200", a.Status, a.Body)
	}
	return queued(t, a)
}

// acceptedMessages waits up to 10 s until SolidTrace has n message
// accepted events of the Run, and gives them.
func acceptedMessages(t *testing.T, env *factorytest.Env, id string, n int) []map[string]any {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		got := eventsOf(env, id, "message_accepted")
		if len(got) >= n {
			if len(got) > n {
				t.Fatalf("want %d accepted messages, got %d: %v", n, len(got), got)
			}
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d accepted messages after 10 s, want %d: %v\nDispatcher log:\n%s", len(got), n, got, env.Logs())
		}
	}
}

// doneRunWithPR is a done Run with a PR, and a Dispatcher that has a
// webhook secret for the forge connection and no free account (a
// continued Run stays queued). It gives the webhook path, the Run id and
// the PR.
func doneRunWithPR(t *testing.T) (*factorytest.Env, *factorytest.Person, string, string, fakeforge.PR) {
	t.Helper()
	env, ana := newEnv(t)
	path := env.ForgeWebhooks(forgeSecret)
	id, pr := runWithPR(t, env, ana)
	env.MarkAtLimit(factorytest.DefaultAccount, env.Clock.Now().Add(24*time.Hour))
	return env, ana, path, id, pr
}

// nothingHappened checks, after each queued reconcile step, that the Run
// is still done with no message and that the factory posted no comment.
func nothingHappened(t *testing.T, env *factorytest.Env, ana *factorytest.Person, id string, pr int, when string) {
	t.Helper()
	barrier(t, env)
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateDone {
		t.Errorf("%s: the Run is %s, want done", when, run.State)
	}
	if got := eventsOf(env, id, "message_accepted"); len(got) != 0 {
		t.Errorf("%s: the Run has the messages %v", when, got)
	}
	for _, c := range env.Forge.Comments(shop, pr) {
		// The test writes the factory's own trigger comments; the
		// factory's answers never start with the trigger word.
		if c.Author == fakeforge.FactoryAuthor && !strings.HasPrefix(c.Body, "/agent") {
			t.Errorf("%s: the factory posted %q", when, c.Body)
		}
	}
}

func TestAnAgentCommentByAReviewerWhoIsNotAMemberContinuesTheRunWithTheUnresolvedComments(t *testing.T) {
	env, ana, path, id, pr := doneRunWithPR(t)
	// Ravi and Asha review on Bitbucket; they are not SolidTrace members.
	rename := env.Forge.PersonComments(shop, pr.ID, fakeforge.Comment{Author: "Ravi", Body: "Rename this function",
		File: "cart.txt", Line: 3})
	reply := env.Forge.PersonComments(shop, pr.ID, fakeforge.Comment{Author: "Asha", Body: "And its test",
		Parent: rename.ID})
	changelog := env.Forge.PersonComments(shop, pr.ID, fakeforge.Comment{Author: "Asha", Body: "Add a changelog line"})
	typo := env.Forge.PersonComments(shop, pr.ID, fakeforge.Comment{Author: "Ravi", Body: "Fix the typo here",
		File: "cart.txt", Line: 1})
	env.Forge.Resolve(shop, pr.ID, typo.ID)
	answered := env.Forge.PersonComments(shop, pr.ID, fakeforge.Comment{Author: "Asha", Body: "Done already?",
		Parent: typo.ID})
	env.Forge.PersonComments(shop, pr.ID, fakeforge.Comment{Author: "Pipelines", Body: "The build failed", Bot: true})
	gone := env.Forge.PersonComments(shop, pr.ID, fakeforge.Comment{Author: "Ravi", Body: "Ignore me"})
	env.Forge.Delete(shop, pr.ID, gone.ID)

	trigger, ok := prComment(t, env, path, pr.ID, fakeforge.Comment{Author: "Ravi", Body: "/agent address the review"})
	if !ok {
		t.Fatal("the /agent comment queued no reconcile")
	}
	ev := acceptedMessages(t, env, id, 1)[0]
	text, _ := ev["message.text"].(string)
	if !strings.HasPrefix(text, "/agent address the review") {
		t.Errorf("the message does not start with the trigger comment:\n%s", text)
	}
	for _, want := range []string{
		fmt.Sprintf("Comment %d by Ravi, on cart.txt line 3, in thread %d:\nRename this function", rename.ID, rename.ID),
		fmt.Sprintf("Comment %d by Asha, on cart.txt line 3, in thread %d:\nAnd its test", reply.ID, rename.ID),
		fmt.Sprintf("Comment %d by Asha, a general comment, in thread %d:\nAdd a changelog line", changelog.ID, changelog.ID),
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the message has no %q:\n%s", want, text)
		}
	}
	for _, not := range []string{"Fix the typo", answered.Body, "The build failed", "Ignore me"} {
		if strings.Contains(text, not) {
			t.Errorf("the message has %q (resolved, by a bot or deleted):\n%s", not, text)
		}
	}
	if strings.Index(text, "Rename this") > strings.Index(text, "And its test") {
		t.Errorf("a thread's comments are not in order:\n%s", text)
	}
	if ev["actor.kind"] != "forge" || ev["actor.id"] != "557058:Ravi" || ev["actor.name"] != "Ravi" ||
		ev["run.continued_from"] != dispatcherapi.StateDone || ev["message.source"] != "pr_comment" ||
		ev["factory.comment"] != float64(trigger.ID) {
		t.Errorf("the accepted event: %v; want Ravi's Bitbucket account, from done", ev)
	}
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateQueued {
		t.Errorf("the Run is %s, want queued", run.State)
	}
}

func TestACommentIsSentOneTimeAndAgainAfterAnEdit(t *testing.T) {
	env, _, path, id, pr := doneRunWithPR(t)
	review := env.Forge.PersonComments(shop, pr.ID, fakeforge.Comment{Author: "Asha", Body: "Rename the test",
		File: "cart.txt", Line: 2})
	first, _ := prComment(t, env, path, pr.ID, fakeforge.Comment{Author: "Asha", Body: "/agent please"})
	acceptedMessages(t, env, id, 1)

	// The same comment again (Bitbucket's retry with a new UUID, or
	// a second delivery) is no new message.
	sendComment(t, env, path, pr.ID, first.ID)
	barrier(t, env)
	if got := eventsOf(env, id, "message_accepted"); len(got) != 1 {
		t.Fatalf("a repeat of the trigger comment gave %d messages, want 1", len(got))
	}

	// A new trigger: the comments that the Run got are not in it.
	prComment(t, env, path, pr.ID, fakeforge.Comment{Author: "Ravi", Body: "/agent also the docs"})
	text, _ := acceptedMessages(t, env, id, 2)[1]["message.text"].(string)
	if !strings.HasPrefix(text, "/agent also the docs") || strings.Contains(text, "Rename the test") ||
		strings.Contains(text, "/agent please") {
		t.Errorf("the second message has comments that the Run got:\n%s", text)
	}

	// An edited comment is sent again, with its new text.
	env.Forge.Edit(shop, pr.ID, review.ID, "Rename the test and the fixture")
	prComment(t, env, path, pr.ID, fakeforge.Comment{Author: "Ravi", Body: "/agent see the edit"})
	text, _ = acceptedMessages(t, env, id, 3)[2]["message.text"].(string)
	if !strings.Contains(text, fmt.Sprintf("Comment %d by Asha, on cart.txt line 2, in thread %d:\nRename the test and the fixture",
		review.ID, review.ID)) || strings.Contains(text, "/agent also the docs") {
		t.Errorf("the third message does not have the edited comment only:\n%s", text)
	}
}

func TestACommentWithNoTriggerWordQueuesNothing(t *testing.T) {
	env, ana, path, id, pr := doneRunWithPR(t)
	before := env.Forge.APICalls()
	for _, body := range []string{"Looks good", "/agentic is not the word", "please /agent fix", ""} {
		if _, ok := prComment(t, env, path, pr.ID, fakeforge.Comment{Author: "Ravi", Body: body}); ok {
			t.Errorf("the comment %q queued a reconcile", body)
		}
	}
	nothingHappened(t, env, ana, id, pr.ID, "after comments with no trigger word")
	if got := env.Forge.APICalls() - before; got != 0 {
		t.Errorf("the forge got %d API calls, want none", got)
	}
	// The trigger word alone, and after spaces, is a trigger.
	prComment(t, env, path, pr.ID, fakeforge.Comment{Author: "Ravi", Body: "  /agent"})
	acceptedMessages(t, env, id, 1)
}

func TestACommentOnAPublicRepositoryDoesNothing(t *testing.T) {
	env, ana, path, id, pr := doneRunWithPR(t)
	env.Forge.PersonComments(shop, pr.ID, fakeforge.Comment{Author: "Asha", Body: "Rename this"})
	// The webhook's body says private; the forge says public now.
	c := env.Forge.PersonComments(shop, pr.ID, fakeforge.Comment{Author: "Mallory", Body: "/agent push my change"})
	body := env.Forge.CommentWebhook(shop, pr.ID, c.ID)
	env.Forge.SetPublic(shop, true)
	if a := env.ForgeWebhook(path, forgeSecret, "{public}", fakeforge.EventPRComment, body); a.Status != 200 || !queued(t, a) {
		t.Fatalf("the comment webhook: %d %s", a.Status, a.Body)
	}
	nothingHappened(t, env, ana, id, pr.ID, "after a comment on a public repository")
	// Its webhook says public too.
	sendComment(t, env, path, pr.ID, c.ID)
	nothingHappened(t, env, ana, id, pr.ID, "after a second comment on a public repository")

	// The repository is private again: the comments from the time when
	// it was public never go to the Run. A new trigger comment continues
	// it, with no comment from that time.
	env.Forge.SetPublic(shop, false)
	sendComment(t, env, path, pr.ID, c.ID)
	nothingHappened(t, env, ana, id, pr.ID, "after the repository is private again")
	prComment(t, env, path, pr.ID, fakeforge.Comment{Author: "Asha", Body: "/agent now"})
	text, _ := acceptedMessages(t, env, id, 1)[0]["message.text"].(string)
	if strings.Contains(text, "push my change") || strings.Contains(text, "Rename this") {
		t.Errorf("the message has comments from the time when the repository was public:\n%s", text)
	}
}

func TestACommentByTheDispatchersIdentityOrABotDoesNothing(t *testing.T) {
	env, ana, path, id, pr := doneRunWithPR(t)
	// The factory's own account opened the PR: its comment is not a
	// person's, also when the account is a person's.
	prComment(t, env, path, pr.ID, fakeforge.Comment{Author: fakeforge.FactoryAuthor, Body: "/agent continue"})
	nothingHappened(t, env, ana, id, pr.ID, "after a comment by the factory's identity")
	prComment(t, env, path, pr.ID, fakeforge.Comment{Author: "Pipelines", Body: "/agent fix the build", Bot: true})
	prComment(t, env, path, pr.ID, fakeforge.Comment{Author: "Renovate", Body: "/agent merge this", Bot: true,
		File: "cart.txt", Line: 1})
	nothingHappened(t, env, ana, id, pr.ID, "after comments by app accounts")
}

func TestAWebhookIsAHintTheForgesCommentsDecide(t *testing.T) {
	env, ana, path, id, pr := doneRunWithPR(t)
	// The comment had the trigger word when Bitbucket sent the webhook;
	// its author removed it, or deleted the comment, or resolved it.
	for _, change := range []func(int){
		func(c int) { env.Forge.Edit(shop, pr.ID, c, "never mind") },
		func(c int) { env.Forge.Delete(shop, pr.ID, c) },
		func(c int) { env.Forge.Resolve(shop, pr.ID, c) },
	} {
		c := env.Forge.PersonComments(shop, pr.ID, fakeforge.Comment{Author: "Ravi", Body: "/agent do it"})
		body := env.Forge.CommentWebhook(shop, pr.ID, c.ID)
		change(c.ID)
		if a := env.ForgeWebhook(path, forgeSecret, fmt.Sprintf("{hint-%d}", c.ID), fakeforge.EventPRComment, body); a.Status != 200 {
			t.Fatalf("the comment webhook: %d %s", a.Status, a.Body)
		}
	}
	// A webhook of a PR that no Run has, and of another repository.
	other := `{"comment":{"id":1,"content":{"raw":"/agent x"}},"pullrequest":{"id":` + fmt.Sprint(pr.ID) +
		`},"repository":{"full_name":"example/other"}}`
	if a := env.ForgeWebhook(path, forgeSecret, "{other}", fakeforge.EventPRComment, []byte(other)); a.Status != 200 {
		t.Fatalf("a comment webhook of another repository: %d %s", a.Status, a.Body)
	}
	nothingHappened(t, env, ana, id, pr.ID, "after webhooks of comments that are no trigger now")
}

func TestACommentWebhookWithNoPROrNoSignatureIsRefused(t *testing.T) {
	env, ana, path, id, pr := doneRunWithPR(t)
	c := env.Forge.PersonComments(shop, pr.ID, fakeforge.Comment{Author: "Ravi", Body: "/agent go"})
	body := env.Forge.CommentWebhook(shop, pr.ID, c.ID)
	if a := env.ForgeWebhook(path, "another-secret", "{bad}", fakeforge.EventPRComment, body); a.Status != 401 {
		t.Errorf("a comment webhook with another secret: %d %s, want 401", a.Status, a.Body)
	}
	for name, raw := range map[string]string{
		"not JSON":      `{"comment":`,
		"no PR":         `{"comment":{"id":1,"content":{"raw":"/agent go"}},"repository":{"full_name":"example/shop"}}`,
		"no repository": `{"comment":{"id":1,"content":{"raw":"/agent go"}},"pullrequest":{"id":1}}`,
	} {
		if a := env.ForgeWebhook(path, forgeSecret, "{"+name+"}", fakeforge.EventPRComment, []byte(raw)); a.Status != 400 {
			t.Errorf("%s: %d %s, want 400", name, a.Status, a.Body)
		}
	}
	nothingHappened(t, env, ana, id, pr.ID, "after the refused webhooks")
}

func TestOnAClosedRunOneAnswerCommentADay(t *testing.T) {
	env, ana, path, id, pr := doneRunWithPR(t)
	env.CheckClosedRuns()
	env.Forge.SetState(shop, pr.ID, "MERGED")
	env.ForgeWebhook(path, forgeSecret, "{merged}", fakeforge.EventPRFulfilled, env.Forge.PRWebhook(shop, pr.ID))
	closedEvent(t, env, id, dispatcherapi.StateDone, "merged", "pr_merged")

	answers := func() []fakeforge.Comment {
		var got []fakeforge.Comment
		for _, c := range env.Forge.Comments(shop, pr.ID) {
			if c.Author == fakeforge.FactoryAuthor {
				got = append(got, c)
			}
		}
		return got
	}
	first, _ := prComment(t, env, path, pr.ID, fakeforge.Comment{Author: "Ravi", Body: "/agent one more fix"})
	barrier(t, env)
	got := answers()
	if len(got) != 1 {
		t.Fatalf("want one answer comment, got %+v", got)
	}
	if got[0].Parent != first.ID || !strings.Contains(got[0].Body, "Run "+id+" is closed") ||
		!strings.Contains(got[0].Body, "start a new Run") || strings.HasPrefix(got[0].Body, "/agent") {
		t.Errorf("the answer: %+v; want a reply that says the Run is closed and how to start a new one", got[0])
	}
	if m := eventsOf(env, id, "message_accepted"); len(m) != 0 {
		t.Errorf("the Closed Run accepted the messages %v", m)
	}

	// Later the same day: no second answer, also for another PR comment.
	env.Clock.Advance(23 * time.Hour)
	prComment(t, env, path, pr.ID, fakeforge.Comment{Author: "Asha", Body: "/agent hello?"})
	sendComment(t, env, path, pr.ID, first.ID)
	barrier(t, env)
	if got := answers(); len(got) != 1 {
		t.Fatalf("a second trigger in the same day: %d answers, want 1", len(got))
	}

	// A day after the answer, a new trigger gets one answer again; a
	// trigger that was answered or seen before gets none.
	env.Clock.Advance(time.Hour)
	sendComment(t, env, path, pr.ID, first.ID)
	barrier(t, env)
	if got := answers(); len(got) != 1 {
		t.Fatalf("a repeat of a trigger that was read: %d answers, want 1", len(got))
	}
	third, _ := prComment(t, env, path, pr.ID, fakeforge.Comment{Author: "Asha", Body: "/agent again"})
	barrier(t, env)
	if got := answers(); len(got) != 2 || got[1].Parent != third.ID {
		t.Fatalf("a trigger a day later: answers %+v, want a second one to comment %d", got, third.ID)
	}
	if run := getRun(t, ana, id); run.State != dispatcherapi.StateDone || run.ClosedAt == nil {
		t.Errorf("the Closed Run is %s, closed at %v", run.State, run.ClosedAt)
	}
}

// An older trigger comment that did not fit in the message stays unread:
// the next trigger comment gives it, as the message says.
func TestAnOlderTriggerCommentThatDidNotFitInTheMessageComesWithTheNextTrigger(t *testing.T) {
	env, _, path, id, pr := doneRunWithPR(t)
	// 30 KiB each: the newest trigger and one other comment fit in 64 KiB.
	big := strings.Repeat("x", 30<<10)
	env.Forge.PersonComments(shop, pr.ID, fakeforge.Comment{Author: "Ravi", Body: "Rename " + big})
	// Its webhook did not arrive.
	older := env.Forge.PersonComments(shop, pr.ID, fakeforge.Comment{Author: "Asha", Body: "/agent first " + big})
	prComment(t, env, path, pr.ID, fakeforge.Comment{Author: "Asha", Body: "/agent second " + big})
	text, _ := acceptedMessages(t, env, id, 1)[0]["message.text"].(string)
	if strings.Contains(text, "/agent first") || !strings.Contains(text, "1 more comments did not fit") {
		t.Fatalf("the first message has the older trigger, or does not say that it did not fit:\n%.300s",
			text[len(text)-300:])
	}

	prComment(t, env, path, pr.ID, fakeforge.Comment{Author: "Ravi", Body: "/agent go on"})
	text, _ = acceptedMessages(t, env, id, 2)[1]["message.text"].(string)
	if !strings.Contains(text, fmt.Sprintf("Comment %d by Asha, a general comment, in thread %d:\n/agent first",
		older.ID, older.ID)) {
		t.Errorf("the next message does not have the older trigger that did not fit:\n%.500s", text)
	}
}
