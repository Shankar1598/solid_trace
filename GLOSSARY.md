# SolidTrace Context

SolidTrace has two application tiers: Console presents Projects, Issues, and Integrations, and EventStore ingests and queries Events.

## Language

**Console**:
The Rails application that manages Projects, Issues, users, and Integrations.
_Avoid_: dashboard backend, web tier

**EventStore**:
The Go application that ingests Events and serves analytical Event queries.
_Avoid_: collector, pipeline

**Event**:
A raw Sentry payload accepted by EventStore and persisted for later query.
_Avoid_: log line, message

**Event ingest**:
The EventStore module that authenticates an incoming payload, stores it as an Event in Pebble and acknowledges it. A `200` means the Event is stored.
_Avoid_: handler logic, endpoint glue

**Event processing**:
The EventStore module that turns stored Events into Issues, stores their rows in ClickHouse and tells the Console. Owns the Issue lifecycle: classification, find-or-create, and reopen-on-resolved.
_Avoid_: pipeline, ingester

**Processing cursor**:
A Project's position in its stored Events or Log batches, up to which Event processing or Log processing has handled them. Both keep their cursors in Pebble, with the range they are inserting into ClickHouse, so a range inserted again after a crash is stored once.
_Avoid_: offset, checkpoint

**IssueRepository**:
The seam between **Event processing** and Issue persistence. Defined as an interface in `processing/`; the production adapter is `storage.SQLiteWriter`. Event processing calls the repository to find, create, and reopen Issues — but the domain decisions (e.g. "resolved Issues reopen on new Events") live in Event processing, not the repository.
_Avoid_: issue store, issue service

**Issue**:
The grouped failure record derived from one or more Events that share a fingerprint inside a Project.
_Avoid_: alert, ticket

**Log**:
One timestamped record an application or shipper sends to a Project, made of built-in fields (time, level, message, service, environment, trace id, span id) and any number of Attributes. Never grouped into Issues.
_Avoid_: event, log line, entry

**Attribute**:
A named value on a Log that is not a built-in field. Nested names are flattened to dotted names (`http.method`), an array is one value, and each value keeps its own type, except a whole number too large to hold exactly, which is kept as a string.
_Avoid_: tag, label, column

**Metric data point**:
One value of an OpenTelemetry metric at one time, such as a sum, a gauge or a histogram, that Log ingest takes from an OTLP sender. Each one is stored as one Log: the metric name is the message, and the values are Attributes under `metric.`.
_Avoid_: a metric, sample, measurement (for one stored point). "Metrics" stays the name of the OpenTelemetry signal, as in "Logs and metrics".

**Field**:
Either a built-in field of a Log or an Attribute; what filters, chips and the field list refer to. Built-in field names are reserved: a value that cannot fill one is kept as an Attribute named `original.<field>`. Names beginning `solidtrace.` are reserved for Attributes SolidTrace sets, and a sender's such key is kept as `original.solidtrace.<rest>`.
_Avoid_: key, column, property

**Level**:
A Log's severity, one of `trace`, `debug`, `info`, `warn`, `error`, `fatal`, or absent when the sender gave none or gave one that is not recognised.
_Avoid_: severity, priority

**Service**:
The part of a system that emitted a Log, such as an application, a worker or a proxy. How Logs from one Project are told apart.
_Avoid_: "source" alone for a Service, app, dataset; a **Source** is a Service and file together

**Log ingest**:
The EventStore module that authenticates incoming Logs, normalises them, stores each request's Logs in Pebble as one Log batch and acknowledges them. A `200` means the Logs are stored.
_Avoid_: shipper endpoint, collector

**Ingest counts**:
Log ingest's per-second counts of what arrived per Project: Logs received, bytes received (decoded and as sent), requests, unreadable Logs, Logs filtered out as not of a **Captured repository** and requests turned away as overloaded, by second of arrival. Kept in memory and written to ClickHouse about once a second; deleted with the Project's days by Retention, Project deletion and the disk limit.
_Avoid_: ingest metrics, ingest stats, throughput log

