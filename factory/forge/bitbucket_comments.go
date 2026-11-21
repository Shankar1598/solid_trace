package forge

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Bitbucket Cloud's PR comments and the repository's visibility, from the
// REST API 2.0 reference (api-group-pullrequests, api-group-repositories).
// The token's pullrequest:write scope covers the comment calls, and
// repository:write covers the read of the repository. Cluster check 10
// (ticket 88) checks the calls and scopes on Bitbucket Cloud.

// accountJSON is Bitbucket's account object: type is user for a person,
// and app_user for an access token or an app.
type accountJSON struct {
	Type        string `json:"type"`
	AccountID   string `json:"account_id"`
	UUID        string `json:"uuid"`
	DisplayName string `json:"display_name"`
}

func (a accountJSON) account() Account {
	return Account{ID: a.AccountID, UUID: a.UUID, Name: a.DisplayName, Kind: a.Type}
}

// commentJSON is the part of Bitbucket's pullrequest_comment that the
// factory reads. inline.to is the line in the new file; inline.from is
// the line in the old file, for a comment on a removed line.
type commentJSON struct {
	ID      int         `json:"id"`
	User    accountJSON `json:"user"`
	Content struct {
		Raw string `json:"raw"`
	} `json:"content"`
	Inline *struct {
		Path string `json:"path"`
		From *int   `json:"from"`
		To   *int   `json:"to"`
	} `json:"inline"`
	Parent *struct {
		ID int `json:"id"`
	} `json:"parent"`
	Resolution *struct {
		Type string `json:"type"`
	} `json:"resolution"`
	Deleted   bool   `json:"deleted"`
	Pending   bool   `json:"pending"`
	UpdatedOn string `json:"updated_on"`
}

func (c commentJSON) comment() Comment {
	out := Comment{ID: c.ID, Author: c.User.account(), Text: c.Content.Raw, Resolved: c.Resolution != nil,
		Deleted: c.Deleted, Pending: c.Pending, Edited: c.UpdatedOn}
	if c.Inline != nil {
		out.File = c.Inline.Path
		switch {
		case c.Inline.To != nil:
			out.Line = *c.Inline.To
		case c.Inline.From != nil:
			out.Line = *c.Inline.From
		}
	}
	if c.Parent != nil {
		out.Parent = c.Parent.ID
	}
	return out
}

// maxCommentPages is the most pages of comments that PRComments reads:
// 100 pages of 100 comments.
const maxCommentPages = 100

// PRComments is GET .../pullrequests/{id}/comments, 100 on each page, and
// each next page. A next page must be on the same API and repository: the
// token goes nowhere else.
func (b *Bitbucket) PRComments(ctx context.Context, repository string, id int) ([]Comment, error) {
	const action = "read the PR's comments"
	workspace, name, err := b.split(repository)
	if err != nil {
		return nil, err
	}
	base := b.apiURL() + "/repositories/" + url.PathEscape(workspace) + "/" + url.PathEscape(name)
	path := fmt.Sprintf("/pullrequests/%d/comments?pagelen=100", id)
	var comments []Comment
	for page := 1; ; page++ {
		var out struct {
			Values []commentJSON `json:"values"`
			Next   string        `json:"next"`
		}
		if err := b.call(ctx, action, repository, http.MethodGet, path, nil, &out); err != nil {
			return nil, err
		}
		for _, c := range out.Values {
			comments = append(comments, c.comment())
		}
		if out.Next == "" {
			return comments, nil
		}
		next, ok := strings.CutPrefix(out.Next, base+"/")
		if !ok {
			return nil, &Error{Forge: "Bitbucket", Action: action, Repository: repository,
				Message: "the next page of comments is not on the API of the repository: " + b.scrub(out.Next)}
		}
		if page == maxCommentPages {
			return nil, &Error{Forge: "Bitbucket", Action: action, Repository: repository,
				Message: fmt.Sprintf("the PR has more than %d pages of comments", maxCommentPages)}
		}
		path = "/" + next
	}
}

// PostPRComment is POST .../pullrequests/{id}/comments with content.raw,
// and parent.id for a reply.
func (b *Bitbucket) PostPRComment(ctx context.Context, repository string, id int, comment NewComment) (Comment, error) {
	const action = "post a comment on the PR"
	if strings.TrimSpace(comment.Text) == "" {
		return Comment{}, &Error{Forge: "Bitbucket", Action: action, Repository: repository,
			Message: "a comment needs a text"}
	}
	body := map[string]any{"content": map[string]string{"raw": comment.Text}}
	if comment.Parent != 0 {
		body["parent"] = map[string]int{"id": comment.Parent}
	}
	var out commentJSON
	err := b.call(ctx, action, repository, http.MethodPost, fmt.Sprintf("/pullrequests/%d/comments", id), body, &out)
	return out.comment(), err
}

// Private is is_private of GET /repositories/{workspace}/{repo_slug}. An
// answer with no is_private is an error, not a public repository.
func (b *Bitbucket) Private(ctx context.Context, repository string) (bool, error) {
	var repo struct {
		IsPrivate *bool `json:"is_private"`
	}
	if err := b.call(ctx, "read the repository", repository, http.MethodGet, "", nil, &repo); err != nil {
		return false, err
	}
	if repo.IsPrivate == nil {
		return false, &Error{Forge: "Bitbucket", Action: "read the repository", Repository: repository,
			Message: "Bitbucket's answer has no is_private"}
	}
	return *repo.IsPrivate, nil
}
