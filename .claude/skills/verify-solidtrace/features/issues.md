# Issues

An Issue is the grouped failure record of one or more Events that share a fingerprint in a Project. An SDK sends Events to ingest, and Event processing makes or reopens the Issue. On the Issues page a user filters Issues by time, ticks several Issues to resolve or unresolve them together, and opens one Issue to resolve it, unresolve it, or comment on it.

## Sub-features

- `issues-from-events`: an Event sent to `/api/<project id>/store?sentry_key=<key>` becomes an Issue with that title, and a second Event with the same fingerprint adds to the same Issue.
- `issues-time-filter`: the time filter lists the Issues seen in the chosen range (`issues.spec.ts`).
- `issues-bulk`: ticked Issues are resolved and unresolved together (`Select <title>`, `Select all Issues on this page`, `Resolve`, `Unresolve`).
- `issues-page`: an Issue's page resolves and unresolves it, and its age counts from its first Event.
- `issues-reopen`: a resolved Issue reopens when a new Event comes for it.

## How to get to it (user POV)

- The sidebar link to Issues, which opens `/<org>/issues?project_id=<id>`.
- An Issue's title in the list, which opens `/<org>/projects/<project>/issues/<number>`.
- An application's Sentry SDK, with a DSN such as `http://testkey123@localhost:4510/<project id>`.

## Driving it with verify

Preconditions:

- The baseline stack is up, and `"$V" doctor` is all `ok`.
- The seed Project's id, from `"$V" run -- bin/rails runner 'print Project.find_by!(slug: "solid-trace-project").id'`.

- **Event to Issue.** Send an Event as an SDK does: `curl -s -X POST "http://localhost:4510/api/<id>/store?sentry_key=testkey123" -H 'Content-Type: application/json' -d '{"event_id":"'"$(openssl rand -hex 16)"'","level":"error","platform":"ruby","exception":{"values":[{"type":"VerifyError","value":"verify probe issue"}]}}'`. The answer is `200`. Then run `"$V" snap --path "/solid-trace-org/issues?project_id=<id>" --name issues-from-event --wait-text VerifyError`. Pass: exit 0, and the ARIA snapshot has the Issue.
- **Same fingerprint.** Send the same body again with a new `event_id`. Then read the count: `"$V" run -- bin/rails runner 'p Issue.where("title LIKE ?", "%VerifyError%").pluck(:times_seen)'`. Pass: one Issue with `2`.
- **Many Issues at once.** `"$V" run -- bin/rails dev:data LOGS=0` sends Issues with Events through ingest, as an SDK does.
- **List, filter, resolve.** `"$V" flow -- e2e/flows/issues.spec.ts`. Pass: each test passes.
- **Proof.** The `snap` files and the flow traces in `<run>/evidence/`.

## Gotchas

- `issues.spec.ts` makes its Issues with `rails runner` in its own Project, and deletes them after. It proves the page, not Event processing. To prove that Events become Issues, use the ingest steps above.
- Event processing is asynchronous. A `200` from ingest means that the Event is stored, not that the Issue exists. Wait for the text in the page or poll the database.
- Issues are per Project. Give `project_id` in the URL, or the page shows the Organization's default Project.