**Source**:
A Service and `file` of one Project together: what the Overview's Biggest sources ranks and **Source counts** count.
_Avoid_: "source" for a Service alone

**Source counts**:
Log processing's daily counts of Logs and bytes per Project, Service and `file`, by the Logs' own UTC day, with the Logs the Log transform dropped.
_Avoid_: source stats, top talkers

**Log batch**:
The normalised Logs of one ingest request, stored in Pebble as one entry until Log processing has moved them to ClickHouse.
_Avoid_: chunk, payload

**Log processing**:
The EventStore module that moves stored Log batches into ClickHouse about once a second, reading each Project's Log batches from its Processing cursor and running the Project's Log transform on their Logs. A Log is in its final place once stored, whatever its time. Separate from Event processing; Logs are never grouped into Issues.
_Avoid_: pipeline, indexer, Log export, hot table, late Log

**Log transform**:
A Project's one VRL program, which Log processing runs on each of the Project's Logs before storing it. It can set, change and remove Fields and drop whole Logs, and what it returns is normalised as Log ingest normalises a sender's Logs. A Log it fails on is stored as it arrived, with the error in `solidtrace.transform_error`. A change applies to the Logs processed after it; stored Logs keep what they had. Every saved version is kept, and restoring one saves it again. Any member can read it; only an admin can change it. Events never go through it.
_Avoid_: VRL layer, pipeline, remap, derived columns, parser

**Retention**:
How many whole days a Project's Logs are kept before they are deleted: an instance default that a Project can override.
_Avoid_: TTL, expiry, archive period

**Trace**:
Every Span, Log and Event that shares one trace id, across an Organization's Projects. A trace id from another Organization shows nothing of it.
_Avoid_: request, distributed trace

