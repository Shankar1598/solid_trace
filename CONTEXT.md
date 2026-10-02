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
The EventStore module that turns stored Events into Issues, indexes them in DuckDB and tells the Console. Owns the Issue lifecycle: classification, find-or-create, and reopen-on-resolved.
_Avoid_: pipeline, ingester

**Processing cursor**:
A Project's position in its stored Events or Log batches, up to which Event processing or Log processing has handled them. Event processing keeps its cursor in Pebble; Log processing keeps its cursor in `logs.duckdb`, committed together with the Logs it covers.
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
_Avoid_: source, app, dataset

**Log ingest**:
The EventStore module that authenticates incoming Logs, normalises them, stores each request's Logs in Pebble as one Log batch and acknowledges them. A `200` means the Logs are stored.
_Avoid_: shipper endpoint, collector

**Log batch**:
The normalised Logs of one ingest request, stored in Pebble as one entry until Log processing has moved them to DuckDB.
_Avoid_: chunk, payload

**Log processing**:
The EventStore module that moves stored Log batches into each Project's hot table in DuckDB, reading each Project's Log batches from its Processing cursor. Separate from Event processing; Logs are never grouped into Issues.
_Avoid_: pipeline, indexer

**Retention**:
How many whole days a Project's Logs are kept before they are deleted: an instance default that a Project can override.
_Avoid_: TTL, expiry, archive period

**Query**:
A request for Logs written in SolidTrace's KQL-style query language. It always runs against one Project and time range chosen outside the text.
_Avoid_: APL, KQL, search

**SQL query**:
Raw DuckDB SQL a user runs directly, only on installs that have turned it on. Its `logs` view shows the Project and time range chosen outside the text; this is a convenience, not a limit on what the SQL can read.
_Avoid_: raw query, advanced query

**Builder**:
The Logs explorer's default way to make a Query without typing it: filter chips plus a search box. It can show only Queries made of equality filters and one search.
_Avoid_: query builder, visual mode, simple mode

**Integration**:
An outbound notification target owned by an organization.
_Avoid_: webhook config, notifier job

**Notification**:
A message owed to one **Integration** about one or more **Issues**, recorded in the outbox until it ends as sent, failed after the last retry, or skipped because it can never be delivered. Each has a notification kind (issue created, threshold reached, assignment changed) that decides the message.
_Avoid_: notification event, alert, event type

**Integration notification**:
The Console module that decides which **Notifications** an **Issue** change produces, and delivers them to each **Integration** in batches under a per-Integration rate limit.
_Avoid_: notifier, integration notifier

**Project**:
The namespace that owns project keys and groups Issues inside an organization.
_Avoid_: app, tenant

## Relationships

- An **EventStore** accepts an **Event** through **Event ingest**
- **Event processing** derives one **Issue** for each stored **Event**, reading each **Project**'s Events from its **Processing cursor**
- An **EventStore** accepts **Logs** through **Log ingest**, one **Log batch** per request
- **Log processing** moves each **Project**'s **Log batches** into DuckDB, reading them from its **Processing cursor**
- A **Project** owns many **Issues** and the keys used to submit **Events**
- A **Project** owns many **Logs**, submitted with the same keys as its **Events**
- A **Project**'s **Retention** decides when its **Logs** are deleted
- A **Console** presents **Projects**, **Issues**, and **Integrations**
- **Integration notification** turns **Issue** changes into **Notifications**; each **Notification** belongs to exactly one **Integration**

## Example dialogue

> **Dev:** "If we change **Event processing**, should the **Console** need to know about new fingerprint rules?"
> **Domain expert:** "No. **Event processing** should hide that and continue producing the right **Issue** for the **Project**."

## Flagged ambiguities

- "event store" was being used for both the Go tier and the Rails HTTP client; resolved: **EventStore** means the Go application, while the Rails client is just an adapter that talks to it.