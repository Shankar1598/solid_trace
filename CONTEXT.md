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
A Project's position in Pebble, up to which Event processing has handled Events.
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
A named value on a Log that is not a built-in field. Nested names are flattened to dotted names (`http.method`), an array is one value, and each value keeps its own type.
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

**Query**:
A request for Logs written in SolidTrace's KQL-style query language. It always runs against one Project and time range chosen outside the text.
_Avoid_: APL, KQL, search

**SQL query**:
Raw DuckDB SQL a user runs directly, unscoped by Project, only on installs that have turned it on.
_Avoid_: raw query, advanced query

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
- A **Project** owns many **Issues** and the keys used to submit **Events**
- A **Project** owns many **Logs**, submitted with the same keys as its **Events**
- A **Console** presents **Projects**, **Issues**, and **Integrations**
- **Integration notification** turns **Issue** changes into **Notifications**; each **Notification** belongs to exactly one **Integration**

## Example dialogue

> **Dev:** "If we change **Event processing**, should the **Console** need to know about new fingerprint rules?"
> **Domain expert:** "No. **Event processing** should hide that and continue producing the right **Issue** for the **Project**."

## Flagged ambiguities

- "event store" was being used for both the Go tier and the Rails HTTP client; resolved: **EventStore** means the Go application, while the Rails client is just an adapter that talks to it.