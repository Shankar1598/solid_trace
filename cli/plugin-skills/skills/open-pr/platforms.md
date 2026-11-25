# Where the PR lives

`open-pr` and `update-pr` do the same few actions on the PR. This file gives the call for each action on each platform. Find the platform from `git remote get-url origin`: github.com or bitbucket.org. A Run is a factory Run (`$SOLIDTRACE_AGENT_SOCKET` is set). Local mode is the `local` argument.

On bitbucket.org, send each request to `https://api.bitbucket.org/2.0/repositories/<workspace>/<repo>` (the base below), with `Authorization: Bearer $BITBUCKET_TOKEN`. Build each JSON body with a tool that escapes text, not by hand.

## The local PR file

The PR file is `$(git rev-parse --git-common-dir)/solidtrace-pr/<branch>.md`, with each `/` of the branch name changed to `-`. It holds a `# <title>` line, the base, the head (`git rev-parse HEAD`), then the body. Git does not track this folder. The feedback file is `<branch>.comments.md` next to it. Each item starts with a line `## <author> · <path>:<line>`, or `## <author>` for a comment on the whole PR, and the text follows. An author `check:<name>` is a failed check.

## Find the open PR of the branch

- **github.com**: `gh pr view <branch> --json number,url,body,baseRefName` (it fails when there is none).
- **bitbucket.org**: `GET <base>/pullrequests?q=source.branch.name="<branch>" AND state="OPEN"`.
- **Local mode**: the PR file, when it exists.

## Open a PR

- **Run**: `solidtrace pr --title "<title>" --description "$(cat "$dir/body.md")"`.
- **github.com**: `gh pr create --base <base> --title "<title>" --body-file "$dir/body.md"`.
- **bitbucket.org**: `POST <base>/pullrequests` with `{"title": ..., "description": <body>, "source": {"branch": {"name": <branch>}}, "destination": {"branch": {"name": <base>}}}`. The PR's URL is `links.html.href` in the answer. With no token, give the person `https://bitbucket.org/<workspace>/<repo>/pull-requests/new?source=<branch>&dest=<base>` and the path of `body.md`.
- **Local mode**: write the PR file. A later local run writes over it.

## Change the PR body

- **Run**: `solidtrace pr --title "<title>" --description "$(cat "$dir/body.md")"`, which changes the Run's PR in place.
- **github.com**: `gh pr edit <number> --body-file "$dir/body.md"`.
- **bitbucket.org**: `PUT <base>/pullrequests/<id>` with `{"description": <body>}`.
- **Local mode**: write the PR file again.

## Read the feedback

- **github.com**: the line comments (`gh api repos/{owner}/{repo}/pulls/<number>/comments`), the other comments (`gh api repos/{owner}/{repo}/issues/<number>/comments`), the review bodies (`gh pr view <number> --json reviews`), and the failed checks (`gh pr checks <number>`).
- **bitbucket.org**: the comments (`GET <base>/pullrequests/<id>/comments`, where a line comment has `inline.path` and `inline.to`) and the build statuses (`GET <base>/pullrequests/<id>/statuses`).
- **Local mode**: the feedback file.

## Who runs the session, and an author's access

- **github.com**: `gh api user --jq .login`, and `gh api repos/{owner}/{repo}/collaborators/<login>/permission` (`admin`, `write`, `read` or `none`).
- **bitbucket.org**: `GET https://api.bitbucket.org/2.0/user`, and whether the author is in `GET <base>/permissions-config/users`.
- **Local mode**: the person runs the session, and wrote all the feedback, with `write` access.

## Reply to feedback

- **github.com**: a line comment with `gh api repos/{owner}/{repo}/pulls/<number>/comments/<id>/replies -f body=<reply>`, other feedback with one `gh pr comment <number> --body-file <file>` that answers each item.
- **bitbucket.org**: `POST <base>/pullrequests/<id>/comments` with `{"content": {"raw": <reply>}, "parent": {"id": <comment id>}}`, with no `parent` for a check.
- **Local mode**: under each item of the feedback file, add the reply as a line that starts with `> `.
