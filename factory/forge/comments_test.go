package forge_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/solidtrace/factory/forge"
	"github.com/solidtrace/factory/internal/fakeforge"
)

// openPR opens the PR 1 from agent/r1 through the Forge.
func openPR(t *testing.T, f *fakeforge.Forge, b *forge.Bitbucket) forge.PR {
	t.Helper()
	f.Push(fakeRepo, "agent/r1", map[string]string{"fix.txt": "a\nb\nc\n"}, "Fix")
	pr, err := b.OpenPR(context.Background(), repository, forge.NewPR{Source: "agent/r1", Title: "Fix"})
	if err != nil {
		t.Fatal(err)
	}
	return pr
}

func TestAPRsAuthorIsTheAccountThatOpenedIt(t *testing.T) {
	f, b := newForge(t)
	pr := openPR(t, f, b)
	got, err := b.PR(context.Background(), repository, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Author.Person() || got.Author.Name != fakeforge.FactoryAuthor || got.Author.ID == "" || got.Author.UUID == "" {
		t.Fatalf("the PR's author = %+v; want the factory's account", got.Author)
	}
	if pr.Author != got.Author {
		t.Errorf("the opened PR's author %+v is not the read PR's %+v", pr.Author, got.Author)
	}
}

func TestThePRCommentsComeWithTheirAuthorPlaceThreadAndState(t *testing.T) {
	f, b := newForge(t)
	ctx := context.Background()
	pr := openPR(t, f, b)
	general := f.PersonComments(fakeRepo, pr.ID, fakeforge.Comment{Author: "Asha", Body: "Looks close"})
	inline := f.PersonComments(fakeRepo, pr.ID, fakeforge.Comment{Author: "Ravi", Body: "Rename this", File: "fix.txt", Line: 3})
	reply := f.PersonComments(fakeRepo, pr.ID, fakeforge.Comment{Author: "Asha", Body: "Agreed", Parent: inline.ID})
	bot := f.PersonComments(fakeRepo, pr.ID, fakeforge.Comment{Author: "Pipelines", Body: "Build passed", Bot: true})
	resolved := f.PersonComments(fakeRepo, pr.ID, fakeforge.Comment{Author: "Ravi", Body: "Typo", File: "fix.txt", Line: 1})
	f.Resolve(fakeRepo, pr.ID, resolved.ID)
	gone := f.PersonComments(fakeRepo, pr.ID, fakeforge.Comment{Author: "Ravi", Body: "Oops"})
	f.Delete(fakeRepo, pr.ID, gone.ID)

	comments, err := b.PRComments(ctx, repository, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 6 {
		t.Fatalf("got %d comments, want 6: %+v", len(comments), comments)
	}
	byID := map[int]forge.Comment{}
	for _, c := range comments {
		byID[c.ID] = c
	}
	g, i, r, bt, rs, d := byID[general.ID], byID[inline.ID], byID[reply.ID], byID[bot.ID], byID[resolved.ID], byID[gone.ID]
	if g.Text != "Looks close" || g.Author.Name != "Asha" || !g.Author.Person() || g.File != "" || g.Line != 0 ||
		g.Parent != 0 || g.Resolved || g.Deleted || g.Edited == "" {
		t.Errorf("the general comment = %+v", g)
	}
	if i.Text != "Rename this" || i.Author.Name != "Ravi" || i.File != "fix.txt" || i.Line != 3 || i.Resolved {
		t.Errorf("the inline comment = %+v", i)
	}
	if r.Parent != inline.ID || r.File != "fix.txt" || r.Line != 3 {
		t.Errorf("the reply = %+v; want the parent %d on fix.txt line 3", r, inline.ID)
	}
	if bt.Author.Person() || bt.Author.Name != "Pipelines" || bt.Author.Kind == "" {
		t.Errorf("the bot's comment has the author %+v; want an account that is not a person", bt.Author)
	}
	if !rs.Resolved {
		t.Errorf("the resolved comment = %+v", rs)
	}
	if !d.Deleted {
		t.Errorf("the deleted comment = %+v", d)
	}
	if g.Author.Same(i.Author) || !g.Author.Same(r.Author) || g.Author.Same(forge.Account{}) {
		t.Errorf("Same: Asha and Ravi are not the same account, Asha is Asha, and no account is the empty one")
	}

	// An edit changes the comment's text and its edit mark.
	f.Edit(fakeRepo, pr.ID, general.ID, "Looks close, but rename the test")
	comments, err = b.PRComments(ctx, repository, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range comments {
		if c.ID == general.ID && (c.Text != "Looks close, but rename the test" || c.Edited == g.Edited) {
			t.Errorf("after the edit: %+v; want the new text and another edit mark than %q", c, g.Edited)
		}
	}
}

func TestThePRCommentsComeFromEachPage(t *testing.T) {
	f, b := newForge(t)
	pr := openPR(t, f, b)
	const n = 230
	for k := 0; k < n; k++ {
		f.PersonComments(fakeRepo, pr.ID, fakeforge.Comment{Author: "Asha", Body: fmt.Sprintf("comment %d", k)})
	}
	comments, err := b.PRComments(context.Background(), repository, pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != n {
		t.Fatalf("got %d comments, want %d", len(comments), n)
	}
	for k, c := range comments {
		if c.Text != fmt.Sprintf("comment %d", k) {
			t.Fatalf("comment %d is %q", k, c.Text)
		}
	}
}

// A next page on another host does not get the token.
func TestANextPageOutsideTheAPIIsRefused(t *testing.T) {
	var other atomic.Int32
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { other.Add(1) }))
	t.Cleanup(elsewhere.Close)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"values":[],"next":%q}`, elsewhere.URL+"/2.0/repositories/team/app/pullrequests/1/comments?page=2")
	}))
	t.Cleanup(api.Close)
	b := &forge.Bitbucket{APIURL: api.URL + "/2.0", Token: "the-token"}
	_, err := b.PRComments(context.Background(), repository, 1)
	var fe *forge.Error
	if !errors.As(err, &fe) {
		t.Fatalf("a next page on another host: %v; want a forge error", err)
	}
	if other.Load() != 0 {
		t.Errorf("the other host got %d requests", other.Load())
	}
}

func TestAPostedCommentIsTheFactorysAndCanBeAReply(t *testing.T) {
	f, b := newForge(t)
	ctx := context.Background()
	pr := openPR(t, f, b)
	asked := f.PersonComments(fakeRepo, pr.ID, fakeforge.Comment{Author: "Asha", Body: "/agent again"})
	posted, err := b.PostPRComment(ctx, repository, pr.ID, forge.NewComment{Text: "The Run is closed.", Parent: asked.ID})
	if err != nil {
		t.Fatal(err)
	}
	if posted.ID == 0 || posted.Text != "The Run is closed." || posted.Parent != asked.ID || posted.Author.Name != fakeforge.FactoryAuthor {
		t.Fatalf("the posted comment = %+v", posted)
	}
	all := f.Comments(fakeRepo, pr.ID)
	if len(all) != 2 || all[1].Author != fakeforge.FactoryAuthor || all[1].Parent != asked.ID || all[1].Body != "The Run is closed." {
		t.Fatalf("the comments on the forge = %+v", all)
	}
	if _, err := b.PostPRComment(ctx, repository, pr.ID, forge.NewComment{}); err == nil {
		t.Errorf("a comment with no text was posted")
	}
}

func TestARepositoryIsPrivateUntilItIsMadePublic(t *testing.T) {
	f, b := newForge(t)
	ctx := context.Background()
	if private, err := b.Private(ctx, repository); err != nil || !private {
		t.Fatalf("Private = %v %v; want true", private, err)
	}
	f.SetPublic(fakeRepo, true)
	if private, err := b.Private(ctx, repository); err != nil || private {
		t.Fatalf("Private of a public repository = %v %v; want false", private, err)
	}
	f.FailNext(http.StatusForbidden, "no scope")
	if _, err := b.Private(ctx, repository); err == nil {
		t.Errorf("Private with a refused call gave no error")
	}
}
