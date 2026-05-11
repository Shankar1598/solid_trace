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
The EventStore module that turns an authenticated incoming payload into an Event plus its derived Issue attributes. Owns the full Issue lifecycle at ingest time: classification, find-or-create, and reopen-on-resolved.
_Avoid_: handler logic, endpoint glue

**IssueRepository**:
The seam between **Event ingest** and Issue persistence. Defined as an interface in `ingest/`; the production adapter is `storage.SQLiteWriter`. Event ingest calls the repository to find, create, and reopen Issues — but the domain decisions (e.g. "resolved Issues reopen on new Events") live in Event ingest, not the repository.
_Avoid_: issue store, issue service

**Issue**:
The grouped failure record derived from one or more Events that share a fingerprint inside a Project.
_Avoid_: alert, ticket

**Integration**:
An outbound notification target owned by an organization.
_Avoid_: webhook config, notifier job

**Project**:
The namespace that owns project keys and groups Issues inside an organization.
_Avoid_: app, tenant

## Relationships

- An **EventStore** accepts an **Event** through **Event ingest**
- An **Event ingest** derives one **Issue** for each accepted **Event**
- A **Project** owns many **Issues** and the keys used to submit **Events**
- A **Console** presents **Projects**, **Issues**, and **Integrations**

## Example dialogue

> **Dev:** "If we change **Event ingest**, should the **Console** need to know about new fingerprint rules?"
> **Domain expert:** "No. **Event ingest** should hide that and continue producing the right **Issue** for the **Project**."

## Flagged ambiguities

- "event store" was being used for both the Go tier and the Rails HTTP client; resolved: **EventStore** means the Go application, while the Rails client is just an adapter that talks to it.