**Span**:
One timed operation in a Trace, with an op, description, start, duration and parent. Its self time is its duration less the time its children cover, and its group is its op and description with literals replaced, so the same query with other values is one group.
_Avoid_: event, segment (for any Span but a Transaction's own)

**Transaction**:
The root Span of one request or job in one Project, as a Sentry SDK sends it. It is named by route or job, and holds its child Spans. One named by its raw URL, or by nothing, is stored as Unparameterised, keeping its raw name as an Attribute. It never becomes an Issue.
_Avoid_: request, endpoint, segment

**Span processing**:
The EventStore module that moves received Transactions from Pebble to ClickHouse, as Log processing moves Logs: each Project's from its own cursor, a range at a time under a dedup token, so a retry never stores a range twice. It works out each Span's self time, group and repeats.
_Avoid_: trace pipeline, APM ingest

**Trace Retention**:
How many whole days a Project keeps its Spans: an instance default, 14 days to start, that a Project can override. Separate from Retention, which is its Logs'.
_Avoid_: span TTL, APM retention

**Query**:
A request for Logs written in SolidTrace's KQL-style query language. It always runs against one Project and time range chosen outside the text.
_Avoid_: APL, KQL, search

**SQL query**:
ClickHouse SQL a user runs directly, on installs that leave it on. It runs as its Project's own database user, so it reads only that Project's Logs, Events and Spans, in the `logs`, `events` and `spans` tables, starting from the time range chosen outside the text; other tables, files and URLs are refused, and it stops at fixed limits of time and memory.
_Avoid_: raw query, advanced query

**Logs explorer**:
The Console page where a Project's Logs are searched and read.
_Avoid_: log viewer, log search, logs page

**Builder**:
The Logs explorer's default way to make a Query without typing it: filter chips plus a search box. It can show only Queries made of equality filters and one search.
_Avoid_: query builder, visual mode, simple mode

**Live tail**:
The Logs explorer's way of showing a Project's Logs as they arrive, in place of a time range: the newest Logs matching the current Query, in the order they were ingested, newest first, with each Log's own time. Only a Query that returns Logs can be tailed.
_Avoid_: live stream, streaming, live mode

**Chart**:
A **Query**'s or **SQL query**'s table drawn as a time series, bars or a single number. A time series needs the last summarize's time bins, which are its x; its number columns are the series and its by columns split them. Bars need a summarize with no time bin. Any table of one row and one number is a single number. The Logs explorer shows it above the table.
_Avoid_: graph, plot, visualization

**Threshold line**:
A horizontal line on a line or area **Chart** of a **Dashboard**, at a fixed value in the Chart's unit, with a label and an optional severity, warn or critical. It is drawn once across the whole range, and the value axis reaches it. It is not a series: the by columns do not split it, it is not in the legend, and it does not take one of the Chart's series. Its severity only sets its colour and is not a Log's **Level**. It only draws a limit and sends nothing; the threshold of a **Threshold monitor** is a different thing.
_Avoid_: reference line, limit series, threshold (alone)

**Overview**:
An Organization's page of what came in, what was kept and what it costs, over Today, 7 or 30 whole UTC days, from **Ingest counts**, **Source counts** and what ClickHouse stores. Any member sees it; `get_overview` answers the same over MCP.
_Avoid_: stats page, usage page, home

**Dashboard**:
A page of **Charts** and **Text blocks** that belongs to an organization. Each chart saves a **Query** on one of the organization's **Projects**, and every chart runs over the time range the Dashboard shows, with the values of the **Dashboard filters** it names. Any member of the organization can see, make, change and delete it.
_Avoid_: board, panel page

**Text block**:
A note in Markdown on a **Dashboard**, with an optional title, at a place on the grid as a **Chart** has, in a **Section** or in none. A member moves, resizes, changes and deletes it as a Chart. It has no **Project** and no **Query**, so it runs nothing, and the range, the **Dashboard filters** and a refresh do not change it. It shows no raw HTML and no images, and its links open in a new tab.
_Avoid_: note, markdown panel, text chart

**Dashboard filter**:
A control in a **Dashboard**'s filter bar, a select or a search box, whose value a **Chart** uses by naming it in its **Query**, as in `where service == $service`. It narrows only the Charts that name it. With no value, it is All, and its comparisons match every **Log**. It can have default values, which it has when the Dashboard opens, until a member picks others or All.
_Avoid_: variable, dashboard parameter, template variable

**Section**:
A titled group of a **Dashboard**'s **Charts**, in an order the members set. The Dashboard shows its Charts in no Section first, then each Section, a header over its Charts on a grid of their own. A viewer can fold a Section to its header; the fold is kept in that viewer's browser only. When a Section is deleted, its Charts stay on the Dashboard in no Section.
_Avoid_: group, row, panel, folder

**Monitor**:
A saved **Query** or **SQL query** on one of an organization's **Projects** that runs every few minutes and tells its **Integrations** what it finds. A Threshold monitor's Query or SQL query gives one number per group; a group fires when its number passes the threshold and resolves when it no longer does. A Match monitor sends the **Logs** ingested since its last run that match its Query. Any member of the organization can see, make, change and delete it.
_Avoid_: alert, alert rule, check

**Threshold monitor**:
A **Monitor** whose **Query** ends in a summarize of one number per row, or whose **SQL query** gives one number per row. Each row is a group, named by its other columns, and each run reads the Monitor's range ending 60 seconds before the run. A group is **firing** while its number passes the threshold and **ok** otherwise. The number is either the value itself or its percent change from its **Baseline**.
_Avoid_: metric alert, threshold alert, threshold rule

**Baseline**:
A **Threshold monitor** group's number over an earlier range of the same length: the range just before, one day earlier, or one week earlier. A Monitor that compares the change fires when the group's number rises, falls or changes by at least its threshold percent from its Baseline.
_Avoid_: previous value, comparison window, reference period

**Match monitor**:
A **Monitor** whose **Query** returns **Logs**. Each run reads the Logs ingested since the last one, in **Log batch** order, and sends one **Notification** when any match. It keeps no state between runs, so it is never firing. Its text is always a Query.
_Avoid_: log alert, match rule

**Firing** and **ok**:
The two states of a **Threshold monitor**'s group: firing while its number passes the threshold, or while the table is empty when the Monitor alerts on no data, and ok otherwise. Going from ok to firing sends a monitor firing **Notification** and going back sends monitor resolved; a group that stays firing sends nothing more. Turning the Monitor off, deleting it, making it a **Match monitor** or switching it between comparing the value and the change resolves its firing groups.
_Avoid_: alerting, triggered, breached, open

**Log file store**:
Where the Log files of the DuckDB releases are: the Parquet files that held a **Project**'s **Logs** stored before the switch to ClickHouse, on the local disk, in the bucket, or both. They are read in place, alongside the Logs stored since, until **Retention** deletes them; no new Log files are written.
_Avoid_: archive, cold storage, data lake, Late file, Log file order

**Bin cache**:
EventStore's memory of a **Chart**'s time bins of hours that have ended, so a Chart run again, as a **Dashboard** refreshes, reads only the bins it lacks. A bin is kept under the Project, the Query, its size and start, and the generation of each hour it covers, which changes whenever Logs of that hour are stored or deleted, so a bin whose Logs have changed is never used again.
_Avoid_: query cache, result cache

**Integration**:
An outbound notification target owned by an organization. Its provider is Slack, PagerDuty, email, or the **Factory** provider, which starts a **Run** for each **Issue** or **Monitor** group that it is told about, through a **Webhook sender** that Console makes at the **Dispatcher**. The Factory provider has no URL field.
_Avoid_: webhook config, notifier job

**Notification**:
A message owed to one **Integration** about one or more **Issues**, or about one group of a **Monitor**, recorded in the outbox until it ends as sent, failed after the last retry, refused by a receiver that will never take it (the **Factory** provider's 4xx other than 429, which is not retried), or skipped because it can never be delivered. Each has a notification kind (issue created, threshold reached, assignment changed, monitor firing, monitor resolved, monitor matched) that decides the message.
_Avoid_: notification event, alert, event type

**Integration notification**:
The Console module that decides which **Notifications** an **Issue** change produces, records those a **Monitor**'s run owes, and delivers them to each **Integration** under a per-Integration rate limit: Issues in batches (one at a time to the **Factory** provider, which starts a **Run** for each), and each group of a Monitor in order.
_Avoid_: notifier, integration notifier

**Organization**:
The group of people, with their roles, that shares a set of **Projects**, **Dashboards**, **Monitors** and **Integrations**. A user can belong to several and works in one at a time.
_Avoid_: team, workspace, org (in prose), tenant

**Invite**:
An admin adding someone to an **Organization** by email. Someone who already has a password becomes a member at once; anyone else becomes an **Invited** member.
_Avoid_: invitation request, join request

**Invited**:
A member who has never set a password, and so can't sign in yet. They stop being Invited when they set one, from an **Invite link** or a password reset, and are then an ordinary member of every Organization that invited them.
_Avoid_: pending user, inactive user

**Invite link**:
A link an admin copies for an **Invited** member, and shares however they like, that lets the member set a password once and signs them in to the Organization it came from. It stops working after seven days, once a password is set, or when the member is removed from that Organization.
_Avoid_: magic link, invitation email, activation link

**Sign-up**:
Making an account without an **Invite**. It works while the install has no user, so whoever set it up can make the first account, which is the **Instance admin**, and after that only if the operator has opened it; otherwise people join by **Invite**.
_Avoid_: registration, self-serve sign-up

**Instance admin**:
A user who changes the install's settings that apply to every **Organization**, such as email. The first user to sign up is one, or on an install that already had users, the oldest; the operator grants or removes it from the command line. Being an Organization's admin isn't enough.
_Avoid_: superuser, super admin, site admin, root

**Console URL**:
The address people and links reach **Console** at, set by an **Instance admin**: emails, invite links and the MCP server's links point there, and **Passkeys** work only on pages opened there.
_Avoid_: base URL, host, domain (for the whole address)

**Ingest URL**:
The address SDKs send **Events**, **Logs** and traces to, which the DSNs Console shows are built from, set by an **Instance admin**. Changing it changes only the DSNs shown from then on.
_Avoid_: DSN host, EventStore URL

**Project**:
The namespace that owns project keys and groups Issues inside an organization. Its description says what it holds, so agents over MCP can tell which Project to query.
_Avoid_: app, tenant

**Meta Organization**:
The Organization SolidTrace makes for itself when it is installed, holding one Project that receives SolidTrace's own Logs, with one Service for each part of SolidTrace. Its first admin is the person who set up the instance, who is also the **Instance admin**. Otherwise an ordinary Organization: deleting it, or its Project, stops SolidTrace's own Logs for good.
_Avoid_: system organization, internal organization, admin organization

**MCP server**:
The Console endpoint where an AI agent reads and changes Projects, Logs, Dashboards, Monitors and Issues over the Model Context Protocol, acting as the user whose **API token** or **OAuth token** it presents.
_Avoid_: agent API, bot, integration

**API token**:
A secret a user makes in their settings that lets a client act as that user through the **MCP server**, the upload of **Agent sessions** and the **Dispatcher**, with the user's role checked on every call. A token with no **Scope** acts as its user over the MCP server. A token with **Scopes** can do only what they give, in its one **Organization**. It can be limited to a list of **Agent sessions** (`sessions:read` always has this list), and an `mcp:read` token to one **Project**. It can expire. Console keeps only its digest. The Dispatcher's token is the one API token with `tokens:create`: an Admin makes it, and it acts as that Admin. It makes a child token for each **Execution**, with some of the Scopes it can give, limits as narrow or narrower, and an expiry no later than its own. A child token stops when its parent is revoked, or when the Admin leaves the **Organization**.
_Avoid_: API key, project key, access token

**OAuth client**:
A program that asks a user, through Console's approval page or device page, to act as them, and gets **OAuth tokens** when the user approves. Console knows it from the address of its published description, or from its registration, whose name Console cannot check. One OAuth client is part of Console: `solidtrace-cli`, the agent CLI that `solidtrace login` runs. Only it, and clients with a published description, can use the device page.
_Avoid_: app, connected app, integration

**Grant**:
One approval by a user of an **OAuth client**, for one **Organization** and some **Scopes**. Each sign-in is its own Grant. The user sees their Grants as Signed-in clients on Settings → User and revokes each one; revoking it stops its OAuth tokens at once.
_Avoid_: connection, authorization, consent

**OAuth token**:
An access token or a refresh token that Console gives under a **Grant**. An access token works for one hour; a refresh token gets a new one, and works once. Console keeps only digests.
_Avoid_: API token (that is the secret a user makes by hand)

**Scope**:
A named right that an **OAuth token** or an **API token** carries, for one resource, with the text the approval page shows. The first is `mcp`, for the **MCP server**; `mcp:read` gives only its read tools. The **Factory** adds `sessions:write`, `sessions:read`, `run_events:write`, `factory`, `issues:comment` and `tokens:create`.
_Avoid_: permission, role

**Session**:
A browser signed in as a user. It lasts 24 hours from signing in, or 7 days if they ticked Remember me, and ends sooner when they sign out or reset their password.
_Avoid_: login

**Two-factor**:
Having an **Authenticator app** or a **Passkey**, so that a password alone no longer signs the user in: after it, they give a code, a **Recovery code** or a Passkey.
_Avoid_: 2FA (in prose), MFA, two-step verification

**Authenticator app**:
An app on the user's phone or computer that shows a new six-digit code every 30 seconds; one way to have **Two-factor**.
_Avoid_: OTP, TOTP (in prose)

**Passkey**:
A way to sign in without a password, kept on the user's device and unlocked with its fingerprint, face or PIN; also one way to have **Two-factor**. It works only where Console is on a domain over HTTPS, and stops working if that domain changes.
_Avoid_: security key, WebAuthn credential

**Recovery code**:
One of a set of single-use codes a user gets when they first turn on **Two-factor**, accepted in place of a code or a **Passkey** when they can use neither.
_Avoid_: backup code

**Require two-factor**:
An **Organization**'s setting that keeps its members without **Two-factor** out of its pages and its **MCP server** calls until they turn it on.
_Avoid_: mandatory 2FA, enforced 2FA

**Agent session**:
One conversation of one coding agent, kept as its transcript. A resume continues the same Agent session; a fork or a clear starts a new one. Its subagents are parts of it. Its title is the last name that a person gave it (`/rename`), else the last title that Claude Code made for it, else the start of its first person message.
_Avoid_: session (alone), conversation, thread, chat

**Review record**:
The record of a PR's reviews that the `open-pr` and `update-pr` skills write at the end of an **Agent session**'s final reply: each review run, each finding with every decision on it and its outcome, the problems that a person, an agent or a test run found and the reviews missed, and the confirmations. Each `update-pr` writes the next round of the same PR. SolidTrace keeps it from the uploaded Agent session, and the newest round is the PR's record.
_Avoid_: PR record, review report, findings record

**Run**:
One unit of work that the factory gives to a coding agent, from its trigger until its PR is merged or declined, or 30 days pass with no activity. It continues across stops, and records who stepped in and the outcome of its PR.
_Avoid_: job, task, agent run

**Active Run**:
A **Run** that has an **Execution** waiting to start or working, or that waits on a person or an account limit, or whose PR is open.
_Avoid_: open run, running run

**Closed Run**:
A **Run** that can no longer continue, because its PR is merged or declined, or 30 days have passed since its last activity (the end of an Execution or a message it accepted). A Run that is done with no PR, failed or stopped is neither active nor closed.
_Avoid_: finished run, ended run

**Execution**:
One time that a **Run** works on a machine, until it ends, waits for a person or an account limit, or is stopped. An answer, PR feedback or a take-over continues the Run in a new Execution.
_Avoid_: attempt, retry, allocation

**Dispatcher**:
The factory's service that queues **Runs**, starts their **Executions** and holds the factory's secrets.
_Avoid_: orchestrator, scheduler

**Factory**:
The part of SolidTrace that gives work to coding agents on the team's own machines, lets people steer them, and records each **Run** in SolidTrace. It is off until the operator turns it on: with no Dispatcher URL in the Instance settings, Console has no Runs link in the sidebar or the palette, no Runs page, no Factory section in a **Project**, no Factory provider for an **Integration** and no Stop button, and a SolidTrace install runs no **Dispatcher**.
_Avoid_: agent platform, orchestrator

**Agent Project**:
The **Project** of an **Organization** that holds the **Agent sessions** of its **Runs**, its engineers' Agent sessions and Claude Code telemetry in a **Captured repository**, and the record of its Runs. It has at most one key, which sends only that telemetry.
_Avoid_: transcripts project, sessions project

**Captured repository**:
A repository whose engineers' **Agent sessions** and Claude Code telemetry an **Organization** records. An Admin lists it in the **Agent Project**'s settings as `host/workspace/name`, or `host/workspace/*` for a whole workspace, and each member can read the list and the Agent sessions. An Agent session in any other repository, or in no repository, never leaves the engineer's machine. Claude Code telemetry of any other repository, or of no repository, is dropped when it arrives.
_Avoid_: tracked repo, allowed repo

**Redaction pattern**:
A pattern of a credential that the server removes from each **Agent session** before it stores it: each character that the pattern matches becomes `*`, so each line keeps its length and stays valid. A default list is always on, and an **Instance admin** adds patterns. A pattern cannot match a quote or a backslash.
_Avoid_: scrub rule, secret filter

**Factory settings**:
A **Project**'s settings for the **Factory**: its repository and folder, the branch its Runs start from when none is given, and which tracker tickets go to it.
_Avoid_: factory config, factory.yaml

**Factory file**:
The file `.solidtrace/factory.toml` in a repository that says how the **Factory** works on it: the machine kind, the setup command, the named check commands, the extra hosts that agents can reach, the time limits of an **Execution** and more patterns that a work-in-progress save leaves out. The **Dispatcher** reads it from the default branch only, from the **Project**'s folder first and then from the root, when it starts each Execution, so a change applies after a person merges it. A file with an error fails the **Run** with the reason.
_Avoid_: factory config, factory.yaml, Factory settings (those are in the Dispatcher)

**Checkpoint**:
The work-in-progress save of an **Execution** that the wrapper pushes to the **Run**'s WIP ref at an interval (5 minutes unless the operator sets another), only when the work changed. A machine lost with no notice loses only the work after the last checkpoint, and a new Execution restores it.
_Avoid_: autosave, backup

**Webhook sender**:
A tool that starts **Runs** with signed requests to the **Dispatcher**'s generic webhook, with the secret that an Admin got when they made it. It belongs to one **Organization**, can be limited to some of its **Projects**, and is the actor of the Runs that it starts. A Log sender is not a Webhook sender.
_Avoid_: webhook client, integration (alone)

**Evidence**:
What a **Run** shows to prove its work: the checks that the factory's tool ran, with their results and the end of their output, and the files that the agent attached.
_Avoid_: proof, test results (alone)

**Cause report**:
What a **Run** that an **Issue** or a **Monitor** started found: the cause of the problem, and the Logs, the code and the steps that prove it. The agent sends it with its done report. Such a Run opens a PR only when a check proves the fix; with no PR, its cause report is its result. The **Dispatcher** posts an Issue Run's cause report on the Issue as a comment by "Factory".
_Avoid_: root cause analysis, RCA, diagnosis

**Take-over**:
A person's move from steering a **Run** by messages to working in its **Agent session** at a terminal. It continues the same Agent session in a new **Execution**.
_Avoid_: attach, hijack

**Schedule**:
A **Project**'s trigger in the **Dispatcher** that starts a **Run** with its prompt at each fire time of its five-field cron expression, in its time zone. A member makes it, and can pause and continue it. A fire time while its last Run is an **Active Run** starts no Run.
_Avoid_: cron job, timer, scheduled task

**Trigger word**:
The word at the start of a comment that makes the comment a message to a **Run**: `/agent`, unless the operator sets another. A comment on the Run's tracker ticket or PR that starts with it continues the Run. It is not a mention. Only a person whom the team let in can steer a Run so: never the **Dispatcher**'s own identity, a bot, an integration or an outsider.
_Avoid_: command, mention, slash command

## Relationships

- An **EventStore** accepts an **Event** through **Event ingest**
- **Event processing** derives one **Issue** for each stored **Event**, reading each **Project**'s Events from its **Processing cursor**
- An **EventStore** accepts **Logs** through **Log ingest**, one **Log batch** per request
- **Log processing** moves each **Project**'s **Log batches** into ClickHouse, reading them from its **Processing cursor**
- A **Project** has one **Log transform**, which **Log processing** runs on each of its **Logs** before storing it
- A **Project** owns many **Issues** and the keys used to submit **Events**
- A **Project** owns many **Logs**, submitted with the same keys as its **Events**
- A **Project**'s **Retention** decides when its **Logs** are deleted
- An **EventStore** accepts **Transactions** in the same envelopes as **Events**; **Span processing** stores each as its **Spans**
- A **Trace** joins the **Spans**, **Logs** and **Events** of one trace id across an organization's **Projects**
- A **Project**'s **Trace Retention** decides when its **Spans** are deleted
- A **Chart** draws one **Query**'s or **SQL query**'s table; the **Logs explorer** shows it above the table
- A **Dashboard** holds many **Charts**, each a saved **Query** on a **Project** of the Dashboard's organization
- A **Dashboard** has an ordered list of **Dashboard filters**; each narrows the **Charts** whose **Query** names it
- A **Dashboard** has an ordered list of **Sections**; a **Chart** is in one Section of its Dashboard or in none
- A **Dashboard** holds **Text blocks** on the same grid as its **Charts**; a Text block is in one **Section** or in none, and has no **Project** or **Query**
- A **Console** presents **Projects**, **Issues**, and **Integrations**
- **Integration notification** turns **Issue** changes into **Notifications**; each **Notification** belongs to exactly one **Integration**
- A **Monitor** runs a saved **Query** on a **Project** of its organization, and its changes become **Notifications** to its **Integrations**
- A **Threshold monitor**'s groups are **firing** or **ok**; a **Match monitor** reads the **Logs** of each new **Log batch**
- The **Bin cache** keeps a **Chart**'s bins of hours that have ended, until **Log processing** or **Retention** changes their Logs
- The **Log file store** holds a **Project**'s **Logs** stored before the switch to ClickHouse, read with those stored since until **Retention** deletes them
- An **Invite** to an **Organization** makes someone without a password an **Invited** member, whom an **Invite link** lets set a password
- An **MCP server** call acts as the user of its **API token**, with what that user's role allows in the organization it acts on
- An **OAuth client** gets **OAuth tokens** under a **Grant**; an **MCP server** call with an OAuth token acts only in the Grant's **Organization**
- An **Instance admin** sets the install's email, which every **Organization**'s mail is sent with
- An **Organization** with **Require two-factor** admits only members who have **Two-factor**, with an **Authenticator app** or a **Passkey**
- An **Agent session** belongs to its **Organization**'s **Agent Project**; an engineer's own Agent session has no **Run**, and is recorded only in a **Captured repository**
- A **Review record** comes from the `solidtrace review final` call at the end of an **Agent session** (in an older Agent session, from its final reply), and is in the same **Agent Project**, with the same **Retention**
- The **Agent Project**'s key sends Claude Code telemetry as **Logs** of the Agent Project; only the telemetry of a **Captured repository** is kept
- A **Run** works in the repository that its **Project**'s **Factory settings** name, and its record is in the **Agent Project**
- A **Run** that reports done shows its **Evidence**; with no check that passed, it shows no Evidence
- A **Run** has one or more **Executions**, and usually one **Agent session**, which each Execution continues
- The **Dispatcher** starts each **Execution** of a **Run**; SolidTrace keeps the Run's record

## Example dialogue

> **Dev:** "If we change **Event processing**, should the **Console** need to know about new fingerprint rules?"
> **Domain expert:** "No. **Event processing** should hide that and continue producing the right **Issue** for the **Project**."

## Flagged ambiguities

- "event store" was being used for both the Go tier and the Rails HTTP client; resolved: **EventStore** means the Go application, while the Rails client is just an adapter that talks to it.
- "derived columns" from `message` were asked for; resolved: they are ordinary **Attributes** that a **Log transform** sets. And "editable in the dashboard" meant a Console page in the **Project**'s settings, not a **Dashboard**.
- "session" was being used for a coding agent's conversation as well as a signed-in browser; resolved: **Session** stays the signed-in browser, and an agent's conversation is an **Agent session**.
- "run" was being used for one time an agent works on a machine and for the whole piece of work across stops; resolved: the whole piece of work is a **Run**, and each time it works on a machine is an **Execution**. A **Monitor**'s run and a **Chart** run are plain verbs, not a **Run**.
- "checkpoint" was being used for a **Processing cursor** and for the factory's save on an interval; resolved: a **Checkpoint** is only the factory's save, and EventStore's position in its stored Events stays the **Processing cursor**.
