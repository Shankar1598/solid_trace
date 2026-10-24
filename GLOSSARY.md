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
Log ingest's per-second counts of what arrived per Project: Logs received, bytes received (decoded and as sent), requests, unreadable Logs and requests turned away as overloaded, by second of arrival. Kept in memory and written to ClickHouse about once a second; deleted with the Project's days by Retention, Project deletion and the disk limit.
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

**Query**:
A request for Logs written in SolidTrace's KQL-style query language. It always runs against one Project and time range chosen outside the text.
_Avoid_: APL, KQL, search

**SQL query**:
ClickHouse SQL a user runs directly, on installs that leave it on. It runs as its Project's own database user, so it reads only that Project's Logs and Events, in the `logs` and `events` tables, starting from the time range chosen outside the text; other tables, files and URLs are refused, and it stops at fixed limits of time and memory.
_Avoid_: raw query, advanced query

**Logs explorer**:
The Console page where a Project's Logs are searched and read.
_Avoid_: log viewer, log search, logs page

**Builder**:
The Logs explorer's default way to make a Query without typing it: filter chips plus a search box. It can show only Queries made of equality filters and one search.
_Avoid_: query builder, visual mode, simple mode

**Chart**:
A **Query**'s or **SQL query**'s table drawn as a time series, bars or a single number. A time series needs the last summarize's time bins, which are its x; its number columns are the series and its by columns split them. Bars need a summarize with no time bin. Any table of one row and one number is a single number. The Logs explorer shows it above the table.
_Avoid_: graph, plot, visualization

**Overview**:
An Organization's page of what came in, what was kept and what it costs, over Today, 7 or 30 whole UTC days, from **Ingest counts**, **Source counts** and what ClickHouse stores. Any member sees it; `get_overview` answers the same over MCP.
_Avoid_: stats page, usage page, home

**Dashboard**:
A page of **Charts** that belongs to an organization. Each chart saves a **Query** on one of the organization's **Projects**, and every chart runs over the time range the Dashboard shows, with the values of the **Dashboard filters** it names. Any member of the organization can see, make, change and delete it.
_Avoid_: board, panel page

**Dashboard filter**:
A control in a **Dashboard**'s filter bar, a select or a search box, whose value a **Chart** uses by naming it in its **Query**, as in `where service == $service`. It narrows only the Charts that name it. With no value, it is All, and its comparisons match every **Log**.
_Avoid_: variable, dashboard parameter, template variable

**Monitor**:
A saved **Query** on one of an organization's **Projects** that runs every few minutes and tells its **Integrations** what it finds. A Threshold monitor's Query ends in a summarize of one number per group; a group fires when its number passes the threshold and resolves when it no longer does. A Match monitor sends the **Logs** ingested since its last run that match its Query. Any member of the organization can see, make, change and delete it.
_Avoid_: alert, alert rule, check

**Threshold monitor**:
A **Monitor** whose **Query** ends in a summarize of one number per row. Each row is a group, named by its other columns, and each run reads the Monitor's range ending 60 seconds before the run. A group is **firing** while its number passes the threshold and **ok** otherwise.
_Avoid_: metric alert, threshold alert, threshold rule

**Match monitor**:
A **Monitor** whose **Query** returns **Logs**. Each run reads the Logs ingested since the last one, in **Log batch** order, and sends one **Notification** when any match. It keeps no state between runs, so it is never firing.
_Avoid_: log alert, match rule

**Firing** and **ok**:
The two states of a **Threshold monitor**'s group: firing while its number passes the threshold, or while the table is empty when the Monitor alerts on no data, and ok otherwise. Going from ok to firing sends a monitor firing **Notification** and going back sends monitor resolved; a group that stays firing sends nothing more. Turning the Monitor off, deleting it or making it a **Match monitor** resolves its firing groups.
_Avoid_: alerting, triggered, breached, open

**Log file store**:
Where the Log files of the DuckDB releases are: the Parquet files that held a **Project**'s **Logs** stored before the switch to ClickHouse, on the local disk, in the bucket, or both. They are read in place, alongside the Logs stored since, until **Retention** deletes them; no new Log files are written.
_Avoid_: archive, cold storage, data lake, Late file, Log file order

**Bin cache**:
EventStore's memory of a **Chart**'s time bins of hours that have ended, so a Chart run again, as a **Dashboard** refreshes, reads only the bins it lacks. A bin is kept under the Project, the Query, its size and start, and the generation of each hour it covers, which changes whenever Logs of that hour are stored or deleted, so a bin whose Logs have changed is never used again.
_Avoid_: query cache, result cache

**Integration**:
An outbound notification target owned by an organization.
_Avoid_: webhook config, notifier job

**Notification**:
A message owed to one **Integration** about one or more **Issues**, or about one group of a **Monitor**, recorded in the outbox until it ends as sent, failed after the last retry, or skipped because it can never be delivered. Each has a notification kind (issue created, threshold reached, assignment changed, monitor firing, monitor resolved, monitor matched) that decides the message.
_Avoid_: notification event, alert, event type

**Integration notification**:
The Console module that decides which **Notifications** an **Issue** change produces, records those a **Monitor**'s run owes, and delivers them to each **Integration** under a per-Integration rate limit: Issues in batches, and each group of a Monitor in order.
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

**Project**:
The namespace that owns project keys and groups Issues inside an organization. Its description says what it holds, so agents over MCP can tell which Project to query.
_Avoid_: app, tenant

**Meta Organization**:
The Organization SolidTrace makes for itself when it is installed, holding one Project that receives SolidTrace's own Logs, with one Service for each part of SolidTrace. Its first admin is the person who set up the instance, who is also the **Instance admin**. Otherwise an ordinary Organization: deleting it, or its Project, stops SolidTrace's own Logs for good.
_Avoid_: system organization, internal organization, admin organization

**MCP server**:
The Console endpoint where an AI agent reads and changes Projects, Logs, Dashboards, Monitors and Issues over the Model Context Protocol, acting as the user whose **API token** it presents.
_Avoid_: agent API, bot, integration

**API token**:
A secret a user makes in their settings that lets a client act as that user through the **MCP server**, with the user's role in each organization checked on every call. Console keeps only its digest.
_Avoid_: API key, project key, access token

## Relationships

- An **EventStore** accepts an **Event** through **Event ingest**
- **Event processing** derives one **Issue** for each stored **Event**, reading each **Project**'s Events from its **Processing cursor**
- An **EventStore** accepts **Logs** through **Log ingest**, one **Log batch** per request
- **Log processing** moves each **Project**'s **Log batches** into ClickHouse, reading them from its **Processing cursor**
- A **Project** has one **Log transform**, which **Log processing** runs on each of its **Logs** before storing it
- A **Project** owns many **Issues** and the keys used to submit **Events**
- A **Project** owns many **Logs**, submitted with the same keys as its **Events**
- A **Project**'s **Retention** decides when its **Logs** are deleted
- A **Chart** draws one **Query**'s or **SQL query**'s table; the **Logs explorer** shows it above the table
- A **Dashboard** holds many **Charts**, each a saved **Query** on a **Project** of the Dashboard's organization
- A **Dashboard** has an ordered list of **Dashboard filters**; each narrows the **Charts** whose **Query** names it
- A **Console** presents **Projects**, **Issues**, and **Integrations**
- **Integration notification** turns **Issue** changes into **Notifications**; each **Notification** belongs to exactly one **Integration**
- A **Monitor** runs a saved **Query** on a **Project** of its organization, and its changes become **Notifications** to its **Integrations**
- A **Threshold monitor**'s groups are **firing** or **ok**; a **Match monitor** reads the **Logs** of each new **Log batch**
- The **Bin cache** keeps a **Chart**'s bins of hours that have ended, until **Log processing** or **Retention** changes their Logs
- The **Log file store** holds a **Project**'s **Logs** stored before the switch to ClickHouse, read with those stored since until **Retention** deletes them
- An **Invite** to an **Organization** makes someone without a password an **Invited** member, whom an **Invite link** lets set a password
- An **MCP server** call acts as the user of its **API token**, with what that user's role allows in the organization it acts on
- An **Instance admin** sets the install's email, which every **Organization**'s mail is sent with

## Example dialogue

> **Dev:** "If we change **Event processing**, should the **Console** need to know about new fingerprint rules?"
> **Domain expert:** "No. **Event processing** should hide that and continue producing the right **Issue** for the **Project**."

## Flagged ambiguities

- "event store" was being used for both the Go tier and the Rails HTTP client; resolved: **EventStore** means the Go application, while the Rails client is just an adapter that talks to it.
- "derived columns" from `message` were asked for; resolved: they are ordinary **Attributes** that a **Log transform** sets. And "editable in the dashboard" meant a Console page in the **Project**'s settings, not a **Dashboard**.